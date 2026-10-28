package clone

import "math"

// ShotInfo is one shot of the source: where it sits, how it is framed and
// how the picture moves.
type ShotInfo struct {
	Start float64 `json:"start"` // first moment it is visible (start of the transition into it)
	End   float64 `json:"end"`   // last moment it is visible (end of the transition out of it)
	// Join is the transition into the next shot ("" = hard cut).
	Join    string  `json:"join,omitempty"`
	JoinDur float64 `json:"join_dur,omitempty"`
	Fit     string  `json:"fit"`          // cover or contain
	Bg      string  `json:"bg,omitempty"` // contain: black, blur or #RRGGBB
	Content Box     `json:"content"`      // the picture's area on the canvas
	Zoom    float64 `json:"zoom,omitempty"`
	ZoomTo  float64 `json:"zoom_to,omitempty"`
	// Continue: the same take as the shot before, punched in or out.
	Continue bool    `json:"continue,omitempty"`
	Still    bool    `json:"still,omitempty"` // barely moves: a photo or screenshot
	Motion   float64 `json:"motion"`          // mean frame change inside the shot
	Bright   float64 `json:"brightness"`
}

func (s ShotInfo) Dur() float64 { return s.End - s.Start }

// shots splits [0, end] at the events and measures each piece.
func (a *Analyzer) shots(evs []Event, end float64) []ShotInfo {
	var out []ShotInfo
	start := 0.0
	for i := 0; i <= len(evs); i++ {
		s := ShotInfo{Start: start, End: end, Fit: "cover", Content: Box{0, 0, 1, 1}}
		if i < len(evs) {
			e := evs[i]
			s.End = e.End
			if e.Kind != "cut" && e.End > e.Start {
				s.Join, s.JoinDur = e.Kind, r3(e.End-e.Start)
			}
			start = e.Start
			if e.Kind == "cut" {
				start = e.At
				s.End = e.At
			}
		}
		if s.End-s.Start < 0.04 {
			continue
		}
		s.Start, s.End = r3(s.Start), r3(s.End)
		out = append(out, s)
	}
	// The clean middle of each shot, without the transitions at its ends.
	for k := range out {
		s := &out[k]
		lo, hi := s.Start, s.End
		if k > 0 && out[k-1].Join != "" {
			lo += out[k-1].JoinDur
		}
		if s.Join != "" {
			hi -= s.JoinDur
		}
		if hi-lo < 2/a.cfg.LowFPS {
			lo, hi = s.Start, s.End
		}
		frames := a.framesIn(lo, hi, 7)
		lay := detectLayout(frames)
		s.Fit, s.Bg, s.Content = lay.fit, lay.bg, lay.content
		var mo, br []float64
		for i := a.lowIndex(lo) + 1; i <= a.lowIndex(hi-1e-3); i++ {
			mo = append(mo, a.low[i].diff)
			br = append(br, a.low[i].mean)
		}
		s.Motion, s.Bright = r2(median(mo)), r2(median(br))
		s.Still = len(mo) >= 3 && s.Motion < 0.6
		if s.Fit == "cover" && hi-lo >= 0.7 {
			a.shotZoom(s, lo, hi)
		}
	}
	a.punchIns(out)
	return out
}

// framesIn picks up to n low frames spread over [lo, hi).
func (a *Analyzer) framesIn(lo, hi float64, n int) []*Gray {
	i0, i1 := a.lowIndex(lo), a.lowIndex(math.Max(lo, hi-1e-3))
	var out []*Gray
	if i1 <= i0 {
		return []*Gray{a.lowGray[i0]}
	}
	for k := 0; k < n; k++ {
		i := i0 + int(math.Round(float64(k)*float64(i1-i0)/float64(max(1, n-1))))
		if len(out) == 0 || out[len(out)-1] != a.lowGray[i] {
			out = append(out, a.lowGray[i])
		}
	}
	return out
}

// shotZoom measures a steady zoom across a shot: the picture at the end is
// the picture at the start scaled by z (a slow push in when z > 1). The
// middle frame must sit halfway, or it is the subject moving, not the
// camera.
func (a *Analyzer) shotZoom(s *ShotInfo, lo, hi float64) {
	first := a.lowGray[a.lowIndex(lo+0.05)].half()
	last := a.lowGray[a.lowIndex(hi-0.05)].half()
	mid := a.lowGray[a.lowIndex((lo+hi)/2)].half()
	z, score, flat := zoomBetween(first, last, 0.8, 1.3)
	if score < 0.6 || score-flat < 0.04 || math.Abs(z-1) < 0.06 {
		return
	}
	zm, sm, _ := zoomBetween(first, mid, 0.8, 1.3)
	if sm < 0.55 || math.Abs(math.Log(zm)-math.Log(z)/2) > math.Abs(math.Log(z))*0.35 {
		return
	}
	s.Zoom, s.ZoomTo = 1, r2(z)
	if z < 1 {
		// Zooming out: start pushed in, end at full frame.
		s.Zoom, s.ZoomTo = r2(1/z), 1
	}
}

