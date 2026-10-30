package clone

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Look is how a piece of text is drawn, read from full-size frames.
type Look struct {
	Color      string  `json:"color"`
	Stroke     string  `json:"stroke,omitempty"`
	Background string  `json:"background,omitempty"`
	BgOpacity  float64 `json:"background_opacity,omitempty"`
	Card       bool    `json:"card,omitempty"`     // one box behind all lines
	BoxArea    *Box    `json:"box_area,omitempty"` // the box itself
	Emphasis   string  `json:"emphasis,omitempty"` // a second text colour, e.g. red key words
	Size       float64 `json:"size"`               // font size in px at 1080 (short side)
	Glyph      float64 `json:"glyph"`              // measured line height, fraction of the frame
	// Per line, as fractions of the frame: top of the capitals, the
	// baseline, and the left and right ends of the letters.
	CapTop []float64 `json:"cap_top,omitempty"`
	Base   []float64 `json:"base,omitempty"`
	Left   []float64 `json:"left,omitempty"`
	Right  []float64 `json:"right,omitempty"`
	Italic bool      `json:"italic,omitempty"`
	Heavy  bool      `json:"heavy,omitempty"` // thick strokes for its size
	Weight float64   `json:"weight"`          // mean run through a letter / letter height
	Align  string    `json:"align"`
	Font   string    `json:"font"`
}

// Crop is a piece of a full-size frame worth reading: the words of a text
// block or a caption sample. Image is the plain crop; OCR is the same area
// made black-on-white for a text recogniser.
type Crop struct {
	ID    string  `json:"id"`
	Kind  string  `json:"kind"` // text, caption, logo
	At    float64 `json:"at"`
	Rect  Rect    `json:"rect"` // in pixels of the full-size frame
	Image *RGB    `json:"-"`
	OCR   *Gray   `json:"-"`
}

// planStills chooses the full-size frames Measure needs: two per text block
// and logo (to tell the text from what is behind it), and a few caption
// words.
func (a *Analyzer) planStills(p *Plan) {
	var times []float64
	add := func(id string, ts ...float64) {
		for _, t := range ts {
			t = r2(clampf(t, 0, math.Max(0, p.ContentEnd-0.05)))
			idx := -1
			for i, v := range times {
				if math.Abs(v-t) < 0.005 {
					idx = i
				}
			}
			if idx < 0 {
				times = append(times, t)
				idx = len(times) - 1
			}
			p.stillOf[id] = append(p.stillOf[id], idx)
		}
	}
	clean := func(t, lo, hi float64) float64 {
		// Move off transitions, where the picture is mid-change.
		for _, e := range p.Events {
			if t >= e.Start-0.08 && t <= e.End+0.08 {
				if e.End+0.15 < hi {
					return e.End + 0.15
				}
				return math.Max(lo, e.Start-0.15)
			}
		}
		return t
	}
	for _, bl := range append(append([]*Block{}, p.Blocks...), p.Logos...) {
		lo, hi := bl.Start, bl.End
		if bl.InDur > 0 {
			lo += bl.InDur
		}
		add(bl.ID, clean(lo+0.3*(hi-lo), lo, hi), clean(lo+0.75*(hi-lo), lo, hi))
	}
	if c := p.Captions; c != nil {
		add("captions", c.sampleAt...)
	}
	p.Stills = times
}

