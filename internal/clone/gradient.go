package clone

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// gradients finds colour fades laid over the footage: rows at the top or
// bottom of the frame that stay black whatever the shot shows, and brighten
// towards the middle. An edit can change its fade between sections, so shots
// are grouped by how far their black reaches. Rows under detected text do not
// count: letters on the black are not picture.
func (a *Analyzer) gradients(p *Plan) []spec.Gradient {
	type sample struct {
		start, end, at float64
		frame          *Gray
	}
	var shots []sample
	for _, s := range p.Shots {
		if s.Fit != "cover" || s.Dur() < 0.25 {
			continue
		}
		i := int(math.Round((s.Start + s.End) / 2 * a.cfg.LowFPS))
		if i >= 0 && i < len(a.lowGray) {
			shots = append(shots, sample{s.Start, s.End, (s.Start + s.End) / 2, a.lowGray[i]})
		}
	}
	if len(shots) < 3 {
		return nil
	}
	W, H := shots[0].frame.W, shots[0].frame.H
	rowsOf := func(f *Gray) []float64 {
		row := make([]float64, H)
		for y := 0; y < H; y++ {
			var sum float64
			for x := 0; x < W; x++ {
				sum += float64(f.Pix[y*W+x])
			}
			row[y] = sum / float64(W)
		}
		return row
	}
	// Rows holding a detected text or logo at a moment.
	maskAt := func(mask []bool, t float64) {
		for _, list := range [][]*Block{p.Blocks, p.Logos} {
			for _, bl := range list {
				if t < bl.Start || t > bl.End {
					continue
				}
				for y := int(bl.Box.Y*float64(H)) - 1; y <= int((bl.Box.Y+bl.Box.H)*float64(H))+1; y++ {
					if y >= 0 && y < H {
						mask[y] = true
					}
				}
			}
		}
	}
	var out []spec.Gradient
	for _, edge := range [][2]int{{H - 1, -1}, {0, 1}} {
		start, step := edge[0], edge[1]
		// How far from the edge a shot stays black.
		reach := func(k int) int {
			row, mask := rowsOf(shots[k].frame), make([]bool, H)
			maskAt(mask, shots[k].at)
			y := start
			for y >= 0 && y < H && (row[y] <= 18 || mask[y]) {
				y += step
			}
			if y < start {
				return start - y
			}
			return y - start
		}
		type group struct {
			from, to, reach int
		}
		var groups []group
		for k := range shots {
			r := reach(k)
			// A shot darker than the fade fits it; only one that reaches much
			// less far has a different fade.
			tol := H * 8 / 100
			if n := len(groups); n > 0 && r >= groups[n-1].reach-tol {
				groups[n-1].to = k
				if r <= groups[n-1].reach+tol {
					groups[n-1].reach = max(groups[n-1].reach, r)
				}
				continue
			}
			groups = append(groups, group{k, k, r})
		}
		// The group's own level, from nearly the brightest moment of each row.
		var found []spec.Gradient
		for gi, g := range groups {
			if g.to == g.from && shots[g.to].end-shots[g.from].start < 3 {
				continue
			}
			row := make([]float64, H)
			mask := make([]bool, H)
			var all [][]float64
			for k := g.from; k <= g.to; k++ {
				all = append(all, rowsOf(shots[k].frame))
				maskAt(mask, shots[k].at)
			}
			for y := 0; y < H; y++ {
				v := make([]float64, 0, len(all))
				for _, r := range all {
					v = append(v, r[y])
				}
				sort.Float64s(v)
				row[y] = v[len(v)*9/10]
			}
			var level []float64
			for y := H / 4; y <= H/2; y++ {
				if !mask[y] {
					level = append(level, row[y])
				}
			}
			if len(level) == 0 || median(level) < 40 {
				continue
			}
			f := fadeFrom(row, mask, start, step, median(level))
			if f == nil {
				continue
			}
			if gi > 0 {
				f.Start = r3(shots[g.from].start)
			}
			if gi < len(groups)-1 {
				f.End = spec.F(r3(shots[g.to].end))
			}
			// Nearly the same as the one before is the same fade. Otherwise they
			// change hands halfway through the transition between them, so the
			// picture is never darkened twice.
			if n := len(found); n > 0 {
				last := &found[n-1]
				if math.Abs(last.From-f.From) < 0.07 && math.Abs(last.To-f.To) < 0.04 {
					last.End = f.End
					continue
				}
				if last.End != nil && f.Start < *last.End {
					mid := r3((f.Start + *last.End) / 2)
					f.Start, last.End = mid, spec.F(mid)
				}
			}
			found = append(found, *f)
		}
		out = append(out, found...)
	}
	return out
}

