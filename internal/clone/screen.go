package clone

import (
	"math"
	"sort"
)

// A screenshot or picture pasted over the footage (a phone screen beside
// the speaker, a chat, a product shot) is a big rectangle that holds still
// to the pixel while the footage around it moves. It is read from the low
// stream: in windows of two seconds, the pixels that did not change while
// the rest of the picture did are joined into regions, and a region that is
// nearly a rectangle, has something in it, floats inside the frame and is
// not a text card already found becomes a picture slot.

const (
	screenWin = 8 // steps of 0.25 s in a window
	screenHop = 4
)

// screens finds the pasted pictures, after the text cards and logos.
func (a *Analyzer) screens(p *Plan) []*Block {
	if len(a.lowGray) < 4 {
		return nil
	}
	W, H := a.lowGray[0].W, a.lowGray[0].H
	step := max(1, int(math.Round(a.cfg.LowFPS*0.25)))
	nsteps := (len(a.lowGray) - 1) / step
	end := p.ContentEnd
	type seen struct {
		s, e float64
		b    Box
	}
	var found []seen
	still := make([]uint16, W*H)
	moved := make([]uint16, W*H)
	for w0 := 0; w0+screenWin <= nsteps; w0 += screenHop {
		for i := range still {
			still[i], moved[i] = 0, 0
		}
		m := 0
		for k := w0; k < w0+screenWin; k++ {
			f, g := a.lowGray[k*step], a.lowGray[(k+1)*step]
			if f.W != W || g.W != W {
				continue
			}
			n := 0
			for i := range f.Pix {
				if d := abs(int(f.Pix[i]) - int(g.Pix[i])); d > 8 {
					n++
				}
			}
			// While the whole picture holds still nothing can be told apart.
			if float64(n) < 0.12*float64(W*H) {
				continue
			}
			m++
			for i := range f.Pix {
				d := abs(int(f.Pix[i]) - int(g.Pix[i]))
				if d <= 2 {
					still[i]++
				} else if d > 4 {
					moved[i]++
				}
			}
		}
		if m < screenWin/2 {
			continue
		}
		s := a.lowTime(w0 * step)
		e := a.lowTime((w0 + screenWin) * step)
		if s >= end {
			break
		}
		mid := a.lowGray[(w0+screenWin/2)*step]
		for _, r := range stillRegions(still, W, H, uint16(math.Ceil(0.85*float64(m)))) {
			if a.screenLike(r, mid, moved, m) {
				found = append(found, seen{s, math.Min(e, end), r.box(W, H)})
			}
		}
	}
	// Join the windows that saw one picture in one place.
	sort.Slice(found, func(i, j int) bool { return found[i].s < found[j].s })
	type span struct {
		s, e float64
		sum  Box
		n    int
		last Box
	}
	var spans []*span
	for _, f := range found {
		var hit *span
		for _, sp := range spans {
			if f.s <= sp.e+0.05 && sp.last.iou(f.b) >= 0.6 {
				hit = sp
				break
			}
		}
		if hit == nil {
			hit = &span{s: f.s}
			spans = append(spans, hit)
		}
		hit.e = math.Max(hit.e, f.e)
		hit.sum = Box{hit.sum.X + f.b.X, hit.sum.Y + f.b.Y, hit.sum.W + f.b.W, hit.sum.H + f.b.H}
		hit.n++
		hit.last = f.b
	}
	var out []*Block
	for _, sp := range spans {
		n := float64(sp.n)
		b := Box{sp.sum.X / n, sp.sum.Y / n, sp.sum.W / n, sp.sum.H / n}
		// Out to where the picture really comes and goes: over footage that
		// itself holds still, the windows cannot tell it apart.
		sp.s, sp.e = a.screenEnds(b, sp.s, sp.e, end)
		if sp.e-sp.s < 2.5 {
			continue
		}
		// A text card or logo already found is not a picture.
		card := false
		for _, bl := range append(append([]*Block{}, p.Blocks...), p.Logos...) {
			over := math.Min(bl.End, sp.e) - math.Max(bl.Start, sp.s)
			if over > 0.5*(sp.e-sp.s) && b.overlap(bl.Box) >= 0.4 {
				card = true
				break
			}
		}
		if card {
			continue
		}
		out = append(out, &Block{Start: r2(sp.s), End: r2(sp.e), Box: b, Lines: []Box{b}, Kind: "screen"})
	}
	for i, bl := range out {
		bl.ID = "screen"
		if i > 0 {
			bl.ID = "screen" + itoa(i+1)
		}
	}
	return out
}