// Measure reads the look of every text block, logo and caption band from
// the full-size frames asked for in Plan.Stills (same order). It returns
// the crops to read words from.
func (a *Analyzer) Measure(frames []*RGB) []Crop {
	p := a.plan
	if p == nil || len(frames) != len(p.Stills) {
		return nil
	}
	var crops []Crop
	// Lines found together may still be two things, like a red label over
	// a white headline card: split blocks where the colour behind the
	// letters changes.
	var blocks []*Block
	for _, bl := range p.Blocks {
		parts := splitByStyle(bl, frames[p.stillOf[bl.ID][0]])
		for k, part := range parts {
			if k > 0 {
				part.ID = fmt.Sprintf("%s-%d", bl.ID, k+1)
				p.stillOf[part.ID] = p.stillOf[bl.ID]
			}
			blocks = append(blocks, part)
		}
	}
	p.Blocks = blocks
	for _, bl := range p.Blocks {
		idx := p.stillOf[bl.ID]
		f1, f2 := frames[idx[0]], frames[idx[len(idx)-1]]
		bl.Look = measureBlock(bl, f1, f2)
		if c := cropFor(bl.ID, "text", p.Stills[idx[0]], f1, bl.Box, bl.Look); c != nil {
			crops = append(crops, *c)
		}
	}
	for _, bl := range p.Logos {
		idx := p.stillOf[bl.ID]
		f := frames[idx[0]]
		if c := cropFor(bl.ID, "logo", p.Stills[idx[0]], f, bl.Box.grow(bl.Box.H, bl.Box.H*0.6), nil); c != nil {
			crops = append(crops, *c)
		}
	}
	if c := p.Captions; c != nil {
		var looks []*Look
		for k, i := range p.stillOf["captions"] {
			b := c.Samples[k]
			bl := &Block{Box: b, Lines: []Box{b}}
			l := measureBlock(bl, frames[i], nil)
			looks = append(looks, l)
			if cr := cropFor(fmt.Sprintf("caption%d", k+1), "caption", p.Stills[i], frames[i], b, l); cr != nil {
				crops = append(crops, *cr)
			}
		}
		c.Look = mergeLooks(looks)
	}
	return crops
}

// splitByStyle splits a block between lines drawn on different
// backgrounds.
func splitByStyle(bl *Block, f *RGB) []*Block {
	if len(bl.Lines) < 2 {
		return []*Block{bl}
	}
	var bgs []rgb
	for _, ln := range bl.Lines {
		r := ln.grow(ln.H*0.3, ln.H*0.25).rect(f.W, f.H)
		var cs []rgb
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				cs = append(cs, px(f, x, y))
			}
		}
		c, _ := dominant(cs)
		bgs = append(bgs, c)
	}
	var out []*Block
	cur := &Block{ID: bl.ID, Start: bl.Start, End: bl.End, In: bl.In, InDur: bl.InDur, Kind: bl.Kind}
	for i, ln := range bl.Lines {
		if i > 0 && dist(bgs[i], bgs[i-1]) > 70 {
			out = append(out, cur)
			cur = &Block{Start: bl.Start, End: bl.End, In: bl.In, InDur: bl.InDur, Kind: bl.Kind}
		}
		if len(cur.Lines) == 0 {
			cur.Box = ln
		} else {
			cur.Box = cur.Box.union(ln)
		}
		cur.Lines = append(cur.Lines, ln)
	}
	return append(out, cur)
}

// mergeLooks keeps the most common colour and the median size of a few
// samples of the same caption style.
func mergeLooks(ls []*Look) *Look {
	if len(ls) == 0 {
		return nil
	}
	out := *ls[0]
	var sizes, glyphs []float64
	count := map[string]int{}
	strokes := 0
	for _, l := range ls {
		sizes = append(sizes, l.Size)
		glyphs = append(glyphs, l.Glyph)
		count[l.Color]++
		if l.Stroke != "" {
			strokes++
		}
	}
	best := 0
	for c, n := range count {
		if n > best || (n == best && c < out.Color) {
			out.Color, best = c, n
		}
	}
	out.Size, out.Glyph = r2(median(sizes)), r3(median(glyphs))
	if strokes*2 < len(ls) {
		out.Stroke = ""
	}
	return &out
}

type rgb struct{ r, g, b float64 }

func (c rgb) hex() string {
	return fmt.Sprintf("#%02X%02X%02X", uint8(clampf(c.r, 0, 255)+0.5), uint8(clampf(c.g, 0, 255)+0.5), uint8(clampf(c.b, 0, 255)+0.5))
}

