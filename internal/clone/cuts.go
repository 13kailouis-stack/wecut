package clone

import (
	"math"
	"sort"
)

// lowStat is what is kept about each frame of the low stream.
type lowStat struct {
	mean, std float64
	diff      float64 // mean |this - previous|
	hdist     float64 // luma histogram distance to the previous frame
	sharp     float64
	hist      [32]float64
}

// Event is a change from one shot to the next. A hard cut has Start ==
// End == At. A transition spans Start..End: the old shot is still visible
// at Start, the new one fully at End.
type Event struct {
	At    float64 `json:"at"`
	Kind  string  `json:"kind"` // cut, flash, fadeblack, dissolve, zoom, blur
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Score float64 `json:"score"`
}

func (e Event) Dur() float64 { return e.End - e.Start }

// detectEvents finds hard cuts and transitions in the low stream.
func (a *Analyzer) detectEvents() []Event {
	st := a.low
	n := len(st)
	if n < 3 {
		return nil
	}
	fps := a.cfg.LowFPS
	t := func(i int) float64 { return float64(i) / fps }
	base := func(i int) float64 {
		var v []float64
		for j := max(1, i-10); j <= min(n-1, i+10); j++ {
			if j < i-1 || j > i+1 {
				v = append(v, st[j].diff)
			}
		}
		return median(v)
	}

	// Frames that are almost white or almost black, used for flashes and
	// fades. A long run is a white or black shot, not a transition.
	bright := make([]bool, n)
	dark := make([]bool, n)
	for i, s := range st {
		bright[i] = s.mean >= 185 && s.std <= 50
		dark[i] = s.mean <= 18 && s.std <= 12
	}

	var evs []Event
	used := make([]bool, n)
	maxRun := int(math.Round(0.9 * fps))

	// Flashes: a short run of bright frames, often with a cut inside it.
	for i := 1; i < n-1; i++ {
		if !bright[i] || used[i] {
			continue
		}
		j := i
		for j+1 < n && bright[j+1] {
			j++
		}
		if j-i+1 > maxRun || i == 0 {
			i = j
			continue
		}
		// Grow over the frames brightening into and fading out of it.
		s, e := i, j
		for s > 1 && st[s-1].mean > st[s-2].mean+6 && i-s < int(0.4*fps) {
			s--
		}
		for e < n-2 && st[e+1].mean > st[e+2].mean+6 && e-j < int(0.5*fps) {
			e++
		}
		before, after := max(0, s-1), min(n-1, e+1)
		if st[before].mean > 170 && st[after].mean > 170 {
			i = j
			continue // a bright scene, not a flash
		}
		for k := s; k <= e; k++ {
			used[k] = true
		}
		mid := (i + j + 1) / 2
		evs = append(evs, Event{At: t(mid), Kind: "flash", Start: t(s), End: t(e + 1), Score: 100})
		i = e
	}

	// Fades through black.
	for i := 1; i < n-1; i++ {
		if !dark[i] || used[i] {
			continue
		}
		j := i
		for j+1 < n && dark[j+1] {
			j++
		}
		if j-i+1 > maxRun || j >= n-1 {
			i = j
			continue
		}
		s, e := i, j
		for s > 1 && st[s-1].mean < st[s-2].mean-3 && i-s < int(0.8*fps) {
			s--
		}
		for e < n-2 && st[e+1].mean < st[e+2].mean-3 && e-j < int(0.8*fps) {
			e++
		}
		if s == i && e == j && j-i < 2 {
			i = j
			continue // a single dark frame inside a dark scene
		}
		for k := s; k <= e; k++ {
			used[k] = true
		}
		evs = append(evs, Event{At: t((i + j + 1) / 2), Kind: "fadeblack", Start: t(s), End: t(e + 1), Score: 100})
		i = e
	}

	// Hard cuts: a jump far above the motion around it.
	for i := 1; i < n; i++ {
		if used[i] {
			continue
		}
		d, b := st[i].diff, base(i)
		if d >= 9 && d >= 3*b+4 && (st[i].hdist >= 0.12 || d >= 30) {
			evs = append(evs, Event{At: t(i), Kind: "cut", Start: t(i), End: t(i), Score: d})
		}
	}

	// Gradual transitions (dissolve, zoom, blur): a run of strong change
	// that ends on a different picture. A zoom or pan inside one shot also
	// changes every frame, but its two ends still have the same colours.
	longBase := func(i int) float64 {
		var v []float64
		for j := max(1, i-int(1.5*fps)); j <= min(n-1, i+int(1.5*fps)); j++ {
			v = append(v, st[j].diff)
		}
		return median(v)
	}
	for i := 2; i < n-2; i++ {
		if used[i] || st[i].diff < math.Max(6, 2.5*longBase(i)) {
			continue
		}
		lb := longBase(i)
		j, dips := i, 0
		for j+1 < n-1 && !used[j+1] && dips <= 1 {
			if st[j+1].diff >= math.Max(4, 1.5*lb) {
				dips = 0
			} else {
				dips++
			}
			j++
		}
		j -= dips
		for i > 2 && !used[i-1] && st[i-1].diff >= math.Max(4, 1.5*lb) {
			i--
		}
		if j-i+1 < 4 || j-i+1 > int(1.6*fps) {
			i = j
			continue
		}
		cutInside := false
		for _, e := range evs {
			if e.At >= t(i-1) && e.At <= t(j+1) {
				cutInside = true
			}
		}
		A, B := a.lowGray[i-1], a.lowGray[j+1]
		hd, cs := histDist(st[i-1].hist, st[j+1].hist), changeScore(A, B)
		if cutInside || (hd < 0.25 && cs < 35) {
			i = j
			continue
		}
		kind := "zoom"
		lowSharp := math.Inf(1)
		for k := i; k <= j; k++ {
			lowSharp = math.Min(lowSharp, st[k].sharp)
		}
		switch {
		case blendScore(A, B, a.lowGray[(i+j)/2]) < 0.5:
			kind = "dissolve"
		case lowSharp < 0.65*math.Min(st[i-1].sharp, st[j+1].sharp):
			kind = "blur"
		}
		for k := i; k <= j; k++ {
			used[k] = true
		}
		evs = append(evs, Event{At: t((i + j + 1) / 2), Kind: kind, Start: t(i - 1), End: t(j + 1), Score: cs})
		i = j
	}

	sort.Slice(evs, func(i, j int) bool { return evs[i].At < evs[j].At })
	evs = mergeEvents(evs, 0.25)

	// A hard cut hidden in blur (whip, zoom blur): sharpness dips around it.
	for k, e := range evs {
		if e.Kind != "cut" {
			continue
		}
		i := int(math.Round(e.At * fps))
		lo, hi := max(0, i-int(0.5*fps)), min(n-1, i+int(0.5*fps))
		// Blurrier than both shots usually are, not just than one of them.
		side := func(a, b int) float64 {
			var v []float64
			for j := max(0, a); j <= min(n-1, b); j++ {
				v = append(v, st[j].sharp)
			}
			return median(v)
		}
		ref := math.Min(side(i-int(1.0*fps), lo-1), side(hi+1, i+int(1.0*fps)))
		s, f := i, i
		for s > lo && st[s-1].sharp < 0.6*ref {
			s--
		}
		for f < hi && st[f].sharp < 0.6*ref {
			f++
		}
		if f-s >= 3 && ref > 3 {
			evs[k].Kind, evs[k].Start, evs[k].End = "blur", t(s), t(f)
		}
	}
	return evs
}

