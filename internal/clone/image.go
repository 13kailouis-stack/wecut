package clone

import (
	"math"
	"sort"
)

// Gray is an 8-bit luma picture, row by row.
type Gray struct {
	W, H int
	Pix  []uint8
}

// RGB is a 24-bit picture, three bytes per pixel, row by row.
type RGB struct {
	W, H int
	Pix  []uint8
}

// Rect is a box in pixels of whatever picture it belongs to.
type Rect struct {
	X, Y, W, H int
}

// Box is a rectangle in fractions of the frame (0..1), the unit every
// result is reported in so it does not depend on analysis sizes.
type Box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (b Box) cx() float64 { return b.X + b.W/2 }
func (b Box) cy() float64 { return b.Y + b.H/2 }

func (b Box) iou(o Box) float64 {
	x0, y0 := math.Max(b.X, o.X), math.Max(b.Y, o.Y)
	x1, y1 := math.Min(b.X+b.W, o.X+o.W), math.Min(b.Y+b.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	in := (x1 - x0) * (y1 - y0)
	return in / (b.W*b.H + o.W*o.H - in)
}

func (b Box) union(o Box) Box {
	x0, y0 := math.Min(b.X, o.X), math.Min(b.Y, o.Y)
	x1, y1 := math.Max(b.X+b.W, o.X+o.W), math.Max(b.Y+b.H, o.Y+o.H)
	return Box{x0, y0, x1 - x0, y1 - y0}
}

// grow widens a box by f of its height on every side, kept inside the frame.
func (b Box) grow(fx, fy float64) Box {
	x0, y0 := math.Max(0, b.X-fx), math.Max(0, b.Y-fy)
	x1, y1 := math.Min(1, b.X+b.W+fx), math.Min(1, b.Y+b.H+fy)
	return Box{x0, y0, x1 - x0, y1 - y0}
}

func (b Box) rect(w, h int) Rect {
	x0 := clampi(int(math.Floor(b.X*float64(w))), 0, w-1)
	y0 := clampi(int(math.Floor(b.Y*float64(h))), 0, h-1)
	x1 := clampi(int(math.Ceil((b.X+b.W)*float64(w))), x0+1, w)
	y1 := clampi(int(math.Ceil((b.Y+b.H)*float64(h))), y0+1, h)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

func (r Rect) box(w, h int) Box {
	return Box{float64(r.X) / float64(w), float64(r.Y) / float64(h), float64(r.W) / float64(w), float64(r.H) / float64(h)}
}

// GrayFromRGB converts with BT.709 weights.
func GrayFromRGB(p *RGB) *Gray {
	g := &Gray{p.W, p.H, make([]uint8, p.W*p.H)}
	for i := range g.Pix {
		r, gg, b := float64(p.Pix[3*i]), float64(p.Pix[3*i+1]), float64(p.Pix[3*i+2])
		g.Pix[i] = uint8(0.2126*r + 0.7152*gg + 0.0722*b + 0.5)
	}
	return g
}

func (g *Gray) at(x, y int) uint8 { return g.Pix[y*g.W+x] }

// sample reads with bilinear interpolation at fractional pixel coordinates,
// clamped to the edge.
func (g *Gray) sample(x, y float64) float64 {
	x = math.Max(0, math.Min(float64(g.W-1), x))
	y = math.Max(0, math.Min(float64(g.H-1), y))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, g.W-1), min(y0+1, g.H-1)
	fx, fy := x-float64(x0), y-float64(y0)
	a := float64(g.at(x0, y0))*(1-fx) + float64(g.at(x1, y0))*fx
	b := float64(g.at(x0, y1))*(1-fx) + float64(g.at(x1, y1))*fx
	return a*(1-fy) + b*fy
}

// half shrinks a picture to half size by averaging 2x2 blocks.
func (g *Gray) half() *Gray {
	w, h := g.W/2, g.H/2
	o := &Gray{w, h, make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := int(g.at(2*x, 2*y)) + int(g.at(2*x+1, 2*y)) + int(g.at(2*x, 2*y+1)) + int(g.at(2*x+1, 2*y+1))
			o.Pix[y*w+x] = uint8((s + 2) / 4)
		}
	}
	return o
}

