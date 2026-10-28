package clone

import (
	"math"
	"sort"
)

// Text that an editor laid over the video differs from text that was in
// the footage (a sign, a shirt) in one way that is easy to measure: it
// stays put while the picture behind it changes. Lines of text are found in
// every frame of the high stream from their edges, followed from frame to
// frame, and kept when the picture moved behind them, when they lasted
// across a cut, or when they came and went in the middle of a shot.

type cand struct {
	r    Rect
	box  Box
	sig  []float64 // the grey picture of the line
	esig []float64 // its edges, which stay put when what shows between the letters changes
}

type track struct {
	first, last int // high frame indexes
	seen        int
	sum         Box // sum of boxes, for the mean
	box         Box // last box
	sig, esig   []float64
	moved       int // frames where the background changed and the box did not
	still       int // frames where both stayed (no evidence either way)
}

func (t *track) mean() Box {
	n := float64(t.seen)
	return Box{t.sum.X / n, t.sum.Y / n, t.sum.W / n, t.sum.H / n}
}

const sigW, sigH = 32, 8

// findLines returns the boxes of text-like lines in a frame: dense runs of
// strong edges, wider than tall.
func findLines(g *Gray) ([]cand, []uint8) {
	w, h := g.W, g.H
	edge := make([]uint8, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			gx := int(g.Pix[i+1]) - int(g.Pix[i-1])
			gy := int(g.Pix[i+w]) - int(g.Pix[i-w])
			if gx < 0 {
				gx = -gx
			}
			if gy < 0 {
				gy = -gy
			}
			if gx+gy >= 70 {
				edge[i] = 1
			}
		}
	}
	// Join letters into words and words into lines: fill short gaps along
	// each row, then bridge one-pixel gaps between rows.
	gap := max(3, w/45)
	fill := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		last := -1
		for x := 0; x < w; x++ {
			if edge[y*w+x] == 1 {
				if last >= 0 && x-last <= gap {
					for k := last; k <= x; k++ {
						fill[y*w+k] = 1
					}
				}
				fill[y*w+x] = 1
				last = x
			}
		}
	}
	for x := 0; x < w; x++ {
		for y := 1; y < h-1; y++ {
			if fill[y*w+x] == 0 && fill[(y-1)*w+x] == 1 && fill[(y+1)*w+x] == 1 {
				fill[y*w+x] = 1
			}
		}
	}
	// Connected regions.
	label := make([]int32, w*h)
	var out []cand
	stack := make([]int, 0, 1024)
	next := int32(0)
	for i := range fill {
		if fill[i] == 0 || label[i] != 0 {
			continue
		}
		next++
		label[i] = next
		stack = append(stack[:0], i)
		x0, y0, x1, y1, area := w, h, 0, 0, 0
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := j%w, j/w
			area++
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			for _, k := range [4]int{j - 1, j + 1, j - w, j + w} {
				if k < 0 || k >= len(fill) || fill[k] == 0 || label[k] != 0 {
					continue
				}
				if (k == j-1 && x == 0) || (k == j+1 && x == w-1) {
					continue
				}
				label[k] = next
				stack = append(stack, k)
			}
		}
		for _, r := range xyCut(edge, w, Rect{x0, y0, x1 - x0 + 1, y1 - y0 + 1}, max(5, min(w, h)*16/1000), gap, 0) {
			if c, ok := lineCand(g, edge, r); ok {
				out = append(out, c)
			}
		}
	}
	// One line found twice (alone, and joined to a box edge beside it):
	// keep the tighter box.
	sort.Slice(out, func(i, j int) bool { return out[i].r.W*out[i].r.H < out[j].r.W*out[j].r.H })
	var kept []cand
	for _, c := range out {
		dup := false
		for _, k := range kept {
			x0, y0 := max(c.r.X, k.r.X), max(c.r.Y, k.r.Y)
			x1, y1 := min(c.r.X+c.r.W, k.r.X+k.r.W), min(c.r.Y+c.r.H, k.r.Y+k.r.H)
			if x1 > x0 && y1 > y0 && float64((x1-x0)*(y1-y0)) >= 0.6*float64(k.r.W*k.r.H) {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, c)
		}
	}
	return kept, edge
}