// mergeEvents folds events closer than gap seconds into one: a strobe or a
// glitch trips the detector on several frames in a row, and the viewer sees
// one change. Transitions win over cuts.
func mergeEvents(evs []Event, gap float64) []Event {
	rank := map[string]int{"cut": 0, "zoom": 1, "blur": 1, "dissolve": 2, "fadeblack": 3, "flash": 4}
	var out []Event
	for _, e := range evs {
		k := len(out) - 1
		if k < 0 || (e.Start-out[k].End >= gap && e.At-out[k].At >= gap) {
			out = append(out, e)
			continue
		}
		o := &out[k]
		switch {
		case o.Kind == "cut" && e.Kind == "cut":
			// Keep the last cut: that is where the new picture starts.
			o.At, o.Start, o.End = e.At, e.At, e.At
		case rank[e.Kind] > rank[o.Kind]:
			o.Kind, o.At = e.Kind, e.At
			fallthrough
		default:
			o.Start, o.End = math.Min(o.Start, e.Start), math.Max(o.End, e.End)
		}
		o.Score = math.Max(o.Score, e.Score)
	}
	return out
}

// blendScore says how far mid is from the halfway blend of a and b,
// relative to how different a and b are (0 = a perfect blend).
func blendScore(a, b, mid *Gray) float64 {
	best := math.Inf(1)
	for _, alpha := range []float64{0.2, 0.35, 0.5, 0.65, 0.8} {
		var s float64
		for i := range mid.Pix {
			v := alpha*float64(a.Pix[i]) + (1-alpha)*float64(b.Pix[i])
			s += math.Abs(float64(mid.Pix[i]) - v)
		}
		best = math.Min(best, s/float64(len(mid.Pix)))
	}
	return best / math.Max(1, absDiff(a, b))
}
