package clone

import "math"

// classifyEntrance reads the first ~0.3 s of a text block on the full-rate
// stream and names the entrance the renderer already draws. Scale becomes
// pop, a drift into place becomes up, letters filling across the line
// become type, and a contrast ramp whose shape is already full becomes
// fade. A cut-in, where the letters are simply there, returns "" so the
// card holds still.
func (a *Analyzer) classifyEntrance(box Box, start, end float64) (string, float64) {
	if len(a.lowGray) < 6 || a.cfg.LowFPS < 8 || end-start < 0.45 {
		return "", 0
	}
	g0 := a.lowGray[0]
	rect := box.rect(g0.W, g0.H)
	if rect.W < 8 || rect.H < 6 {
		return "", 0
	}
	preT := start - 2/a.cfg.LowFPS
	if preT < 0 {
		return "", 0
	}
	settledT := math.Min(end-1/a.cfg.LowFPS, start+0.42)
	if settledT < start+0.28 {
		return "", 0
	}
	pre := a.lowGray[a.lowIndex(preT)]
	settled := a.lowGray[a.lowIndex(settledT)]
	mask := maskPixels(pre, settled, rect, 18)
	if len(mask) < 16 {
		return "", 0
	}
	// The picture around the card has to hold still. A cut or a zoom of
	// the whole frame moves the ring as much as the letters.
	inside := regionDiff(pre, settled, rect)
	ring := ringDiff(pre, settled, rect, max(2, rect.H/3))
	if inside < 12 || ring > inside*0.55 {
		return "", 0
	}
	final := meanAbsIdx(settled, pre, mask)
	if final < 16 {
		return "", 0
	}

	step := 1 / a.cfg.LowFPS
	var ss []inkSample
	for t := start; t <= start+0.32+1e-4; t += step {
		f := a.lowGray[a.lowIndex(t)]
		s := measureInk(f, pre, rect, mask, 12, final)
		s.t = t
		ss = append(ss, s)
	}
	if len(ss) < 4 {
		return "", 0
	}
	late := ss[len(ss)-1]
	first := -1
	for i, s := range ss {
		if s.contrast > 0.15 && s.n > 4 {
			first = i
			break
		}
	}
	if first < 0 {
		return "", 0
	}
	early := ss[first]
	// Hard cut: the whole shape is at full contrast on the first frame
	// that shows it.
	if early.contrast > 0.82 && early.w > 0.85 && early.h > 0.85 {
		return "", 0
	}
	doneBy := func(ok func(inkSample) bool) float64 {
		for _, s := range ss {
			if ok(s) {
				d := s.t - start + step
				if d < 0.12 {
					d = 0.12
				}
				return r2(math.Min(0.5, d))
			}
		}
		return 0.3
	}

	// Letters: the line is already full height and the ink grows from
	// one side.
	if early.h > 0.72 && early.w < 0.72 && late.w > 0.88 && late.h > 0.75 && late.w-early.w >= 0.22 {
		dLeft := math.Abs(late.left - early.left)
		dRight := math.Abs(late.right - early.right)
		anchor, grow := math.Min(dLeft, dRight), math.Max(dLeft, dRight)
		if grow > 0.12 && anchor <= grow*0.45 && math.Abs(late.h-early.h) < 0.28 {
			return "type", doneBy(func(s inkSample) bool { return s.w > 0.9 })
		}
	}
	// Scale: both axes grow, and the ink stays centred.
	if early.w < 0.78 && early.h < 0.78 && late.w > 0.85 && late.h > 0.85 &&
		late.w-early.w >= 0.18 && late.h-early.h >= 0.18 &&
		math.Abs(late.cx-0.5) < 0.22 && math.Abs(late.cy-0.5) < 0.22 &&
		math.Abs(late.cx-early.cx) < 0.22 && math.Abs(late.cy-early.cy) < 0.22 {
		return "pop", doneBy(func(s inkSample) bool { return s.w > 0.9 && s.h > 0.9 })
	}
	// Drift: the width is already the line, and the ink moves or fills
	// vertically into place (the renderer's "up").
	vert := math.Abs(late.cy - early.cy)
	horiz := math.Abs(late.cx - early.cx)
	fillsUp := late.h-early.h >= 0.2 && early.w > 0.75 && late.w > 0.8 && late.h > 0.8 && late.h-early.h > late.w-early.w
	drifts := vert >= 0.14 && vert > horiz*1.3 && early.w > 0.72 && late.w > 0.8 && early.h > 0.6 && late.h > 0.75
	if fillsUp || drifts {
		return "up", doneBy(func(s inkSample) bool { return s.h > 0.88 && s.contrast > 0.7 })
	}
	// Fade: the shape is full while the contrast climbs through the window.
	if early.w > 0.8 && early.h > 0.8 && late.contrast-early.contrast >= 0.28 {
		mid := 0
		for _, s := range ss {
			if s.contrast > 0.2 && s.contrast < 0.85 && s.w > 0.75 {
				mid++
			}
		}
		if mid >= 3 {
			return "fade", fadeDur(a, pre, mask, final, start)
		}
	}
	return "", 0
}

