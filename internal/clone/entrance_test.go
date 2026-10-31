package clone

import "testing"

// entranceClip is one second of a still grey frame with a white card that
// comes in over frames 6..14 (0.2 s to 0.5 s at 30 fps).
func entranceClip(paint func(g *Gray, frame int)) *Analyzer {
	a := New(Config{W: 100, H: 100, Duration: 1.2, LowFPS: 30, HighFPS: 6})
	for i := 0; i < 36; i++ {
		g := &Gray{100, 100, make([]uint8, 100*100)}
		for p := range g.Pix {
			g.Pix[p] = 30
		}
		paint(g, i)
		a.AddLow(g)
	}
	return a
}

func paintBox(g *Gray, x, y, w, h int, v uint8) {
	for yy := y; yy < y+h; yy++ {
		if yy < 0 || yy >= g.H {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx < 0 || xx >= g.W {
				continue
			}
			g.Pix[yy*g.W+xx] = v
		}
	}
}

func TestClassifyEntrance(t *testing.T) {
	box := Box{0.2, 0.3, 0.5, 0.2} // 50×20 pixels at (20,30)
	full := func(g *Gray, v uint8) { paintBox(g, 20, 30, 50, 20, v) }
	anim := func(frame int) (int, bool) {
		if frame < 6 {
			return 0, false
		}
		k := frame - 6
		if k > 8 {
			k = 8
		}
		return k, true
	}
	cases := []struct {
		name  string
		paint func(*Gray, int)
		want  string
	}{
		{"pop", func(g *Gray, frame int) {
			k, ok := anim(frame)
			if !ok {
				return
			}
			sc := 0.35 + 0.65*float64(k)/8
			rw, rh := int(50*sc+0.5), int(20*sc+0.5)
			paintBox(g, 20+(50-rw)/2, 30+(20-rh)/2, rw, rh, 220)
		}, "pop"},
		{"type", func(g *Gray, frame int) {
			k, ok := anim(frame)
			if !ok {
				return
			}
			rw := int(50 * (0.2 + 0.8*float64(k)/8))
			if rw < 1 {
				rw = 1
			}
			paintBox(g, 20, 30, rw, 20, 220)
		}, "type"},
		{"up", func(g *Gray, frame int) {
			k, ok := anim(frame)
			if !ok {
				return
			}
			rh := int(20 * (0.25 + 0.75*float64(k)/8))
			if rh < 1 {
				rh = 1
			}
			paintBox(g, 20, 30+(20-rh), 50, rh, 220)
		}, "up"},
		{"fade", func(g *Gray, frame int) {
			k, ok := anim(frame)
			if !ok {
				return
			}
			full(g, uint8(30+(220-30)*k/8))
		}, "fade"},
		{"cut", func(g *Gray, frame int) {
			if frame >= 6 {
				full(g, 220)
			}
		}, ""},
		{"camera", func(g *Gray, frame int) {
			// The whole frame changes with the card: not a text entrance.
			if frame >= 6 {
				for p := range g.Pix {
					g.Pix[p] = 180
				}
				full(g, 250)
			}
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := entranceClip(c.paint)
			kind, dur := a.classifyEntrance(box, 0.2, 1.1)
			if kind != c.want {
				t.Fatalf("kind %q, want %q (dur %.2f)", kind, c.want, dur)
			}
			if kind != "" && (dur < 0.12 || dur > 0.8) {
				t.Fatalf("duration %.2f", dur)
			}
			// A real entrance takes more than one frame. 0.12 is the floor
			// used when the measure fails.
			if kind != "" && dur <= 0.12 {
				t.Fatalf("duration %.2f looks unmeasured", dur)
			}
		})
	}
}

func TestRefineTimingStoresEntrance(t *testing.T) {
	a := entranceClip(func(g *Gray, frame int) {
		if frame < 6 {
			return
		}
		k := frame - 6
		if k > 8 {
			k = 8
		}
		sc := 0.35 + 0.65*float64(k)/8
		rw, rh := int(50*sc+0.5), int(20*sc+0.5)
		paintBox(g, 20+(50-rw)/2, 30+(20-rh)/2, rw, rh, 220)
	})
	bl := &Block{Box: Box{0.2, 0.3, 0.5, 0.2}, Start: 0.25, End: 1.05, Kind: "text"}
	a.refineTiming(bl, nil, 1.2)
	if bl.In != "pop" || bl.InDur <= 0.12 {
		t.Fatalf("in %q dur %.2f (start %.2f end %.2f)", bl.In, bl.InDur, bl.Start, bl.End)
	}
}