func (c rgb) luma() float64 { return 0.2126*c.r + 0.7152*c.g + 0.0722*c.b }

func (c rgb) sat() float64 {
	mx := math.Max(c.r, math.Max(c.g, c.b))
	mn := math.Min(c.r, math.Min(c.g, c.b))
	if mx < 1 {
		return 0
	}
	return (mx - mn) / mx
}

func dist(a, b rgb) float64 {
	dr, dg, db := a.r-b.r, a.g-b.g, a.b-b.b
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func px(f *RGB, x, y int) rgb {
	i := 3 * (y*f.W + x)
	return rgb{float64(f.Pix[i]), float64(f.Pix[i+1]), float64(f.Pix[i+2])}
}

// dominant returns the most common colour of the pixels (quantised to 4
// bits a channel, then averaged within the winning bin) and its share.
func dominant(cs []rgb) (rgb, float64) {
	if len(cs) == 0 {
		return rgb{}, 0
	}
	type acc struct {
		n       int
		r, g, b float64
	}
	bins := map[int]*acc{}
	for _, c := range cs {
		k := int(c.r)>>4<<8 | int(c.g)>>4<<4 | int(c.b)>>4
		a := bins[k]
		if a == nil {
			a = &acc{}
			bins[k] = a
		}
		a.n++
		a.r += c.r
		a.g += c.g
		a.b += c.b
	}
	// Merge each bin with its neighbours so a colour split across a bin
	// edge still wins.
	bestK, bestN := 0, -1
	for k := range bins {
		n := 0
		for dr := -1; dr <= 1; dr++ {
			for dg := -1; dg <= 1; dg++ {
				for db := -1; db <= 1; db++ {
					if o := bins[k+dr<<8+dg<<4+db]; o != nil {
						n += o.n
					}
				}
			}
		}
		if n > bestN || (n == bestN && k < bestK) {
			bestK, bestN = k, n
		}
	}
	var acc3 acc
	for dr := -1; dr <= 1; dr++ {
		for dg := -1; dg <= 1; dg++ {
			for db := -1; db <= 1; db++ {
				if o := bins[bestK+dr<<8+dg<<4+db]; o != nil {
					acc3.n += o.n
					acc3.r += o.r
					acc3.g += o.g
					acc3.b += o.b
				}
			}
		}
	}
	center := rgb{acc3.r / float64(acc3.n), acc3.g / float64(acc3.n), acc3.b / float64(acc3.n)}
	// Settle on the pixels near that colour.
	n := 0
	for round := 0; round < 2; round++ {
		var sum rgb
		n = 0
		for _, c := range cs {
			if dist(c, center) < 36 {
				sum.r += c.r
				sum.g += c.g
				sum.b += c.b
				n++
			}
		}
		if n == 0 {
			return center, 0
		}
		center = rgb{sum.r / float64(n), sum.g / float64(n), sum.b / float64(n)}
	}
	return center, float64(n) / float64(len(cs))
}

// measureBlock reads a block's colours, box, size, slant and alignment. f2
// (another moment of the same block, or nil) separates the text from a
// moving background.
func measureBlock(bl *Block, f1, f2 *RGB) *Look {
	W, H := f1.W, f1.H
	short := float64(min(W, H))
	tr := bl.Box.rect(W, H)
	l := &Look{Align: "center", Font: "montserrat"}

	var inside []rgb
	for y := tr.Y; y < tr.Y+tr.H; y++ {
		for x := tr.X; x < tr.X+tr.W; x++ {
			inside = append(inside, px(f1, x, y))
		}
	}
	bg, share := dominant(inside)
	// Short words on a box can show more letter than box: try the second
	// most common colour too, and keep whichever surrounds the text.
	var bg2 rgb
	share2 := 0.0
	{
		var rest []rgb
		for _, c := range inside {
			if dist(c, bg) > 60 {
				rest = append(rest, c)
			}
		}
		if c, sh := dominant(rest); sh > 0.4 {
			bg2, share2 = c, sh*float64(len(rest))/float64(len(inside))
		}
	}
	lineH := 0.0
	for _, ln := range bl.Lines {
		lineH = math.Max(lineH, ln.H)
	}
	mY := max(2, int(lineH*float64(H)*0.45))
	mX := max(2, int(lineH*float64(H)*0.6))

	// A box: one colour fills much of the text area and most of the band
	// around it.
	surrounds := func(c rgb) float64 {
		band, near := 0, 0
		for y := max(0, tr.Y-mY); y < min(H, tr.Y+tr.H+mY); y++ {
			for x := max(0, tr.X-mX); x < min(W, tr.X+tr.W+mX); x++ {
				if x >= tr.X && x < tr.X+tr.W && y >= tr.Y && y < tr.Y+tr.H {
					continue
				}
				band++
				if dist(px(f1, x, y), c) < 36 {
					near++
				}
			}
		}
		if band == 0 {
			return 0
		}
		return float64(near) / float64(band)
	}
	// Or the colour reaches past the text on at least three sides: a card
	// whose band above or below holds other lines.
	encloses := func(c rgb) bool {
		area := boxArea(f1, tr, c)
		pad := int(math.Max(2, lineH*float64(H)*0.12))
		sides := 0
		for _, d := range []int{tr.X - area.X, tr.Y - area.Y, area.X + area.W - tr.X - tr.W, area.Y + area.H - tr.Y - tr.H} {
			if d >= pad {
				sides++
			}
		}
		return sides >= 3
	}
	boxed := false
	s1, s2 := 0.0, 0.0
	if share >= 0.25 {
		s1 = surrounds(bg)
	}
	if share2 >= 0.25 {
		s2 = surrounds(bg2)
	}
	switch {
	case s1 >= 0.5 && s1 >= s2:
		boxed = true
	case s2 >= 0.5:
		boxed, bg = true, bg2
	case share >= 0.35 && encloses(bg):
		boxed = true
	case share2 >= 0.35 && encloses(bg2):
		boxed, bg = true, bg2
	}
	// A colour that fills the frame from side to side and runs off its top
	// or bottom is the picture: a black bar, or a dark gradient behind white
	// text. A card ends before the frame does.
	if boxed {
		a := boxArea(f1, tr, bg)
		if a.W >= W*97/100 && (a.Y <= 1 || a.Y+a.H >= H-1) {
			boxed = false
		}
	}

	var glyph []rgb
	glyphAt := func(c rgb) bool { return dist(c, bg) > 80 }
	if boxed {
		l.Background, l.BgOpacity = bg.hex(), 100
		if f2 != nil && f2.W == W && f2.H == H {
			// A see-through box changes colour with what is behind it.
			var other []rgb
			for y := tr.Y; y < tr.Y+tr.H; y++ {
				for x := tr.X; x < tr.X+tr.W; x++ {
					other = append(other, px(f2, x, y))
				}
			}
			if bg2, _ := dominant(other); dist(bg, bg2) > 18 {
				l.BgOpacity = 75
			}
		}
		area := boxArea(f1, tr, bg)
		ab := area.box(W, H)
		l.BoxArea = &ab
		l.Card = true
		for y := tr.Y; y < tr.Y+tr.H; y++ {
			for x := tr.X; x < tr.X+tr.W; x++ {
				if c := px(f1, x, y); glyphAt(c) {
					glyph = append(glyph, c)
				}
			}
		}
		if len(bl.Lines) > 1 {
			// Line boxes leave the gaps between lines see-through.
			gapRow, gapNear := 0, 0
			for i := 0; i+1 < len(bl.Lines); i++ {
				a, b := bl.Lines[i].rect(W, H), bl.Lines[i+1].rect(W, H)
				for y := a.Y + a.H; y < b.Y; y++ {
					for x := tr.X; x < tr.X+tr.W; x++ {
						gapRow++
						if dist(px(f1, x, y), bg) < 36 {
							gapNear++
						}
					}
				}
			}
			if gapRow > 0 && float64(gapNear)/float64(gapRow) < 0.5 {
				l.Card = false
			}
		}
	} else {
		// No box: the text is what stays the same between two moments (when
		// the picture behind moved), and the brightest or most coloured part
		// of that.
		stable := func(x, y int) bool { return true }
		if f2 != nil && f2.W == W && f2.H == H {
			changed, total := 0, 0
			for y := tr.Y; y < tr.Y+tr.H; y++ {
				for x := tr.X; x < tr.X+tr.W; x++ {
					total++
					if dist(px(f1, x, y), px(f2, x, y)) > 40 {
						changed++
					}
				}
			}
			if float64(changed)/float64(max(1, total)) > 0.25 {
				stable = func(x, y int) bool { return dist(px(f1, x, y), px(f2, x, y)) < 30 }
			}
		}
		var cs []rgb
		var lum []float64
		for y := tr.Y; y < tr.Y+tr.H; y++ {
			for x := tr.X; x < tr.X+tr.W; x++ {
				if stable(x, y) {
					c := px(f1, x, y)
					cs = append(cs, c)
					lum = append(lum, c.luma()+80*c.sat())
				}
			}
		}
		sort.Float64s(lum)
		cut := 200.0
		if len(lum) > 0 {
			cut = math.Max(150, lum[len(lum)*70/100])
		}
		for _, c := range cs {
			if c.luma()+80*c.sat() >= cut {
				glyph = append(glyph, c)
			}
		}
		if len(glyph) > 0 {
			fill, _ := dominant(glyph)
			bg = rgb{0, 0, 0}
			glyphAt = func(c rgb) bool { return dist(c, fill) < 70 }
			// A dark ring around the letters is an outline.
			ring, darkN := 0, 0
			var darks []rgb
			for y := tr.Y + 1; y < tr.Y+tr.H-1; y++ {
				for x := tr.X + 1; x < tr.X+tr.W-1; x++ {
					if !glyphAt(px(f1, x, y)) {
						continue
					}
					for _, d := range [4][2]int{{-3, 0}, {3, 0}, {0, -3}, {0, 3}} {
						nx, ny := clampi(x+d[0], 0, W-1), clampi(y+d[1], 0, H-1)
						c := px(f1, nx, ny)
						if glyphAt(c) {
							continue
						}
						ring++
						if c.luma() < 85 && stable(nx, ny) {
							darkN++
							darks = append(darks, c)
						}
					}
				}
			}
			if ring > 0 && float64(darkN)/float64(ring) > 0.35 {
				sc, _ := dominant(darks)
				l.Stroke = sc.hex()
			}
		}
	}

	// Text colour, and a second colour for key words.
	if len(glyph) > 0 {
		fill, _ := dominant(glyph)
		l.Color = fill.hex()
		var rest []rgb
		for _, c := range glyph {
			if dist(c, fill) > 90 {
				rest = append(rest, c)
			}
		}
		if len(rest) >= len(glyph)*6/100 {
			if e, sh := dominant(rest); sh > 0.5 && e.sat() > 0.35 {
				l.Emphasis = e.hex()
				// Key words are letters too, a whole line can be in them.
				fillAt := glyphAt
				glyphAt = func(c rgb) bool { return fillAt(c) || dist(c, e) < 70 }
			}
		}
	} else {
		l.Color = "#FFFFFF"
	}

	// Line height from the letters themselves at full size, and where the
	// capitals start and the baseline runs: rows busy with letter pixels.
	// Ascenders and descenders are thin, so they fall under the cut.
	var hs []float64
	inner := Rect{0, 0, W, H}
	if l.BoxArea != nil {
		// Only inside the box: its edge against the picture is not a letter.
		a := l.BoxArea.rect(W, H)
		inner = Rect{a.X + 2, a.Y + 2, a.W - 4, a.H - 4}
	}
	bl.Lines = splitTightLines(f1, bl.Lines, glyphAt)
	lineH = 0
	for _, ln := range bl.Lines {
		lineH = math.Max(lineH, ln.H)
	}
	for i, ln := range bl.Lines {
		r := ln.grow(0, ln.H*0.35).rect(W, H)
		x0, x1 := max(r.X, inner.X), min(r.X+r.W, inner.X+inner.W)
		y0, y1 := max(r.Y, inner.Y), min(r.Y+r.H, inner.Y+inner.H)
		// Never into the next line: set tight, the two are a few rows apart.
		if i > 0 {
			y0 = max(y0, (bl.Lines[i-1].rect(W, H).Y+bl.Lines[i-1].rect(W, H).H+ln.rect(W, H).Y)/2)
		}
		if i+1 < len(bl.Lines) {
			y1 = min(y1, (ln.rect(W, H).Y+ln.rect(W, H).H+bl.Lines[i+1].rect(W, H).Y+1)/2)
		}
		if x1-x0 < 4 || y1-y0 < 4 {
			continue
		}
		r = Rect{x0, y0, x1 - x0, y1 - y0}
		counts := make([]int, r.H)
		cols := make([]int, r.W)
		top, bot, peak := -1, -1, 0
		lx, rx := W, -1
		for y := r.Y; y < r.Y+r.H; y++ {
			n := 0
			for x := r.X; x < r.X+r.W; x++ {
				if glyphAt(px(f1, x, y)) {
					n++
					cols[x-r.X]++
				}
			}
			counts[y-r.Y] = n
			peak = max(peak, n)
			if n >= max(2, r.W/60) {
				if top < 0 {
					top = y
				}
				bot = y
			}
		}
		if top >= 0 {
			hs = append(hs, float64(bot-top+1)/float64(H))
		}
		// Where the letters start and end: columns with several letter pixels
		// and a neighbour that has them too, so a stray bright speck in the
		// footage beside the line does not stretch it.
		need := max(2, (bot-top+1)/6)
		busy := func(i int) bool { return i >= 0 && i < len(cols) && cols[i] >= need }
		for i := range cols {
			if busy(i) && (busy(i+1) || busy(i+2)) {
				lx = min(lx, r.X+i)
				rx = max(rx, r.X+i)
			}
			if busy(i) && (busy(i-1) || busy(i-2)) {
				rx = max(rx, r.X+i)
			}
		}
		capTop, base := -1, -1
		for i, n := range counts {
			if n*5 >= peak && capTop < 0 {
				capTop = i
			}
			if n*3 >= peak {
				base = i
			}
		}
		if capTop >= 0 && base > capTop && rx > lx {
			l.CapTop = append(l.CapTop, r3(float64(r.Y+capTop)/float64(H)))
			l.Base = append(l.Base, r3(float64(r.Y+base+1)/float64(H)))
			l.Left = append(l.Left, r3(float64(lx)/float64(W)))
			l.Right = append(l.Right, r3(float64(rx+1)/float64(W)))
		}
	}
	l.Glyph = r3(median(hs))
	if l.Glyph < lineH*0.7 || l.Glyph > lineH*1.4 {
		// Glow or scenery joined the letters; trust the line finder.
		l.Glyph = r3(lineH)
	}
	l.Size = r2(l.Glyph * float64(H) / 0.9 * 1080 / short)

	// Slant and weight from the letter shapes.
	var lrs []Rect
	for _, ln := range bl.Lines {
		lrs = append(lrs, ln.rect(W, H))
	}
	l.Italic, l.Heavy, l.Weight = shape(f1, lrs, glyphAt, l.Glyph*float64(H))
	l.Weight = r3(l.Weight)

	// Alignment from where the lines start and end: the letters' own ends
	// when measured, lines split out of one box all have the same box.
	if len(bl.Lines) > 1 {
		var ls, cs, rs []float64
		for _, ln := range bl.Lines {
			ls = append(ls, ln.X)
			cs = append(cs, ln.cx())
			rs = append(rs, ln.X+ln.W)
		}
		if len(l.Left) == len(bl.Lines) {
			ls, cs, rs = l.Left, nil, l.Right
			for i := range ls {
				cs = append(cs, (ls[i]+rs[i])/2)
			}
		}
		sl, sc, sr := spread(ls), spread(cs), spread(rs)
		switch {
		case sl <= sc && sl <= sr:
			l.Align = "left"
		case sr < sc:
			l.Align = "right"
		}
	} else if c := bl.Box.cx(); c < 0.42 {
		l.Align = "left"
	} else if c > 0.58 {
		l.Align = "right"
	}

	switch {
	case l.Italic:
		l.Font = "poppins"
	case l.Heavy && l.Stroke != "":
		l.Font = "anton"
	case l.Background != "":
		l.Font = "poppins"
	}
	return l
}

func spread(v []float64) float64 {
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return hi - lo
}

// boxArea grows the text rectangle outward while the edge rows and columns
// are still mostly the box colour.
func boxArea(f *RGB, r Rect, bg rgb) Rect {
	frac := func(x0, y0, x1, y1 int) float64 {
		n, k := 0, 0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				n++
				if dist(px(f, x, y), bg) < 36 {
					k++
				}
			}
		}
		if n == 0 {
			return 0
		}
		return float64(k) / float64(n)
	}
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	for y0 > 0 && frac(x0, y0-1, x1, y0) > 0.7 {
		y0--
	}
	for y1 < f.H && frac(x0, y1, x1, y1+1) > 0.7 {
		y1++
	}
	for x0 > 0 && frac(x0-1, y0, x0, y1) > 0.7 {
		x0--
	}
	for x1 < f.W && frac(x1, y0, x1+1, y1) > 0.7 {
		x1++
	}
	// Another box can sit on the card's corner, like a label on a headline:
	// the row is still the card's if its colour runs in from one side.
	fromSide := func(y int) float64 {
		left, right := 0, 0
		for x := x0; x < x1 && dist(px(f, x, y), bg) < 36; x++ {
			left++
		}
		for x := x1 - 1; x >= x0 && dist(px(f, x, y), bg) < 36; x-- {
			right++
		}
		return float64(max(left, right)) / float64(max(1, x1-x0))
	}
	for y0 > 0 && fromSide(y0-1) > 0.35 {
		y0--
	}
	for y1 < f.H && fromSide(y1) > 0.35 {
		y1++
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// splitTightLines separates lines the tracker saw as one: set tight, they share a
// bounding box, but their letters still leave a row band with almost none.
func splitTightLines(f *RGB, lines []Box, on func(rgb) bool) []Box {
	var out []Box
	for _, ln := range lines {
		r := ln.rect(f.W, f.H)
		counts := make([]int, r.H)
		peak := 0
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				if on(px(f, x, y)) {
					counts[y-r.Y]++
				}
			}
			peak = max(peak, counts[y-r.Y])
		}
		// Bands of rows busy with letters. Ascenders and descenders fall under
		// the cut, so one line of mixed case is still one band.
		type band struct{ a, b int }
		var bands []band
		tallest := 0
		for i := 0; i < len(counts); i++ {
			if float64(counts[i]) < 0.22*float64(peak) {
				continue
			}
			j := i
			for j+1 < len(counts) && float64(counts[j+1]) >= 0.22*float64(peak) {
				j++
			}
			bands = append(bands, band{i, j + 1})
			tallest = max(tallest, j+1-i)
			i = j
		}
		var keep []band
		for _, b := range bands {
			if b.b-b.a >= max(5, tallest/2) {
				keep = append(keep, b)
			}
		}
		if len(keep) < 2 {
			out = append(out, ln)
			continue
		}
		for i, b := range keep {
			y0, y1 := r.Y, r.Y+r.H
			if i > 0 {
				y0 = r.Y + (keep[i-1].b+b.a)/2
			}
			if i+1 < len(keep) {
				y1 = r.Y + (b.b+keep[i+1].a)/2
			}
			out = append(out, Rect{r.X, y0, r.W, y1 - y0}.box(f.W, f.H))
		}
	}
	return out
}

