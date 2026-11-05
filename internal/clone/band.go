package clone

import (
	"math"
	"sort"
)

// The line finder reads the whole frame at once, so a caption can slip past
// it: thin letters over busy footage join the edges around them into one
// region that is no line, and white letters with a thin coloured outline
// over a pale picture have edges too faint for it. Once the band is known,
// its own strip is read again, apart from the picture above and below it
// and with the fainter edges too, and the words found there carry the band
// on past where the first pass lost it.

// piece is a word (or a line of words) read in the band's strip: the same
// edges held in one place from its first high frame to its last.
type piece struct {
	s, e float64
	b    Box
}

// bandStrip is the rows read again for a band at y with lines h high:
// captions over a pasted screen often sit a little lower or higher.
func bandStrip(y, h float64) (float64, float64) {
	half := math.Max(2.5*h, 0.05)
	return y - half, y + half
}

// bandWords reads the strip y0..y1 of the high frames from t0 to t1.
func (a *Analyzer) bandWords(y0, y1, t0, t1 float64) []piece {
	if len(a.edges) == 0 || len(a.faint) != len(a.edges) {
		return nil
	}
	w, h := a.highW, a.highH
	fps := a.cfg.HighFPS
	ry0, ry1 := clampi(int(y0*float64(h)), 0, h), clampi(int(math.Ceil(y1*float64(h))), 0, h)
	sh := ry1 - ry0
	if sh < 5 {
		return nil
	}
	k0 := clampi(int(t0*fps), 0, len(a.edges)-1)
	k1 := clampi(int(math.Ceil(t1*fps)), k0, len(a.edges)-1)

	type found struct {
		r    Rect // in strip rows
		bits []uint8
	}
	strip := func(m []uint64) []uint8 {
		out := make([]uint8, w*sh)
		for y := 0; y < sh; y++ {
			for x := 0; x < w; x++ {
				i := (ry0+y)*w + x
				if m[i/64]&(1<<(i%64)) != 0 {
					out[y*w+x] = 1
				}
			}
		}
		return out
	}
	cut := func(m []uint8, r Rect) []uint8 {
		out := make([]uint8, 0, r.W*r.H)
		for y := r.Y; y < r.Y+r.H; y++ {
			out = append(out, m[y*w+r.X:y*w+r.X+r.W]...)
		}
		return out
	}
	// same is the share of edges two looks at one place have in common.
	same := func(p found, q found) float64 {
		x0, y0 := max(p.r.X, q.r.X), max(p.r.Y, q.r.Y)
		x1, y1 := min(p.r.X+p.r.W, q.r.X+q.r.W), min(p.r.Y+p.r.H, q.r.Y+q.r.H)
		both, np, nq := 0, 0, 0
		for _, v := range p.bits {
			np += int(v)
		}
		for _, v := range q.bits {
			nq += int(v)
		}
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if p.bits[(y-p.r.Y)*p.r.W+x-p.r.X] == 1 && q.bits[(y-q.r.Y)*q.r.W+x-q.r.X] == 1 {
					both++
				}
			}
		}
		if np == 0 || nq == 0 {
			return 0
		}
		return float64(both) / float64(max(np, nq))
	}
	type run struct {
		first, last int
		f           found
		gone        bool
	}
	var open, done []*run
	for k := k0; k <= k1; k++ {
		var here []found
		for mi, m := range [][]uint64{a.edges[k], a.faint[k]} {
			sm := strip(m)
			for _, r := range lineRects(sm, w, sh, 0, sh) {
				b := Rect{r.X, r.Y + ry0, r.W, r.H}.box(w, h)
				if math.Abs(b.cx()-0.5) > 0.1 || !lineShape(sm, w, h, r) {
					continue
				}
				f := found{r, cut(sm, r)}
				dup := false
				for _, o := range here {
					if mi == 1 && o.r.box(w, sh).iou(r.box(w, sh)) > 0.3 {
						dup = true
						break
					}
				}
				if !dup {
					here = append(here, f)
				}
			}
		}
		taken := map[*run]bool{}
		for _, f := range here {
			var best *run
			bestS := 0.55
			for _, o := range open {
				if taken[o] || o.f.r.box(w, sh).iou(f.r.box(w, sh)) < 0.5 {
					continue
				}
				if s := same(o.f, f); s > bestS {
					best, bestS = o, s
				}
			}
			if best == nil {
				best = &run{first: k}
				open = append(open, best)
			}
			taken[best] = true
			best.last, best.f = k, f
		}
		var still []*run
		for _, o := range open {
			if k-o.last > 1 || k == k1 {
				done = append(done, o)
			} else {
				still = append(still, o)
			}
		}
		open = still
	}
	var out []piece
	for _, o := range done {
		s, e := a.highTime(o.first), a.highTime(o.last+1)
		// A word shows for a beat; a line held through a whole verse while
		// its words light up in turn, for longer. Scenery on a tripod holds
		// for the whole shot.
		if o.last-o.first < 1 || e-s > 8 {
			continue
		}
		b := Rect{o.f.r.X, o.f.r.Y + ry0, o.f.r.W, o.f.r.H}.box(w, h)
		if e-s > 3.5 && !a.appeared(b, s, e) {
			continue
		}
		out = append(out, piece{s, e, b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].s < out[j].s })
	return out
}

