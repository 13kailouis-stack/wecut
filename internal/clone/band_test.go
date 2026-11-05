package clone

import "testing"

// Pale words in a thin outline over a pale screen: their edges are too faint
// for the line finder, but reading the band's strip again finds each word.
func TestBandWordsFaint(t *testing.T) {
	const W, H = 288, 512
	a := New(Config{W: W, H: H, Duration: 6, LowFPS: 6, HighFPS: 6})
	for k := 0; k < 36; k++ {
		g := &Gray{W: W, H: H, Pix: make([]uint8, W*H)}
		for i := range g.Pix {
			g.Pix[i] = 222
		}
		// A new word every 6 frames, of its own width and letters.
		word := k / 6
		n := 4 + word%3
		x0 := W/2 - n*7
		for l := 0; l < n; l++ {
			lx := x0 + l*14 + (word*3)%5
			for y := 396; y < 412; y++ {
				for x := lx; x < lx+9; x++ {
					v := uint8(240)
					if x == lx || x == lx+8 {
						v = 200 // the thin outline
					}
					g.Pix[y*W+x] = v
				}
			}
		}
		a.AddHigh(g)
	}
	cands, _, _ := findLines(&Gray{W: W, H: H, Pix: make([]uint8, W*H)})
	if len(cands) != 0 {
		t.Fatalf("a blank frame has lines: %v", cands)
	}
	if strong := a.debugTracks(); strong != 0 {
		t.Fatalf("the line finder saw %d tracks; the test wants words it cannot see", strong)
	}
	words := a.bandWords(0.73, 0.84, 0, 6)
	if len(words) < 5 {
		t.Fatalf("found %d words, want 6: %+v", len(words), words)
	}
	for _, w := range words {
		if w.e-w.s > 1.2 {
			t.Fatalf("word %+v lasts too long: words that took each other's place were joined", w)
		}
	}
}

func (a *Analyzer) debugTracks() int { return len(a.tracks) }

func TestCarryBand(t *testing.T) {
	words := []piece{
		{1, 1.5, Box{}}, // far before, alone
		{20.2, 21, Box{}}, {21.5, 22, Box{}}, {24.5, 25.5, Box{}},
		{31, 32, Box{}}, // alone after a pause of 5.5 s: not carried
	}
	s0, s1, used := carryBand(5, 20, words)
	if s0 != 5 || s1 != 25.5 || len(used) != 3 {
		t.Fatalf("band %v..%v with %d words, want 5..25.5 with 3", s0, s1, len(used))
	}
	// One word past the end is no more than a flicker.
	if _, s1, _ := carryBand(5, 20, words[1:2]); s1 != 20 {
		t.Fatalf("one word carried the band to %v", s1)
	}
	// Past a long gap, a run of words that is captions by itself.
	run := []piece{{33, 34, Box{}}, {34, 35, Box{}}, {35, 36, Box{}}, {36, 37, Box{}}, {37.2, 38, Box{}}}
	if _, s1, _ := carryBand(5, 22.5, run); s1 != 38 {
		t.Fatalf("band end %v, want the later run's end 38", s1)
	}
	// The same words before the band carry its start back.
	if s0, _, _ := carryBand(45, 50, run); s0 != 33 {
		t.Fatalf("band start %v, want 33", s0)
	}
	if _, s1, _ := carryBand(5, 22.5, run[:3]); s1 != 22.5 {
		t.Fatalf("three words past a long gap carried the band to %v", s1)
	}
}

// Laid-over words hold still to the pixel; a shirt's print in footage keeps
// the noise of the camera.
func TestInsideNoise(t *testing.T) {
	const W, H = 72, 128
	a := New(Config{W: W, H: H, Duration: 3, LowFPS: 30, HighFPS: 6})
	seed := uint32(7)
	for k := 0; k < 90; k++ {
		g := &Gray{W: W, H: H, Pix: make([]uint8, W*H)}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				seed = seed*1664525 + 1013904223
				v := 120 + int(seed>>27) // noise of 0..31
				if y >= 20 && y < 30 {
					v = 240 // the laid-over line
				}
				g.Pix[y*W+x] = uint8(v)
			}
		}
		a.AddLow(g)
	}
	if n := a.insideNoise(Box{0.1, 20.0 / H, 0.8, 10.0 / H}, 0, 3); n > 0.5 {
		t.Fatalf("laid-over line noise %.2f, want about 0", n)
	}
	if n := a.insideNoise(Box{0.1, 60.0 / H, 0.8, 10.0 / H}, 0, 3); n <= 3 {
		t.Fatalf("footage noise %.2f, want more than 3", n)
	}
}