// shape tells italic letters by the shear that makes the left edges of
// their strokes line up best, line by line, and heavy letters by stroke
// thickness.
func shape(f *RGB, lines []Rect, on func(rgb) bool, glyphPx float64) (italic, heavy bool, stroke float64) {
	type pt struct{ x, y float64 }
	var pts []pt
	runs, runLen := 0, 0
	for _, r := range lines {
		yc := float64(r.Y) + float64(r.H)/2
		for y := r.Y; y < r.Y+r.H; y++ {
			prev := false
			for x := r.X; x < r.X+r.W; x++ {
				cur := on(px(f, x, y))
				if cur {
					runLen++
					if !prev {
						runs++
						pts = append(pts, pt{float64(x), float64(y) - yc})
					}
				}
				prev = cur
			}
		}
	}
	if len(pts) < 40 || runs == 0 {
		return false, false, 0
	}
	// Stroke weight: the mean horizontal run through a letter, as a share
	// of the line's letter height.
	stroke = float64(runLen) / float64(runs) / math.Max(1, glyphPx)
	heavy = stroke > 0.2
	score := func(s float64) float64 {
		cols := make(map[int]float64)
		for _, p := range pts {
			cols[int(math.Round(p.x+s*p.y))]++
		}
		var q float64
		for _, v := range cols {
			q += v * v
		}
		return q
	}
	flat := score(0)
	best, bs := 0.0, flat
	for s := 0.04; s <= 0.4; s += 0.02 {
		if v := score(s); v > bs {
			best, bs = s, v
		}
	}
	italic = best >= 0.1 && bs > flat*1.08
	return italic, heavy, stroke
}