// xyCut splits a region into lines (at rows with almost no edges) and
// each line into pieces (at wide columns with almost no edges), twice over:
// a caption touching a table edge, or a card's text joined to the scenery
// beside it, comes apart into its own line.
func xyCut(edge []uint8, w int, r Rect, minH, gap, depth int) []Rect {
	var out []Rect
	for _, band := range splitRows(edge, w, r.X, r.Y, r.X+r.W-1, r.Y+r.H-1, minH) {
		parts := splitCols(edge, w, band, gap)
		for _, p := range parts {
			if depth < 2 && (len(parts) > 1 || p.H < band.H || p.W < r.W) && (p.W != r.W || p.H != r.H) {
				out = append(out, xyCut(edge, w, p, minH, gap, depth+1)...)
			} else {
				out = append(out, p)
			}
		}
	}
	return out
}

// edgePatch is the share of edge pixels in each cell of a w x h grid over
// r, zero-mean and unit length like patch.
func edgePatch(edge []uint8, stride int, r Rect, w, h int) []float64 {
	out := make([]float64, w*h)
	var m float64
	for y := r.Y; y < r.Y+r.H; y++ {
		cy := (y - r.Y) * h / r.H
		for x := r.X; x < r.X+r.W; x++ {
			if edge[y*stride+x] == 1 {
				out[cy*w+(x-r.X)*w/r.W]++
				m++
			}
		}
	}
	return normalize(out, m/float64(len(out)))
}

// splitCols cuts a band at runs of columns (wider than two letter gaps)
// whose edges are only a stray line, not letters.
func splitCols(edge []uint8, w int, r Rect, gap int) []Rect {
	thr := max(1, r.H*15/100)
	busy := make([]bool, r.W)
	for x := 0; x < r.W; x++ {
		n := 0
		for y := r.Y; y < r.Y+r.H; y++ {
			n += int(edge[y*w+r.X+x])
		}
		busy[x] = n > thr
	}
	var out []Rect
	start, quiet := -1, 0
	for x := 0; x <= r.W; x++ {
		on := x < r.W && busy[x]
		switch {
		case on:
			if start < 0 {
				start = x
			}
			quiet = 0
		case start >= 0:
			quiet++
			if quiet > 2*gap || x == r.W {
				end := x - quiet + 1
				if end > start {
					out = append(out, Rect{r.X + start, r.Y, end - start, r.H})
				}
				start, quiet = -1, 0
			}
		}
	}
	return out
}

// splitRows cuts a region into text lines at rows with almost no edges:
// lines set close together (or framed by a box edge) join into one region.
func splitRows(edge []uint8, w, x0, y0, x1, y1, minH int) []Rect {
	counts := make([]int, y1-y0+1)
	flips := make([]int, y1-y0+1)
	var nz []float64
	for y := y0; y <= y1; y++ {
		n, f := 0, 0
		for x := x0; x <= x1; x++ {
			n += int(edge[y*w+x])
			if x > x0 && edge[y*w+x] != edge[y*w+x-1] {
				f++
			}
		}
		counts[y-y0], flips[y-y0] = n, f
		if n > 0 {
			nz = append(nz, float64(n))
		}
	}
	// A row belongs to a line when it has a fair share of the typical
	// row's edges; the median, not the maximum, so one long box edge or
	// table line does not make every text row look empty.
	thr := math.Max(1, 0.35*median(nz))
	var out []Rect
	start := -1
	for i := 0; i <= len(counts); i++ {
		// A row that is one long run (the edge between two stacked line
		// boxes) separates lines instead of joining them.
		on := i < len(counts) && float64(counts[i]) > thr && !(flips[i] <= 4 && counts[i]*2 > x1-x0)
		switch {
		case on && start < 0:
			start = i
		case !on && start >= 0:
			if i-start >= minH {
				// Tighten the sides to where this band has edges.
				lx, rx := x1, x0
				for y := y0 + start; y < y0+i; y++ {
					for x := x0; x <= x1; x++ {
						if edge[y*w+x] == 1 {
							lx, rx = min(lx, x), max(rx, x)
						}
					}
				}
				if rx > lx {
					out = append(out, Rect{lx, y0 + start, rx - lx + 1, i - start})
				}
			}
			start = -1
		}
	}
	return out
}