// bandOnly drops what is not a caption word: lines of a card already found
// (a hook card can sit in the band's strip), and regions much taller than
// two of the band's lines with the picture around them joined on.
func bandOnly(words []piece, blocks []*Block, lineH float64) []piece {
	var out []piece
	sort.Slice(words, func(i, j int) bool { return words[i].s < words[j].s })
	for _, p := range words {
		if p.b.H > 5.5*lineH {
			continue
		}
		card := false
		for _, bl := range blocks {
			if len(bl.Lines) > 2 && p.s < bl.End && p.e > bl.Start && p.b.overlap(bl.Box) >= 0.5 {
				card = true
				break
			}
		}
		if !card {
			out = append(out, p)
		}
	}
	return out
}

// carryBand extends a band from s0..s1 over the words read in its strip:
// they keep coming with no pause longer than a breath (4.5 seconds: the
// words read again are fewer than the band's own). Past a longer gap, up
// to 12 seconds, it goes on only to a run that is captions by itself: five
// words or more, one after another. It returns the new ends and the words
// it used.
func carryBand(s0, s1 float64, words []piece) (float64, float64, []piece) {
	var after, before []piece
	for _, p := range words {
		if p.e > s1 {
			after = append(after, p)
		}
	}
	for i := len(words) - 1; i >= 0; i-- {
		if words[i].s < s0 {
			// Mirrored in time, so one walk serves both ends.
			before = append(before, piece{-words[i].e, -words[i].s, words[i].b})
		}
	}
	e1, used1 := carry(s1, after)
	e0, used0 := carry(-s0, before)
	for i := range used0 {
		used0[i].s, used0[i].e = -used0[i].e, -used0[i].s
	}
	return -e0, e1, append(used1, used0...)
}

// carry walks words (sorted by start) on from reach.
func carry(reach float64, words []piece) (float64, []piece) {
	var used []piece
	for i := 0; i < len(words); {
		gap := words[i].s - reach
		// The run starting here: words with short pauses between them.
		j, end := i, words[i].e
		for j+1 < len(words) && words[j+1].s-end <= 1.5 {
			j++
			end = math.Max(end, words[j].e)
		}
		run := words[i : j+1]
		if gap > 12 || gap > 4.5 && moments(run) < 5 {
			break
		}
		// One word alone right past the end is as likely a flicker of the
		// picture.
		if len(used) == 0 && moments(run) < 2 && (j+1 >= len(words) || words[j+1].s-end > 4.5) {
			break
		}
		used = append(used, run...)
		reach = math.Max(reach, end)
		i = j + 1
	}
	return reach, used
}

// moments counts the words of a run that came up at their own time: the
// two lines of one caption come up together.
func moments(run []piece) int {
	n, last := 0, math.Inf(-1)
	for _, p := range run {
		if p.s-last > 0.15 {
			n++
		}
		last = p.s
	}
	return n
}
