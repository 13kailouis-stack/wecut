package clone

import (
	"math"
	"testing"
)

// frame paints a w x h grey picture: a moving pattern of the given seed, and
// optionally dark bars at the top and bottom (a picture shown whole).
func frame(w, h, seed, i int, bars bool, white float64) *Gray {
	g := &Gray{w, h, make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 128 + 90*math.Sin(float64(x*(seed+2)+i)/7)*math.Cos(float64(y*(seed+3))/9+float64(seed))
			if bars && (y < h/4 || y >= h*3/4) {
				v = 6
			}
			v = v*(1-white) + 255*white
			g.Pix[y*w+x] = uint8(math.Max(0, math.Min(255, v)))
		}
	}
	return g
}

func TestEventsShotsAndFraming(t *testing.T) {
	w, h := LowSize(1080, 1920)
	a := New(Config{W: 1080, H: 1920, Duration: 6, LowFPS: 30, HighFPS: 6})
	for i := 0; i < 180; i++ {
		var f *Gray
		switch {
		case i < 60: // shot 1
			f = frame(w, h, 1, i, false, 0)
		case i < 120: // shot 2, inside bars, flashing white into shot 3
			white := 0.0
			if i >= 114 {
				white = math.Min(1, float64(i-113)/5)
			}
			f = frame(w, h, 5, i, true, white)
		default:
			white := 0.0
			if i < 126 {
				white = 1 - float64(i-119)/7
			}
			f = frame(w, h, 9, i, false, white)
		}
		a.AddLow(f)
	}
	p := a.Analyze()
	if len(p.Events) != 2 {
		t.Fatalf("events %+v", p.Events)
	}
	if e := p.Events[0]; e.Kind != "cut" || math.Abs(e.At-2) > 0.04 {
		t.Errorf("cut %+v", e)
	}
	if e := p.Events[1]; e.Kind != "flash" || e.Start < 3.6 || e.End > 4.4 {
		t.Errorf("flash %+v", e)
	}
	if len(p.Shots) != 3 {
		t.Fatalf("shots %+v", p.Shots)
	}
	if s := p.Shots[1]; s.Fit != "contain" || s.Bg != "black" || math.Abs(s.Content.H-0.5) > 0.06 || s.Join != "flash" {
		t.Errorf("shot 2 %+v", s)
	}
	if p.Shots[0].Fit != "cover" || p.Shots[2].Fit != "cover" {
		t.Errorf("framing %+v %+v", p.Shots[0], p.Shots[2])
	}
	// The template keeps the original's length: shot lengths minus joins.
	total := 0.0
	for _, s := range p.Shots {
		total += s.Dur() - s.JoinDur
	}
	if math.Abs(total-6) > 0.05 {
		t.Errorf("shots add up to %.3f s", total)
	}
}

func TestZoomBetween(t *testing.T) {
	a := frame(72, 128, 3, 0, false, 0)
	// b is a scaled 1.15x about the centre.
	b := &Gray{72, 128, make([]uint8, 72*128)}
	for y := 0; y < 128; y++ {
		for x := 0; x < 72; x++ {
			b.Pix[y*72+x] = uint8(a.sample(35.5+(float64(x)-35.5)/1.15, 63.5+(float64(y)-63.5)/1.15))
		}
	}
	z, score, flat := zoomBetween(a.half(), b.half(), 0.8, 1.3)
	if math.Abs(z-1.15) > 0.03 || score < 0.9 || score <= flat {
		t.Errorf("zoom %.3f score %.2f flat %.2f", z, score, flat)
	}
}

func TestReadableAndSize(t *testing.T) {
	for s, want := range map[string]bool{"FLOX NEWS": true, "aa - La -_-": false, "> ls": false, "081120224589": true, "|": false} {
		if readable(s) != want {
			t.Errorf("readable(%q) != %v", s, want)
		}
	}
	if !isUpper("SATU ASIA SAMPAI IRI!") || isUpper("Tolak Ngantor") {
		t.Error("isUpper")
	}
}