// lineCand keeps a region that looks like a line of text: wider than
// tall, dense with edges, with the on/off rhythm of letters.
func lineCand(g *Gray, edge []uint8, r Rect) (cand, bool) {
	w, h := g.W, g.H
	bw, bh := r.W, r.H
	short := min(w, h)
	if bh < max(5, short*16/1000) || bh > h*14/100 || bw < bh*3/2 || bw < w*5/100 {
		return cand{}, false
	}
	edges, flips := 0, 0
	for y := r.Y; y < r.Y+bh; y++ {
		for x := r.X; x < r.X+bw; x++ {
			edges += int(edge[y*w+x])
		}
	}
	for _, yy := range []int{r.Y + bh/3, r.Y + bh/2, r.Y + 2*bh/3} {
		for x := r.X + 1; x < r.X+bw; x++ {
			if edge[yy*w+x] != edge[yy*w+x-1] {
				flips++
			}
		}
	}
	dens := float64(edges) / float64(bw*bh)
	if dens < 0.12 || dens > 0.85 || float64(flips)/3 < float64(bw)/float64(bh)*0.9 {
		return cand{}, false
	}
	return cand{r: r, box: r.box(w, h), sig: patch(g, r, sigW, sigH), esig: edgePatch(edge, w, r, sigW, sigH)}, true
}

// trackText follows the lines of one high frame on from the frames before.
func (a *Analyzer) trackText(g *Gray, k int) {
	cands, edge := findLines(g)
	a.edges = append(a.edges, pack(edge))
	if a.debug != nil {
		var bs []Box
		for _, c := range cands {
			bs = append(bs, c.box)
		}
		a.debug.High = append(a.debug.High, bs)
	}
	var still []*track
	for _, t := range a.active {
		if k-t.last <= 2 {
			still = append(still, t)
		}
	}
	a.active = still
	taken := map[*track]bool{}
	for _, c := range cands {
		var best *track
		bestScore := 0.72
		for _, t := range a.active {
			if taken[t] || t.box.iou(c.box) < 0.5 {
				continue
			}
			if s := math.Max(dot(t.sig, c.sig), dot(t.esig, c.esig)); s > bestScore {
				best, bestScore = t, s
			}
		}
		if best == nil {
			t := &track{first: k, last: k, seen: 1, sum: c.box, box: c.box, sig: c.sig, esig: c.esig}
			a.tracks = append(a.tracks, t)
			a.active = append(a.active, t)
			taken[t] = true
			continue
		}
		taken[best] = true
		best.last, best.box, best.sig, best.esig = k, c.box, c.sig, c.esig
		best.seen++
		best.sum = Box{best.sum.X + c.box.X, best.sum.Y + c.box.Y, best.sum.W + c.box.W, best.sum.H + c.box.H}
		if p := a.prevHigh; p != nil && p.W == g.W && p.H == g.H {
			ring := ringDiff(p, g, c.r, max(4, c.r.H))
			in := regionDiff(p, g, c.r)
			switch {
			case ring >= 7 && in <= 0.35*ring:
				best.moved++
			case ring < 4:
				best.still++
			}
		}
	}
}