// cropFor cuts the words of a block out of a frame, plain and as
// black-on-white for OCR.
func cropFor(id, kind string, at float64, f *RGB, b Box, l *Look) *Crop {
	pad := b.H * 0.25
	r := b.grow(pad, pad).rect(f.W, f.H)
	if r.W < 4 || r.H < 4 {
		return nil
	}
	img := &RGB{r.W, r.H, make([]uint8, 3*r.W*r.H)}
	for y := 0; y < r.H; y++ {
		copy(img.Pix[3*y*r.W:3*(y+1)*r.W], f.Pix[3*((r.Y+y)*f.W+r.X):3*((r.Y+y)*f.W+r.X+r.W)])
	}
	c := &Crop{ID: id, Kind: kind, At: at, Rect: r, Image: img}
	if l != nil && l.Color != "" {
		var emph rgb
		fill := parseHex(l.Color)
		hasEmph := l.Emphasis != ""
		if hasEmph {
			emph = parseHex(l.Emphasis)
		}
		g := &Gray{r.W, r.H, make([]uint8, r.W*r.H)}
		for i := range g.Pix {
			p := rgb{float64(img.Pix[3*i]), float64(img.Pix[3*i+1]), float64(img.Pix[3*i+2])}
			v := uint8(255)
			if dist(p, fill) < 90 || (hasEmph && dist(p, emph) < 90) {
				v = 0
			}
			g.Pix[i] = v
		}
		c.OCR = g
	}
	return c
}

func parseHex(s string) rgb {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &r, &g, &b)
	return rgb{float64(r), float64(g), float64(b)}
}