// stillRegions joins the pixels still in at least need steps into regions.
func stillRegions(still []uint16, W, H int, need uint16) []Rect {
	label := make([]bool, W*H)
	var out []Rect
	stack := make([]int, 0, 256)
	for i := range still {
		if still[i] < need || label[i] {
			continue
		}
		label[i] = true
		stack = append(stack[:0], i)
		x0, y0, x1, y1, n := W, H, 0, 0, 0
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := j%W, j/W
			n++
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			for _, k := range [4]int{j - 1, j + 1, j - W, j + W} {
				if k < 0 || k >= len(still) || label[k] || still[k] < need {
					continue
				}
				if (k == j-1 && x == 0) || (k == j+1 && x == W-1) {
					continue
				}
				label[k] = true
				stack = append(stack, k)
			}
		}
		r := Rect{x0, y0, x1 - x0 + 1, y1 - y0 + 1}
		// Nearly a rectangle: the region fills most of its box.
		if r.W*5 >= W && r.H*8 >= H && n*10 >= r.W*r.H*7 {
			out = append(out, r)
		}
	}
	return out
}

// screenLike checks a still region: not the whole frame, touching at most
// one edge of it, with something in it (a plain band does not count), and
// moving footage right around it.
func (a *Analyzer) screenLike(r Rect, mid *Gray, moved []uint16, m int) bool {
	W, H := mid.W, mid.H
	if r.W*100 >= W*95 || r.H*100 >= H*95 {
		return false
	}
	edges := 0
	if r.X <= 0 {
		edges++
	}
	if r.Y <= 0 {
		edges++
	}
	if r.X+r.W >= W {
		edges++
	}
	if r.Y+r.H >= H {
		edges++
	}
	if edges > 1 {
		return false
	}
	tex := 0
	for y := max(1, r.Y); y < min(H-1, r.Y+r.H); y++ {
		for x := max(1, r.X); x < min(W-1, r.X+r.W); x++ {
			i := y*W + x
			if abs(int(mid.Pix[i+1])-int(mid.Pix[i-1]))+abs(int(mid.Pix[i+W])-int(mid.Pix[i-W])) >= 12 {
				tex++
			}
		}
	}
	if tex*20 < r.W*r.H {
		return false
	}
	ring, ringMoved := 0, 0
	pad := 2
	for y := r.Y - pad; y < r.Y+r.H+pad; y++ {
		for x := r.X - pad; x < r.X+r.W+pad; x++ {
			if x < 0 || y < 0 || x >= W || y >= H || (x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H) {
				continue
			}
			ring++
			if int(moved[y*W+x])*2 >= m {
				ringMoved++
			}
		}
	}
	return ring > 0 && ringMoved*10 >= ring*3
}

// aspectNote says the shape of the original picture, so one of the same
// shape can be chosen: ", about 9:16".
func aspectNote(b Box, W, H int) string {
	if W <= 0 || H <= 0 || b.H <= 0 {
		return ""
	}
	ratio := b.W * float64(W) / (b.H * float64(H))
	best, bw, bh := math.Inf(1), 1, 1
	for _, c := range [][2]int{{1, 1}, {4, 5}, {3, 4}, {2, 3}, {9, 16}, {1, 2}, {5, 4}, {4, 3}, {3, 2}, {16, 9}} {
		if d := math.Abs(math.Log(ratio * float64(c[1]) / float64(c[0]))); d < best {
			best, bw, bh = d, c[0], c[1]
		}
	}
	return ", about " + itoa(bw) + ":" + itoa(bh)
}

// screenEnds moves the ends of a screen out to where its picture really
// comes and goes: a window counts only when the screen showed through most
// of it, so the windows found start late and end early.
func (a *Analyzer) screenEnds(b Box, s, e, end float64) (float64, float64) {
	W, H := a.lowGray[0].W, a.lowGray[0].H
	r := b.grow(-0.1*b.W, -0.1*b.H).rect(W, H)
	// The same picture: most of it unchanged. Words laid over it, or a level
	// meter in a screen recording, change a part.
	same := func(ref float64, t float64) bool {
		f, g := a.lowGray[a.lowIndex(ref)], a.lowGray[a.lowIndex(t)]
		n, diff := 0, 0
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				n++
				if abs(int(f.at(x, y))-int(g.at(x, y))) > 12 {
					diff++
				}
			}
		}
		return n > 0 && diff*100 <= n*30
	}
	inS, inE := s+math.Min(0.5, (e-s)/4), e-math.Min(0.5, (e-s)/4)
	for s > 0 && same(inS, s-1/a.cfg.LowFPS) {
		s = math.Max(0, s-1/a.cfg.LowFPS)
	}
	for e < end && same(inE, e) {
		e = math.Min(end, e+1/a.cfg.LowFPS)
	}
	return s, e
}