func meanStd(pix []uint8) (float64, float64) {
	if len(pix) == 0 {
		return 0, 0
	}
	var s, s2 float64
	for _, v := range pix {
		f := float64(v)
		s += f
		s2 += f * f
	}
	n := float64(len(pix))
	m := s / n
	return m, math.Sqrt(math.Max(0, s2/n-m*m))
}

// absDiff is the mean absolute difference of two same-sized pictures.
func absDiff(a, b *Gray) float64 {
	var s int
	for i, v := range a.Pix {
		d := int(v) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		s += d
	}
	return float64(s) / float64(len(a.Pix))
}

// changeScore is how much the picture changed between two frames,
// measured on the half of a 4x6 grid that changed most: a picture shown
// inside bars or on a static background changes completely at a cut while
// most of the frame stays put.
func changeScore(a, b *Gray) float64 {
	const gx, gy = 4, 6
	cols, rows := gx, gy
	if a.W > a.H {
		cols, rows = gy, gx
	}
	var cells []float64
	for j := 0; j < rows; j++ {
		for i := 0; i < cols; i++ {
			r := Rect{i * a.W / cols, j * a.H / rows, (i+1)*a.W/cols - i*a.W/cols, (j+1)*a.H/rows - j*a.H/rows}
			cells = append(cells, regionDiff(a, b, r))
		}
	}
	sort.Float64s(cells)
	var s float64
	half := cells[len(cells)/2:]
	for _, v := range half {
		s += v
	}
	return s / float64(len(half))
}

// regionDiff is the mean absolute difference inside r.
func regionDiff(a, b *Gray, r Rect) float64 {
	var s, n int
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			d := int(a.at(x, y)) - int(b.at(x, y))
			if d < 0 {
				d = -d
			}
			s += d
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(s) / float64(n)
}

// ringDiff is the mean absolute difference in a band around r (r grown by
// pad pixels), not counting r itself: how much the picture behind a text
// box changes.
func ringDiff(a, b *Gray, r Rect, pad int) float64 {
	x0, y0 := max(0, r.X-pad), max(0, r.Y-pad)
	x1, y1 := min(a.W, r.X+r.W+pad), min(a.H, r.Y+r.H+pad)
	var s, n int
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
				continue
			}
			d := int(a.at(x, y)) - int(b.at(x, y))
			if d < 0 {
				d = -d
			}
			s += d
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(s) / float64(n)
}

// histogram of 32 luma bins, normalised to sum 1.
func histogram(g *Gray) [32]float64 {
	var h [32]float64
	for _, v := range g.Pix {
		h[v>>3]++
	}
	n := float64(len(g.Pix))
	for i := range h {
		h[i] /= n
	}
	return h
}

func histDist(a, b [32]float64) float64 {
	var s float64
	for i := range a {
		s += math.Abs(a[i] - b[i])
	}
	return s / 2
}

// sharpness is the mean gradient magnitude: it drops during blur, whip and
// zoom transitions.
func sharpness(g *Gray) float64 {
	var s int
	for y := 1; y < g.H-1; y++ {
		row := y * g.W
		for x := 1; x < g.W-1; x++ {
			gx := int(g.Pix[row+x+1]) - int(g.Pix[row+x-1])
			gy := int(g.Pix[row+x+g.W]) - int(g.Pix[row+x-g.W])
			if gx < 0 {
				gx = -gx
			}
			if gy < 0 {
				gy = -gy
			}
			s += gx + gy
		}
	}
	return float64(s) / float64((g.W-2)*(g.H-2))
}

// patch reads r out of g resampled to w x h, zero-mean and unit length, for
// comparing two looks of the same region with a dot product (NCC).
func patch(g *Gray, r Rect, w, h int) []float64 {
	out := make([]float64, w*h)
	var m float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := float64(r.X) + (float64(x)+0.5)*float64(r.W)/float64(w) - 0.5
			sy := float64(r.Y) + (float64(y)+0.5)*float64(r.H)/float64(h) - 0.5
			v := g.sample(sx, sy)
			out[y*w+x] = v
			m += v
		}
	}
	return normalize(out, m/float64(len(out)))
}

func normalize(v []float64, mean float64) []float64 {
	var n float64
	for i := range v {
		v[i] -= mean
		n += v[i] * v[i]
	}
	if n < 1e-9 {
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampf(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }
func r3(v float64) float64 { return math.Round(v*1000) / 1000 }
