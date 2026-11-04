package clone

import "testing"

// A hook card set as dark words on white boxes, one per line, over footage,
// with a green quote badge above it: the shape TikTok's text tool makes.
func lineBoxFrame() (*RGB, []Box, Box) {
	const W, H = 540, 960
	f := &RGB{W: W, H: H, Pix: make([]uint8, 3*W*H)}
	set := func(x, y int, c rgb) {
		i := 3 * (y*W + x)
		f.Pix[i], f.Pix[i+1], f.Pix[i+2] = uint8(c.r), uint8(c.g), uint8(c.b)
	}
	// Footage: a busy, mid-dark picture.
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			v := float64(40 + (x*7+y*3)%50)
			set(x, y, rgb{v, v * 3 / 4, v / 2})
		}
	}
	fill := func(x0, y0, x1, y1 int, c rgb) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				set(x, y, c)
			}
		}
	}
	white, ink, green := rgb{253, 254, 253}, rgb{36, 36, 36}, rgb{112, 191, 168}
	// Three lines, boxes of different widths, 8 px of footage between them.
	widths := []int{300, 440, 360}
	var lines []Box
	top := 660
	for _, w := range widths {
		x0, x1 := 40, 40+w
		fill(x0, top, x1, top+34, white)
		// Letters: strokes between the capitals' top and the baseline.
		for x := x0 + 14; x < x1-14; x += 11 {
			fill(x, top+7, x+6, top+29, ink)
		}
		// The tracker's rect sits a few pixels off the box, as it does.
		lines = append(lines, Box{float64(x0+5) / W, float64(top+3) / H, float64(w-10) / W, 28.0 / H})
		top += 42
	}
	// The badge: a wide green tag with two white marks in it.
	fill(46, 600, 112, 640, green)
	fill(62, 610, 74, 628, white)
	fill(80, 610, 92, 628, white)
	mark := Box{60.0 / W, 609.0 / H, 34.0 / W, 20.0 / H}
	return f, lines, mark
}

func TestMeasureLineBoxes(t *testing.T) {
	f, lines, _ := lineBoxFrame()
	bl := &Block{Lines: lines, Box: lines[0].union(lines[1]).union(lines[2])}
	l := measureBlock(bl, f, nil)
	if l.Background == "" || dist(hexRGB(t, l.Background), rgb{253, 254, 253}) > 30 {
		t.Fatalf("background %q, want the white of the boxes", l.Background)
	}
	if dist(hexRGB(t, l.Color), rgb{36, 36, 36}) > 40 {
		t.Fatalf("color %q, want the dark ink", l.Color)
	}
	if l.Card {
		t.Fatal("one box per line, not one card")
	}
	if l.Radius != 0 {
		t.Fatalf("radius %v, want square corners", l.Radius)
	}
}

func TestBadgeAttachAndMeasure(t *testing.T) {
	f, lines, mark := lineBoxFrame()
	bl := &Block{Start: 0, End: 5, Lines: lines, Box: lines[0].union(lines[1]).union(lines[2]), Kind: "text"}
	// A stray mark beside the card shown for part of the time, and the badge.
	stray := &Block{Start: 1, End: 4, Box: Box{0.75, 0.64, 0.06, 0.02}, Lines: []Box{{0.75, 0.64, 0.06, 0.02}}}
	badge := &Block{Start: 0.4, End: 5, Box: mark, Lines: []Box{mark}}
	attachBadges([]*Block{bl}, []*Block{stray, badge})
	if bl.Badge == nil || *bl.Badge != mark {
		t.Fatalf("badge %+v, want the mark above the first line", bl.Badge)
	}
	b := measureBadge(*bl.Badge, f)
	if b == nil {
		t.Fatal("badge not measured")
	}
	if dist(hexRGB(t, b.Color), rgb{112, 191, 168}) > 30 || dist(hexRGB(t, b.Glyph), rgb{253, 254, 253}) > 40 {
		t.Fatalf("badge colours %s / %s", b.Color, b.Glyph)
	}
	// The whole tag, wider than tall, not just the marks in it.
	if b.W < 0.11 || b.W > 0.14 || b.H < 0.035 || b.H > 0.048 {
		t.Fatalf("badge size %.3f x %.3f, want about 0.122 x 0.042", b.W, b.H)
	}
}

func hexRGB(t *testing.T, s string) rgb {
	t.Helper()
	var c rgb
	if len(s) != 7 {
		t.Fatalf("not a colour: %q", s)
	}
	v := func(i int) uint8 {
		n := 0
		for _, ch := range s[i : i+2] {
			n *= 16
			switch {
			case ch >= '0' && ch <= '9':
				n += int(ch - '0')
			case ch >= 'A' && ch <= 'F':
				n += int(ch-'A') + 10
			case ch >= 'a' && ch <= 'f':
				n += int(ch-'a') + 10
			}
		}
		return uint8(n)
	}
	c.r, c.g, c.b = float64(v(1)), float64(v(3)), float64(v(5))
	return c
}