// fadeDur follows a contrast ramp a little past the geometry window, so a
// fade of about half a second keeps its length.
func fadeDur(a *Analyzer, pre *Gray, mask []int, final, start float64) float64 {
	step := 1 / a.cfg.LowFPS
	last := start + 0.25
	limit := math.Min(start+0.8, a.lowTime(len(a.lowGray)-1))
	for t := start; t <= limit+1e-6; t += step {
		c := meanAbsIdx(a.lowGray[a.lowIndex(t)], pre, mask) / final
		last = t
		if c >= 0.9 {
			break
		}
	}
	d := last - start + step
	if d < 0.12 {
		d = 0.12
	}
	return r2(math.Min(0.8, d))
}

type inkSample struct {
	t, contrast float64
	w, h        float64
	cx, cy      float64
	left, right float64
	n           int
}

func maskPixels(pre, settled *Gray, r Rect, thr int) []int {
	var out []int
	w := settled.W
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if iabs(int(settled.Pix[y*w+x])-int(pre.Pix[y*w+x])) >= thr {
				out = append(out, y*w+x)
			}
		}
	}
	return out
}

func measureInk(frame, pre *Gray, r Rect, mask []int, thr int, final float64) inkSample {
	var s inkSample
	if len(mask) == 0 || r.W < 1 || r.H < 1 || final <= 0 {
		return s
	}
	x0, y0 := r.W, r.H
	x1, y1 := 0, 0
	var sum, sx, sy float64
	for _, i := range mask {
		x, y := i%frame.W, i/frame.W
		d := iabs(int(frame.Pix[i]) - int(pre.Pix[i]))
		sum += float64(d)
		if d < thr {
			continue
		}
		s.n++
		lx, ly := x-r.X, y-r.Y
		if lx < x0 {
			x0 = lx
		}
		if ly < y0 {
			y0 = ly
		}
		if lx+1 > x1 {
			x1 = lx + 1
		}
		if ly+1 > y1 {
			y1 = ly + 1
		}
		sx += float64(lx)
		sy += float64(ly)
	}
	s.contrast = (sum / float64(len(mask))) / final
	if s.n == 0 {
		return s
	}
	s.w = float64(x1-x0) / float64(r.W)
	s.h = float64(y1-y0) / float64(r.H)
	s.cx = (sx / float64(s.n)) / float64(r.W)
	s.cy = (sy / float64(s.n)) / float64(r.H)
	s.left = float64(x0) / float64(r.W)
	s.right = float64(x1) / float64(r.W)
	return s
}

func meanAbsIdx(a, b *Gray, idx []int) float64 {
	if len(idx) == 0 {
		return 0
	}
	var s float64
	for _, i := range idx {
		s += float64(iabs(int(a.Pix[i]) - int(b.Pix[i])))
	}
	return s / float64(len(idx))
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