// Block is a piece of laid-over text: one or more lines shown together.
type Block struct {
	ID    string  `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Box   Box     `json:"box"`   // the text, in fractions of the frame
	Lines []Box   `json:"lines"` // each line
	In    string  `json:"in,omitempty"`
	InDur float64 `json:"in_dur,omitempty"`
	Out   string  `json:"out,omitempty"`
	// Filled in by Measure.
	Look    *Look  `json:"look,omitempty"`
	Words   string `json:"words,omitempty"`
	Slot    string `json:"slot,omitempty"` // the recipe slot it became
	Kind    string `json:"kind"`           // text, label, logo
	stillAt []float64
}

// CaptionBand is where spoken words appear, one word or a short phrase at
// a time.
type CaptionBand struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Y        float64 `json:"y"`      // centre line
	Height   float64 `json:"height"` // typical line height, fraction of the frame
	Width    float64 `json:"width"`  // typical width
	Mode     string  `json:"mode"`   // pop (one word) or chunk
	MaxWords int     `json:"max_words"`
	Changes  float64 `json:"changes_per_second"`
	Look     *Look   `json:"look,omitempty"`
	Samples  []Box   `json:"-"`
	sampleAt []float64
}

// textResults sorts the tracks into captions, text blocks and logos.
func (a *Analyzer) textResults(p *Plan) {
	end := p.ContentEnd
	ht := a.highTime
	dt := 1 / a.cfg.HighFPS
	crossesCut := func(s, e float64) bool {
		for _, ev := range p.Events {
			// Only a real change of picture: text on a screenshot stays put
			// through a zoom or blur into another view of the same image.
			if ev.Kind != "cut" && ev.Kind != "flash" && ev.Kind != "fadeblack" {
				continue
			}
			if ev.At > s+0.25 && ev.At < e-0.25 {
				return true
			}
		}
		return false
	}
	nearCut := func(t float64) bool {
		if t < 0.2 || t > end-0.2 {
			return true
		}
		for _, ev := range p.Events {
			if t >= ev.Start-0.25 && t <= ev.End+0.25 {
				return true
			}
		}
		return false
	}

	// Laid-over lines: long-lived lines, with whatever evidence each has on
	// its own that it sits on top of the picture.
	var lines []span
	for _, t := range a.tracks {
		if t.seen < 2 {
			continue
		}
		s, e := ht(t.first), math.Min(ht(t.last)+dt, end)
		if e-s < 0.5 || s >= end-0.2 {
			continue
		}
		b := t.mean()
		if b.H < 0.013 {
			continue // platform watermarks and tiny print
		}
		// Something that comes and goes in the middle of a shot counts when
		// the picture behind it either moved while it held still, or did not
		// move at all (so the change was the text itself).
		midShot := !nearCut(s) && !nearCut(e) && a.popsIn(b, s, e) && (t.moved >= 1 || t.still*2 >= t.seen)
		lines = append(lines, span{s, e, b, t.moved >= 2 || crossesCut(s, e) || midShot})
	}
	// One line seen as several tracks (lost for a moment behind a flash or
	// a busy background) is stitched back together, before and after its
	// span is grown over the frames where its edges still show.
	lines = stitch(lines)
	for i := range lines {
		lines[i] = a.extend(lines[i], end)
	}
	lines = stitch(lines)
	sort.Slice(lines, func(i, j int) bool { return lines[i].b.Y < lines[j].b.Y })

	// Lines shown together and stacked closely are one block.
	type group struct {
		bl *Block
		ev bool
	}
	var groups []*group
	for _, l := range lines {
		var into *group
		for _, g := range groups {
			bl := g.bl
			ov := math.Min(bl.End, l.e) - math.Max(bl.Start, l.s)
			span := math.Max(bl.End, l.e) - math.Min(bl.Start, l.s)
			last := bl.Lines[len(bl.Lines)-1]
			gap := l.b.Y - (last.Y + last.H)
			hOK := l.b.H < last.H*1.6 && last.H < l.b.H*1.6
			xOK := math.Min(l.b.X+l.b.W, bl.Box.X+bl.Box.W)-math.Max(l.b.X, bl.Box.X) > 0
			if ov/math.Min(bl.End-bl.Start, l.e-l.s) >= 0.6 && ov/span >= 0.4 && gap < 1.3*math.Max(l.b.H, last.H) && gap > -0.5*l.b.H && hOK && xOK {
				into = g
				break
			}
		}
		if into == nil {
			groups = append(groups, &group{&Block{Start: l.s, End: l.e, Box: l.b, Lines: []Box{l.b}, Kind: "text"}, l.ev})
			continue
		}
		into.bl.Lines = append(into.bl.Lines, l.b)
		into.bl.Box = into.bl.Box.union(l.b)
		into.bl.Start, into.bl.End = math.Min(into.bl.Start, l.s), math.Max(into.bl.End, l.e)
		into.ev = into.ev || l.ev
	}
	// A card's lines see only the card around them; the block as a whole
	// sees the picture moving past its edges.
	// Text inside one photo or screenshot that zooms or holds still is
	// part of the picture unless it popped up on its own.
	inStill := func(s, e float64) bool {
		for _, sh := range p.Shots {
			if s >= sh.Start-0.1 && e <= sh.End+0.1 {
				return sh.Still || sh.ZoomTo > 0
			}
		}
		return false
	}
	var blocks []*Block
	for _, g := range groups {
		bl := g.bl
		if inStill(bl.Start, bl.End) && !a.popsIn(bl.Box, bl.Start, bl.End) {
			continue
		}
		steady := a.steady(bl.Box, bl.Start, bl.End)
		if g.ev || crossesCut(bl.Start, bl.End) || (steady && a.behindMoves(bl.Box, bl.Start, bl.End)) ||
			(steady && !nearCut(bl.Start) && !nearCut(bl.End) && a.popsIn(bl.Box, bl.Start, bl.End)) {
			blocks = append(blocks, bl)
		}
	}

	// Captions: short-lived lines that keep coming back to one centred
	// band, away from the blocks (a card sliding in breaks into short
	// pieces too).
	type short struct {
		t      *track
		s, e   float64
		b      Box
		steady bool
	}
	inBlock := func(b Box, s, e float64) bool {
		for _, bl := range blocks {
			if bl.Box.iou(b) > 0.2 && s < bl.End+0.5 && e > bl.Start-0.5 {
				return true
			}
		}
		return false
	}
	var shorts []short
	for _, t := range a.tracks {
		s, e := ht(t.first), ht(t.last)+dt
		b := t.mean()
		if s >= end || e-s > 3 || b.H < 0.01 {
			continue
		}
		if math.Abs(b.cx()-0.5) > 0.1 || b.W > 0.92 || inBlock(b, s, e) {
			continue
		}
		if !a.appeared(b, s, e) {
			continue // scenery, not a word: the same thing was there before
		}
		shorts = append(shorts, short{t, s, e, b, a.steady(b, s, e)})
	}
	bins := make([]float64, 50)
	for _, sh := range shorts {
		bins[clampi(int(sh.b.cy()*50), 0, 49)] += sh.e - sh.s
	}
	peak := 0
	for i := range bins {
		if bins[i]+bins[max(0, i-1)]*0.5+bins[min(49, i+1)]*0.5 > bins[peak]+bins[max(0, peak-1)]*0.5+bins[min(49, peak+1)]*0.5 {
			peak = i
		}
	}
	var cap []short
	for _, sh := range shorts {
		if math.Abs(sh.b.cy()-(float64(peak)+0.5)/50) <= 0.04 {
			cap = append(cap, sh)
		}
	}
	if len(cap) >= 5 {
		sort.Slice(cap, func(i, j int) bool { return cap[i].s < cap[j].s })
		// Keep the busiest stretch: words come every second or so; a stray
		// line far from the rest is not part of it.
		bestA, bestB, bestN := 0, 0, 0
		for i := 0; i < len(cap); {
			j, reach := i, cap[i].e
			for j+1 < len(cap) && cap[j+1].s-reach <= 1.5 {
				j++
				reach = math.Max(reach, cap[j].e)
			}
			if j-i+1 > bestN {
				bestA, bestB, bestN = i, j, j-i+1
			}
			i = j + 1
		}
		cap = cap[bestA : bestB+1]
	}
	if len(cap) >= 5 {
		s0, s1 := cap[0].s, cap[len(cap)-1].e
		covered, lastEnd := 0.0, s0
		var hs, ws, ys, ratios []float64
		steady := 0
		for _, c := range cap {
			if c.steady {
				steady++
			}
			if c.e > lastEnd {
				covered += c.e - math.Max(c.s, lastEnd)
				lastEnd = c.e
			}
			hs = append(hs, c.b.H)
			ws = append(ws, c.b.W)
			ys = append(ys, c.b.cy())
			ratios = append(ratios, c.b.W/c.b.H)
		}
		span := s1 - s0
		// Words hold still while they show; scenery picked up in a moving
		// shot mostly does not.
		if span >= 3 && covered/span >= 0.3 && float64(len(cap))/span >= 0.4 && steady*10 >= len(cap) {
			band := &CaptionBand{Start: r2(s0), End: r2(math.Min(s1, end)), Y: r3(median(ys)), Height: r3(median(hs)), Width: r3(median(ws)), Changes: r2(float64(len(cap)) / span)}
			band.Mode, band.MaxWords = "pop", 1
			if ratio := median(ratios); ratio > 5 {
				band.Mode = "chunk"
				band.MaxWords = clampi(int(math.Round(ratio/3.2)), 2, 6)
			}
			// Sample the band at a few of its longest words.
			byLen := append([]short(nil), cap...)
			sort.Slice(byLen, func(i, j int) bool { return byLen[i].e-byLen[i].s > byLen[j].e-byLen[j].s })
			for _, c := range byLen[:min(3, len(byLen))] {
				band.Samples = append(band.Samples, c.b)
				band.sampleAt = append(band.sampleAt, (c.s+c.e)/2)
			}
			p.Captions = band
			// A caption that stayed up long enough to look like a block is
			// still a caption.
			var keep []*Block
			for _, bl := range blocks {
				if math.Abs(bl.Box.cy()-band.Y) < 0.035 && bl.End-bl.Start < 3 && len(bl.Lines) == 1 {
					continue
				}
				keep = append(keep, bl)
			}
			blocks = keep
		}
	}

	// Logos stay on screen for most of the video near an edge. A logo's
	// name and its small tagline are one logo.
	for _, bl := range blocks {
		if bl.End-bl.Start >= 0.6*end && (bl.Box.cy() < 0.2 || bl.Box.cy() > 0.85) && len(bl.Lines) <= 2 && bl.Box.W < 0.6 {
			bl.Kind = "logo"
		}
	}
	var kept []*Block
	home := map[*Block]Box{} // each logo's own box, so merging never chains
	for _, bl := range blocks {
		merged := false
		for _, k := range kept {
			if k.Kind != "logo" {
				continue
			}
			hb, ok := home[k]
			if !ok {
				hb = k.Box
				home[k] = hb
			}
			near := hb.grow(hb.H, hb.H*0.8)
			small := bl.Box.H <= hb.H*1.5 && bl.Box.W <= hb.W*2 && len(bl.Lines) <= 2
			if small && near.iou(bl.Box) > 0 && bl.End-bl.Start >= 0.5*(k.End-k.Start) && math.Min(k.End, bl.End)-math.Max(k.Start, bl.Start) > 0.5*(bl.End-bl.Start) {
				k.Box = k.Box.union(bl.Box)
				k.Lines = append(k.Lines, bl.Lines...)
				k.Start, k.End = math.Min(k.Start, bl.Start), math.Max(k.End, bl.End)
				merged = true
				break
			}
		}
		// A short mark (a corner bug, an icon) is not a text slot.
		if !merged && (bl.Kind == "logo" || bl.Box.W >= 0.08) {
			kept = append(kept, bl)
		}
	}
	blocks = kept
	sort.Slice(blocks, func(i, j int) bool {
		if math.Abs(blocks[i].Start-blocks[j].Start) > 0.1 {
			return blocks[i].Start < blocks[j].Start
		}
		return blocks[i].Box.Y < blocks[j].Box.Y
	})
	nt, nl := 0, 0
	for _, bl := range blocks {
		a.refineTiming(bl, p.Events, end)
		if bl.Kind == "logo" {
			nl++
			bl.ID = "logo"
			if nl > 1 {
				bl.ID = "logo" + itoa(nl)
			}
			p.Logos = append(p.Logos, bl)
			continue
		}
		nt++
		bl.ID = "text" + itoa(nt)
		p.Blocks = append(p.Blocks, bl)
	}
}

// behindMoves says whether the picture around b changed while b itself held
// still, at a few moments of [s, e].
func (a *Analyzer) behindMoves(b Box, s, e float64) bool {
	if len(a.lowGray) == 0 {
		return false
	}
	r := b.rect(a.lowGray[0].W, a.lowGray[0].H)
	pad := max(3, r.H/3)
	n, moving := 0, 0
	for t := s + 0.1; t+0.25 < e; t += 0.25 {
		f, g := a.lowGray[a.lowIndex(t)], a.lowGray[a.lowIndex(t+0.25)]
		ring, in := ringDiff(f, g, r, pad), regionDiff(f, g, r)
		if ring >= 7 {
			moving++
			if in <= 3 && in <= 0.25*ring {
				n++
			}
		}
	}
	return n >= 3 && n*10 >= moving*6
}

// steady says whether what is inside b stays the same while it is shown:
// laid-over text does, scenery in a moving shot does not.
func (a *Analyzer) steady(b Box, s, e float64) bool {
	if len(a.lowGray) == 0 || e-s < 0.1 {
		return false
	}
	r := b.rect(a.lowGray[0].W, a.lowGray[0].H)
	lo, hi := s+math.Min(0.1, (e-s)/4), e-math.Min(0.1, (e-s)/4)
	f, g, m := a.lowGray[a.lowIndex(lo)], a.lowGray[a.lowIndex(hi)], a.lowGray[a.lowIndex((lo+hi)/2)]
	return regionDiff(f, g, r) <= 7 && regionDiff(f, m, r) <= 7
}

// popsIn says whether b came up (or went away) on its own: the inside
// changed while the picture around it did not, as when a title appears
// over a shot, not when the whole view zooms or scrolls.
func (a *Analyzer) popsIn(b Box, s, e float64) bool {
	if len(a.lowGray) == 0 {
		return false
	}
	r := b.rect(a.lowGray[0].W, a.lowGray[0].H)
	pad := max(3, r.H/3)
	at := func(t0, t1 float64) bool {
		f, g := a.lowGray[a.lowIndex(math.Max(0, t0))], a.lowGray[a.lowIndex(t1)]
		in, ring := regionDiff(f, g, r), ringDiff(f, g, r, pad)
		return in > 14 && ring < 0.45*in
	}
	return at(s-0.2, s+0.15) || at(e-0.15, e+0.2)
}

// appeared says whether what is inside b during [s, e] differs from what
// was there just before it (or just after): a word that came up, not
// scenery the line finder picked up on and off.
func (a *Analyzer) appeared(b Box, s, e float64) bool {
	if len(a.lowGray) == 0 {
		return false
	}
	r := b.rect(a.lowGray[0].W, a.lowGray[0].H)
	mid := a.lowGray[a.lowIndex((s+e)/2)]
	before := a.lowGray[a.lowIndex(math.Max(0, s-0.2))]
	after := a.lowGray[a.lowIndex(e+0.15)]
	return regionDiff(before, mid, r) > 14 || regionDiff(after, mid, r) > 14
}

// refineTiming finds the exact first and last frame of a block on the
// full-rate stream, and whether it fades in.
func (a *Analyzer) refineTiming(bl *Block, evs []Event, end float64) {
	if len(a.lowGray) == 0 {
		return
	}
	lw, lh := a.lowGray[0].W, a.lowGray[0].H
	r := bl.Box.rect(lw, lh)
	mid := a.lowGray[a.lowIndex((bl.Start+bl.End)/2)]
	diff := func(i int) float64 { return regionDiff(a.lowGray[i], mid, r) }
	present := median([]float64{diff(a.lowIndex(bl.Start + 0.3*(bl.End-bl.Start))), diff(a.lowIndex(bl.Start + 0.6*(bl.End-bl.Start)))})

	// Start: walk back from the high-stream start until the region no
	// longer looks like it does in the middle.
	i := a.lowIndex(bl.Start + 0.05)
	lo := a.lowIndex(math.Max(0, bl.Start-0.4))
	for i > lo && diff(i-1) <= present+8 {
		i--
	}
	start := a.lowTime(i)
	// A fade: the difference shrinks over several frames, not at once.
	if i > 2 {
		absent := diff(max(0, i-3))
		ramp := 0
		for j := i; j < min(len(a.lowGray), i+int(0.8*a.cfg.LowFPS)); j++ {
			if diff(j) > present+0.25*(absent-present) {
				ramp++
			} else {
				break
			}
		}
		if ramp >= 3 && absent > present+12 {
			bl.In, bl.InDur = "fade", r2(float64(ramp+1)/a.cfg.LowFPS)
		}
	}
	j := a.lowIndex(math.Min(end, bl.End) - 0.05)
	hi := a.lowIndex(math.Min(end, bl.End+0.4))
	for j < hi && diff(j+1) <= present+8 {
		j++
	}
	stop := math.Min(end, a.lowTime(j+1))
	// Snap to a cut within a few frames: cards usually start and end on
	// cuts.
	for _, ev := range evs {
		if math.Abs(ev.At-start) <= 0.12 {
			start = ev.At
		}
		if math.Abs(ev.At-stop) <= 0.12 {
			stop = ev.At
		}
	}
	if start < 0.1 {
		start = 0
	}
	if end-stop < 0.15 {
		stop = end
	}
	bl.Start, bl.End = r2(start), r2(stop)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// span is a line of laid-over text and when it shows.
type span struct {
	s, e float64
	b    Box
	ev   bool // evidence that it is laid over the picture
}

// pack keeps an edge map as bits.
func pack(edge []uint8) []uint64 {
	out := make([]uint64, (len(edge)+63)/64)
	for i, v := range edge {
		if v != 0 {
			out[i/64] |= 1 << (i % 64)
		}
	}
	return out
}

func (a *Analyzer) edgeAt(k, i int) bool { return a.edges[k][i/64]&(1<<(i%64)) != 0 }

// extend grows a line's time span over neighbouring high frames where its
// usual edges are present, and trims frames at its ends where they are
// not.
func (a *Analyzer) extend(l span, end float64) span {
	if len(a.edges) == 0 {
		return l
	}
	w, h := a.highW, a.highH
	r := l.b.rect(w, h)
	k0 := clampi(int(math.Round(l.s*a.cfg.HighFPS)), 0, len(a.edges)-1)
	k1 := clampi(int(math.Round(l.e*a.cfg.HighFPS))-1, k0, len(a.edges)-1)
	// The core: pixels that are an edge in most frames of the span.
	var core []int
	n := k1 - k0 + 1
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			i := y*w + x
			c := 0
			for k := k0; k <= k1; k++ {
				if a.edgeAt(k, i) {
					c++
				}
			}
			if c*10 >= n*7 {
				core = append(core, i)
			}
		}
	}
	if len(core) < 12 {
		return l
	}
	present := func(k int) bool {
		hit := 0
		for _, i := range core {
			x, y := i%w, i/w
			found := false
			for dy := -1; dy <= 1 && !found; dy++ {
				for dx := -1; dx <= 1 && !found; dx++ {
					xx, yy := x+dx, y+dy
					if xx >= 0 && yy >= 0 && xx < w && yy < h && a.edgeAt(k, yy*w+xx) {
						found = true
					}
				}
			}
			if found {
				hit++
			}
		}
		return hit*10 >= len(core)*6
	}
	first, last := k0, k1
	for miss := 0; first > 0 && miss <= 1; {
		if present(first - 1 - miss) {
			first -= 1 + miss
			miss = 0
		} else {
			miss++
			if first-1-miss < 0 {
				break
			}
		}
	}
	for miss := 0; last < len(a.edges)-1 && miss <= 1; {
		if last+1+miss < len(a.edges) && present(last+1+miss) {
			last += 1 + miss
			miss = 0
		} else {
			miss++
			if last+1+miss >= len(a.edges) {
				break
			}
		}
	}
	l.s = a.highTime(first)
	l.e = math.Min(end, a.highTime(last+1))
	return l
}

func stitch(lines []span) []span {
	sort.Slice(lines, func(i, j int) bool { return lines[i].s < lines[j].s })
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(lines) && !merged; i++ {
			for j := i + 1; j < len(lines); j++ {
				li, lj := lines[i], lines[j]
				if li.b.iou(lj.b) < 0.55 || lj.s-li.e > 0.7 || li.s-lj.e > 0.7 {
					continue
				}
				wi, wj := li.e-li.s, lj.e-lj.s
				b := Box{(li.b.X*wi + lj.b.X*wj) / (wi + wj), (li.b.Y*wi + lj.b.Y*wj) / (wi + wj), (li.b.W*wi + lj.b.W*wj) / (wi + wj), (li.b.H*wi + lj.b.H*wj) / (wi + wj)}
				lines[i] = span{math.Min(li.s, lj.s), math.Max(li.e, lj.e), b, li.ev || lj.ev}
				lines = append(lines[:j], lines[j+1:]...)
				merged = true
				break
			}
		}
	}
	return lines
}