// punchIns marks hard cuts that only change the zoom on the same take: the
// first frame after the cut is the last frame before it, scaled.
func (a *Analyzer) punchIns(shots []ShotInfo) {
	for k := 1; k < len(shots); k++ {
		prev, cur := &shots[k-1], &shots[k]
		if prev.Join != "" || prev.Fit != "cover" || cur.Fit != "cover" {
			continue
		}
		i := a.lowIndex(cur.Start)
		if i < 1 {
			continue
		}
		before, after := a.lowGray[i-1].half(), a.lowGray[i].half()
		z, score, flat := zoomBetween(before, after, 0.6, 1.7)
		if score < 0.72 || score-flat < 0.12 || math.Abs(z-1) < 0.07 {
			continue
		}
		base := math.Max(1, prev.Zoom)
		if prev.ZoomTo > 0 {
			base = prev.ZoomTo
		}
		cur.Continue = true
		if z > 1 {
			cur.Zoom = r2(base * z)
		} else {
			// Punched out: the shot before was the closer one.
			cur.Zoom = r2(math.Max(1, base*z))
			if base*z < 1 {
				prev.Zoom = r2(prev.Zoom / (base * z))
			}
		}
		if cur.ZoomTo > 0 {
			cur.ZoomTo = r2(cur.ZoomTo * cur.Zoom)
		}
	}
}

// zoomBetween finds the scale z about the centre (with a small shift) that
// best turns a into b: b(p) = a(c + (p-c)/z). It returns z, the
// correlation there, and the best correlation without scaling.
func zoomBetween(a, b *Gray, lo, hi float64) (float64, float64, float64) {
	cx, cy := float64(a.W-1)/2, float64(a.H-1)/2
	mx, my := a.W/8, a.H/8
	var ref []float64
	var mean float64
	for y := my; y < b.H-my; y++ {
		for x := mx; x < b.W-mx; x++ {
			v := float64(b.at(x, y))
			ref = append(ref, v)
			mean += v
		}
	}
	ref = normalize(ref, mean/float64(len(ref)))
	score := func(z, dx, dy float64) float64 {
		v := make([]float64, 0, len(ref))
		var m float64
		for y := my; y < b.H-my; y++ {
			for x := mx; x < b.W-mx; x++ {
				s := a.sample(cx+(float64(x)-cx)/z+dx, cy+(float64(y)-cy)/z+dy)
				v = append(v, s)
				m += s
			}
		}
		return dot(ref, normalize(v, m/float64(len(v))))
	}
	best := func(z float64) float64 {
		b := -1.0
		for dy := -2.0; dy <= 2; dy++ {
			for dx := -2.0; dx <= 2; dx++ {
				b = math.Max(b, score(z, dx, dy))
			}
		}
		return b
	}
	flat := best(1)
	bz, bs := 1.0, flat
	for z := lo; z <= hi+1e-9; z *= 1.025 {
		if s := best(z); s > bs {
			bz, bs = z, s
		}
	}
	// Refine around the best step.
	for _, f := range []float64{0.99, 0.995, 1.005, 1.01} {
		if s := best(bz * f); s > bs {
			bz, bs = bz*f, s
		}
	}
	return bz, bs, flat
}

type layout struct {
	fit     string
	bg      string
	content Box
}