// fadeFrom walks in from a frame edge: first the rows that are always dark,
// then the rows brightening until the picture's own level. Masked rows hold
// letters, not picture, and are walked over.
func fadeFrom(row []float64, mask []bool, edge, step int, plateau float64) *spec.Gradient {
	H := len(row)
	skip := func(y int) bool { return mask != nil && mask[y] }
	in := func(y int) bool { return y >= 0 && y < H }
	solid := edge
	for in(solid) && (row[solid] <= 18 || skip(solid)) {
		solid += step
	}
	dark := solid - edge
	if dark < 0 {
		dark = -dark
	}
	if dark < H/20 {
		return nil
	}
	// A fade is a straight line, so two points on it say where it starts and
	// ends: where the rows reach a quarter and a half of the picture's level.
	// Reading the ends themselves would catch the picture's own falloff, and
	// compression around black.
	quarter := solid
	for in(quarter) && (row[quarter] < 0.25*plateau || skip(quarter)) {
		quarter += step
	}
	half := quarter
	for in(half) && (row[half] < 0.5*plateau || skip(half)) {
		half += step
	}
	if !in(half) {
		return nil
	}
	full := quarter - (half - quarter)
	clear := full + 2*(half-full)
	span := clear - full
	if span < 0 {
		span = -span
	}
	if span < H/40 || !in(clear) || !in(full) {
		return nil
	}
	return &spec.Gradient{From: r3(float64(clear) / float64(H)), To: r3(float64(full) / float64(H)), Color: "#000000"}
}

// gradientsFrom applies a model's gradientN answers to what was measured.
func gradientsFrom(measured []spec.Gradient, reads map[string]Reading) []spec.Gradient {
	byN := map[int]*spec.Gradient{}
	for i := range measured {
		g := measured[i]
		byN[i+1] = &g
	}
	for id, rd := range reads {
		n, ok := strings.CutPrefix(id, "gradient")
		if !ok {
			continue
		}
		k, err := strconv.Atoi(n)
		if err != nil {
			continue
		}
		if rd.Drop {
			delete(byN, k)
			continue
		}
		g := byN[k]
		if g == nil {
			if rd.From == nil || rd.To == nil {
				continue
			}
			g = &spec.Gradient{Color: "#000000"}
			byN[k] = g
		}
		if rd.From != nil {
			g.From = *rd.From
		}
		if rd.To != nil {
			g.To = *rd.To
		}
		if rd.Color != "" {
			g.Color = rd.Color
		}
		if rd.Opacity != nil {
			g.Opacity = rd.Opacity
		}
	}
	var keys []int
	for k := range byN {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var out []spec.Gradient
	for _, k := range keys {
		out = append(out, *byN[k])
	}
	return out
}

// filtersFrom turns filterN answers into colour washes for a stretch of the
// video: measuring sees only grey, so a colour change is a model's to report.
func filtersFrom(reads map[string]Reading) []spec.Filter {
	var ids []string
	for id := range reads {
		if strings.HasPrefix(id, "filter") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []spec.Filter
	for _, id := range ids {
		rd := reads[id]
		if rd.Drop {
			continue
		}
		f := spec.Filter{Name: rd.Name, Color: rd.Color, Amount: rd.Opacity, End: rd.End}
		if f.Name == "" {
			f.Name = "color"
		}
		if rd.Start != nil {
			f.Start = *rd.Start
		}
		out = append(out, f)
	}
	return out
}
