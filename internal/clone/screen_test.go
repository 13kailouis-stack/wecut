package clone

import "testing"

// A phone screen pasted over handheld footage from 2 s to 6 s, and a plain
// dark band along the top that never moves either.
func screenClip(pasted bool) *Analyzer {
	const W, H = 72, 128
	a := New(Config{W: W, H: H, Duration: 8, LowFPS: 30, HighFPS: 6})
	seed := uint32(3)
	for k := 0; k < 240; k++ {
		g := &Gray{W: W, H: H, Pix: make([]uint8, W*H)}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				seed = seed*1664525 + 1013904223
				v := 60 + int(seed>>25) // footage: changes every frame
				if y < 10 {
					v = 8
				}
				g.Pix[y*W+x] = uint8(v)
			}
		}
		if t := float64(k) / 30; pasted && t >= 2 && t < 6 {
			for y := 60; y < 110; y++ {
				for x := 10; x < 40; x++ {
					v := 235
					if (y/4)%2 == 0 && x%5 != 0 {
						v = 90 // lines of the screen's text
					}
					g.Pix[y*W+x] = uint8(v)
				}
			}
		}
		a.AddLow(g)
	}
	return a
}

func TestScreens(t *testing.T) {
	a := screenClip(true)
	got := a.screens(&Plan{ContentEnd: 8})
	if len(got) != 1 {
		t.Fatalf("found %d screens, want 1: %+v", len(got), got)
	}
	s := got[0]
	if s.Start < 1.5 || s.Start > 2.6 || s.End < 5.4 || s.End > 6.6 {
		t.Fatalf("screen %v..%v, want about 2..6", s.Start, s.End)
	}
	want := Box{10.0 / 72, 60.0 / 128, 30.0 / 72, 50.0 / 128}
	if s.Box.iou(want) < 0.7 {
		t.Fatalf("screen box %+v, want about %+v", s.Box, want)
	}
	// The text card already found there is not a picture.
	if got := a.screens(&Plan{ContentEnd: 8, Blocks: []*Block{{Start: 2, End: 6, Box: want}}}); len(got) != 0 {
		t.Fatalf("a text card read as a screen: %+v", got)
	}
	if got := screenClip(false).screens(&Plan{ContentEnd: 8}); len(got) != 0 {
		t.Fatalf("footage with a dark band has screens: %+v", got)
	}
}

func TestAspectNote(t *testing.T) {
	if s := aspectNote(Box{0, 0, 0.5, 0.5 * 9 / 16 * 1080 / 1920 * 16 / 9}, 1080, 1920); s != ", about 1:1" {
		t.Fatalf("got %q", s)
	}
	if s := aspectNote(Box{0, 0, 0.45, 0.45 * 16 / 9 * 1080 / 1920}, 1080, 1920); s != ", about 9:16" {
		t.Fatalf("got %q", s)
	}
}