// detectLayout tells a full-frame shot from a picture shown whole inside
// bars (black or another flat colour) or on a blurred copy of itself.
func detectLayout(frames []*Gray) layout {
	out := layout{fit: "cover", content: Box{0, 0, 1, 1}}
	if len(frames) == 0 {
		return out
	}
	w, h := frames[0].W, frames[0].H
	rows, cols := make([]float64, h), make([]float64, w)
	rowDark, colDark := make([]float64, h), make([]float64, w)
	rowSharp, colSharp := make([]float64, h), make([]float64, w)
	for _, g := range frames {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := g.at(x, y)
				if v < 24 {
					rowDark[y]++
					colDark[x]++
				}
				if x > 0 && y > 0 {
					d := math.Abs(float64(v)-float64(g.at(x-1, y))) + math.Abs(float64(v)-float64(g.at(x, y-1)))
					rowSharp[y] += d
					colSharp[x] += d
				}
				rows[y] += float64(v)
				cols[x] += float64(v)
			}
		}
	}
	n := float64(len(frames))
	for y := range rowDark {
		rowDark[y] /= n * float64(w)
		rowSharp[y] /= n * float64(w)
	}
	for x := range colDark {
		colDark[x] /= n * float64(h)
		colSharp[x] /= n * float64(h)
	}
	if t, b, ok := bars(rowDark); ok {
		out.fit, out.bg = "contain", "black"
		out.content = Box{0, float64(t) / float64(h), 1, float64(b-t) / float64(h)}
		return out
	}
	if l, r, ok := bars(colDark); ok {
		out.fit, out.bg = "contain", "black"
		out.content = Box{float64(l) / float64(w), 0, float64(r-l) / float64(w), 1}
		return out
	}
	if t, b, ok := blurBands(rowSharp); ok {
		out.fit, out.bg = "contain", "blur"
		out.content = Box{0, float64(t) / float64(h), 1, float64(b-t) / float64(h)}
		return out
	}
	if l, r, ok := blurBands(colSharp); ok {
		out.fit, out.bg = "contain", "blur"
		out.content = Box{float64(l) / float64(w), 0, float64(r-l) / float64(w), 1}
	}
	return out
}

// bars finds dark bands at both ends of a profile (share of dark pixels
// per row): the picture is the stretch between them. A logo printed on a
// bar is allowed, so a bar row only has to be mostly dark.
func bars(dark []float64) (int, int, bool) {
	n := len(dark)
	isBar := func(v float64) bool { return v >= 0.7 }
	t := 0
	for t < n && isBar(dark[t]) {
		t++
	}
	b := n
	for b > t && isBar(dark[b-1]) {
		b--
	}
	// Allow a short logo stretch inside a bar: skip up to 12% of rows that
	// are not dark if dark rows follow.
	extend := func(i, step int) int {
		for j := i; j >= 0 && j < n && abs(j-i) <= n*12/100; j += step {
			if isBar(dark[j]) {
				k := j
				for k >= 0 && k < n && isBar(dark[k]) {
					k += step
				}
				if abs(k-i) > abs(j-i)*2 {
					return k
				}
			}
		}
		return i
	}
	if t > 0 {
		t = extend(t, 1)
	}
	if b < n {
		b = extend(b-1, -1) + 1
	}
	top, bottom := float64(t)/float64(n), float64(n-b)/float64(n)
	if top < 0.04 || bottom < 0.04 || top+bottom < 0.12 || b-t < n/5 {
		return 0, 0, false
	}
	// The picture itself must not be dark at its edges.
	if dark[min(n-1, t+1)] > 0.5 || dark[max(0, b-2)] > 0.5 {
		return 0, 0, false
	}
	return t, b, true
}

// blurBands finds soft bands at both ends of a sharpness profile around a
// sharp middle: a picture laid over a blurred, enlarged copy of itself.
func blurBands(sharp []float64) (int, int, bool) {
	n := len(sharp)
	var mids []float64
	for i := n / 3; i < 2*n/3; i++ {
		mids = append(mids, sharp[i])
	}
	ref := median(mids)
	if ref < 2 {
		return 0, 0, false
	}
	soft := func(v float64) bool { return v < 0.32*ref }
	t := 0
	for t < n/2 && soft(sharp[t]) {
		t++
	}
	b := n
	for b > n/2 && soft(sharp[b-1]) {
		b--
	}
	// Tolerate a few sharp rows (a logo, a line of text) inside a band.
	grow := func(i, step int) int {
		for {
			j, sharpRun := i, 0
			for j >= 0 && j < n && !soft(sharp[j]) && sharpRun < n/20 {
				j += step
				sharpRun++
			}
			if j < 0 || j >= n || sharpRun >= n/20 {
				return i
			}
			k := j
			for k >= 0 && k < n && soft(sharp[k]) {
				k += step
			}
			if abs(k-j) < 3 {
				return i
			}
			i = k
		}
	}
	t = grow(t, 1)
	b = grow(b-1, -1) + 1
	top, bottom := float64(t)/float64(n), float64(n-b)/float64(n)
	if top < 0.06 || bottom < 0.06 || math.Abs(top-bottom) > 0.12 || b-t < n/5 {
		return 0, 0, false
	}
	return t, b, true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
