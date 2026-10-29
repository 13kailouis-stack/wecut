package clone

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/13kailouis-stack/wecut/internal/analyze"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// Reading is the words read from one crop, by OCR or by a model looking at
// it. Lines keep the original line breaks; Words (optional) carry boxes in
// the crop's pixels, used to find the key words drawn in another colour.
type Reading struct {
	Lines []string `json:"lines"`
	Words []Word   `json:"words,omitempty"`
	// Conf is the reader's mean confidence, 0..100 (0 = not given, as
	// when a model or a person read the crop).
	Conf float64 `json:"conf,omitempty"`
}

type Word struct {
	Text string `json:"text"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// Options for Build.
type Options struct {
	Name   string
	Source string             // the source video, as the caller names it
	Music  string             // file to use as the template's song ("" = none)
	Read   map[string]Reading // words per crop ID (text1, caption1...)
	Crops  []Crop             // from Measure, to find coloured key words
	Tool   string             // e.g. "wecut 0.3.0 clone"
	// Tried means every text crop was offered to a reader (OCR or a
	// model): a block it found no words in is a graphic or scenery, and
	// gets no text slot.
	Tried bool
}

// Result is the template and what was found, for a person or an agent to
// check.
type Result struct {
	Recipe *recipe.Recipe `json:"recipe"`
	Plan   *Plan          `json:"plan"`
	Slots  []SlotInfo     `json:"slots"` // one per media slot, in order
	Hints  []string       `json:"hints"`
}

// SlotInfo describes a media slot for a person filling the template.
type SlotInfo struct {
	N       int     `json:"n"`
	Section string  `json:"section"`
	Start   float64 `json:"start"` // in the template's video
	Dur     float64 `json:"dur"`
	About   string  `json:"about"`
	Ref     float64 `json:"ref"` // a moment of the source shot, for a thumbnail
	Join    string  `json:"join,omitempty"`
}

// Build writes the recipe.
func (a *Analyzer) Build(o Options) *Result {
	p := a.plan
	if p == nil {
		p = a.Analyze()
	}
	res := &Result{Plan: p}
	W, H := a.cfg.W, a.cfg.H
	r := &recipe.Recipe{Recipe: recipe.Version, Name: o.Name, MaxDuration: math.Max(3, math.Ceil(p.ContentEnd*100)/100)}
	r.About = fmt.Sprintf("Cloned from %s: %d shots in %.1f s", baseName(o.Source), len(p.Shots), p.ContentEnd)
	r.Source = &recipe.Source{File: o.Source, Duration: r2(p.Duration), Used: r2(p.ContentEnd), Tool: o.Tool}
	r.Output = &recipe.Output{Ratio: analyze.Ratio(W, H), Res: 1080, FPS: 30}
	if r.Output.Ratio == "other" {
		r.Output.Ratio = "original"
	}
	// The template plays at the original's frame rate, so its cuts fall on
	// the same frames.
	for _, f := range []int{24, 25, 50, 60} {
		if math.Abs(a.cfg.LowFPS-float64(f)) < 0.5 {
			r.Output.FPS = f
		}
	}
	for _, s := range p.Shots {
		if s.Fit == "contain" {
			switch s.Bg {
			case "blur":
				r.Output.Background = "blur"
			case "black":
				if r.Output.Background == "" {
					r.Output.Background = "#000000"
				}
			}
		}
	}

	// Sections: the hook (shots under an opening card), then a new section
	// wherever the framing changes.
	hookEnd := -1.0
	for _, bl := range p.Blocks {
		if bl.Start <= 0.5 && bl.End < p.ContentEnd*0.6 && bl.Look != nil && bl.Look.Background != "" {
			hookEnd = math.Max(hookEnd, bl.End)
		}
	}
	type group struct {
		shots []int
		hook  bool
	}
	var groups []*group
	for k, s := range p.Shots {
		n := len(groups)
		inHook := hookEnd > 0 && s.Start < hookEnd-0.2
		if n > 0 {
			g := groups[n-1]
			last := p.Shots[g.shots[len(g.shots)-1]]
			if g.hook == inHook && (g.hook || last.Fit == s.Fit) {
				g.shots = append(g.shots, k)
				continue
			}
		}
		groups = append(groups, &group{shots: []int{k}, hook: inHook})
	}
	music := o.Music != "" && !p.Speech
	bodyN := 0
	slotN := 0
	offset := 0.0
	for gi, g := range groups {
		sec := recipe.Section{Role: "hook"}
		if !g.hook {
			bodyN++
			sec.Role = "body"
			if bodyN > 1 {
				sec.Role = fmt.Sprintf("body%d", bodyN)
			}
		}
		fits := map[string]int{}
		for _, k := range g.shots {
			fits[p.Shots[k].Fit]++
		}
		sec.Fit = "cover"
		if fits["contain"] > fits["cover"] {
			sec.Fit = "contain"
		}
		var abouts []string
		for j, k := range g.shots {
			s := p.Shots[k]
			sh := recipe.Shot{Dur: r3(s.Dur()), Ref: spec.F(r2(s.Start + s.Dur()/2))}
			if s.Fit != sec.Fit {
				sh.Fit = s.Fit
			}
			if s.Fit == "cover" {
				if s.Zoom > 1 {
					sh.Zoom = s.Zoom
				}
				if s.ZoomTo > 0 {
					sh.ZoomTo = s.ZoomTo
				}
			}
			sh.Continue = s.Continue && j > 0
			sh.About = shotAbout(s, W, H, p.Captions)
			abouts = append(abouts, sh.About)
			if s.Join != "" {
				t := &spec.Transition{Type: transitionName(s.Join), Duration: r2(clampf(s.JoinDur, 0.1, 3))}
				if j == len(g.shots)-1 {
					if gi < len(groups)-1 {
						sec.TransitionOut = t
					}
				} else {
					sh.Join = t
				}
			}
			sec.Shots = append(sec.Shots, sh)
			if !sh.Continue {
				slotN++
			}
			join := ""
			if s.Join != "" && (j < len(g.shots)-1 || gi < len(groups)-1) {
				join = transitionName(s.Join)
			}
			res.Slots = append(res.Slots, SlotInfo{N: len(res.Slots) + 1, Section: sec.Role, Start: r2(offset), Dur: sh.Dur, About: sh.About, Ref: *sh.Ref, Join: join})
			offset += sh.Dur
			if join != "" {
				offset -= s.JoinDur
			}
		}
		sec.About = summarize(abouts)
		if music {
			sec.Mute = true
		}
		if g.hook && p.Speech && gi+1 < len(groups) {
			// The talk is heard from the first second, under the hook.
			sec.Audio = "next"
			sec.About += "; silent B-roll, the next section's talk plays under it"
		}
		r.Sections = append(r.Sections, sec)
	}

	// Text slots.
	reads := o.Read
	named := map[string]bool{}
	slotName := func(bl *Block) string {
		l := bl.Look
		switch {
		case l != nil && l.Background != "" && len(bl.Lines) >= 2 && !named["headline"]:
			return "headline"
		case l != nil && l.Background != "" && len(bl.Lines) == 1 && !named["label"] && isAboveCard(bl, p.Blocks):
			return "label"
		case bl.End >= p.ContentEnd-0.15 && bl.Start > p.ContentEnd*0.6 && !named["cta"]:
			return "cta"
		}
		for n := 1; ; n++ {
			if c := fmt.Sprintf("text%d", n); !named[c] {
				return c
			}
		}
	}
	skipped := 0
	for _, bl := range p.Blocks {
		l := bl.Look
		if l == nil {
			continue
		}
		rd := reads[bl.ID]
		joined := strings.TrimSpace(strings.Join(rd.Lines, " "))
		if (o.Tried && !readable(joined)) || (rd.Conf > 0 && rd.Conf < 62) || strings.HasPrefix(joined, "@") {
			// No words, or a platform's @handle watermark.
			skipped++
			continue
		}
		name := slotName(bl)
		named[name] = true
		words := strings.Join(rd.Lines, "\n")
		sl := recipe.Slot{Slot: name, Start: r2(bl.Start), Text: words}
		if words == "" {
			sl.Text = placeholder(name, len(bl.Lines))
		}
		sl.About = fmt.Sprintf("%d line(s) of %s text", len(bl.Lines), describeLook(l))
		if bl.End < p.ContentEnd-0.1 {
			sl.End = spec.F(r2(bl.End))
		}
		upper := isUpper(words)
		size := sizeFor(l.Glyph, H, min(W, H), words, upper, len(bl.Lines))
		capPx := capHeight(l, H)
		sl.Font, sl.Size, sl.Color = l.Font, size, l.Color
		sl.Align = l.Align
		sl.Shadow = spec.B(false)
		if l.Stroke != "" {
			sl.Stroke = l.Stroke
		} else {
			sl.Stroke = "none"
		}
		if l.Background != "" {
			sl.Background = l.Background
			sl.BackgroundOpacity = spec.F(l.BgOpacity)
			if l.Card {
				sl.Box = "card"
			}
		} else {
			sl.Background = "none"
		}
		if l.Italic {
			sl.Italic = spec.B(true)
		}
		if upper {
			sl.Uppercase = spec.B(true)
		}
		y := bl.Box.cy()
		x := bl.Box.cx()
		switch l.Align {
		case "left":
			x = bl.Box.X
		case "right":
			x = bl.Box.X + bl.Box.W
		}
		if capPx > 0 {
			// Exact layout: size from the capitals, the original's line
			// spacing and card padding, placed by the capitals' middle.
			short := float64(min(W, H))
			fs := capPx / engine.CapRatio(sl.Font)
			// Our font is not the original's: when the words are known, size
			// it so the widest line is as wide as the original's, which keeps
			// the same line breaks inside the same card.
			if lines := rd.Lines; len(lines) == len(l.Left) && len(lines) > 0 {
				k := math.Inf(1)
				for i, ln := range lines {
					text := ln
					if upper {
						text = strings.ToUpper(ln)
					}
					if ours := engine.TextWidth(sl.Font, text, fs); ours > 0 {
						k = math.Min(k, (l.Right[i]-l.Left[i])*float64(W)/ours)
					}
				}
				if k < 1 && k > 0.6 {
					fs *= k
				}
			}
			sl.Size = r2(clampf(fs*1080/short, 8, 400))
			n := len(l.Base)
			y = (l.CapTop[0] + l.Base[n-1]) / 2
			left, right := l.Left[0], l.Right[0]
			for i := range l.Left {
				left, right = math.Min(left, l.Left[i]), math.Max(right, l.Right[i])
			}
			switch l.Align {
			case "left":
				x = left
			case "right":
				x = right
			default:
				x = (left + right) / 2
			}
			sl.LineHeight = 1.2
			if n > 1 {
				var gaps []float64
				for i := 1; i < n; i++ {
					gaps = append(gaps, (l.Base[i]-l.Base[i-1])*float64(H))
				}
				sl.LineHeight = r2(clampf(median(gaps)/fs, 0.8, 2.5))
			}
			if b := l.BoxArea; b != nil {
				// Each side on its own: the space under the last baseline
				// holds the descenders and is larger than the space above.
				k := 1080 / short
				top := (l.CapTop[0] - b.Y) * float64(H) * k
				bottom := (b.Y + b.H - l.Base[n-1]) * float64(H) * k
				lp := (left - b.X) * float64(W) * k
				rp := (b.X + b.W - right) * float64(W) * k
				sl.Padding = []float64{r2(clampf(top, 0, 300)), r2(clampf(rp, 0, 300)), r2(clampf(bottom, 0, 300)), r2(clampf(lp, 0, 300))}
			}
			sl.MaxWidth = r2(clampf(right-left+0.04, 0.2, 1))
			if l.BoxArea != nil {
				sl.MaxWidth = r2(clampf(l.BoxArea.W+0.02, 0.2, 1))
			}
			if n == 1 {
				// A one-line label stays one line: room for longer words or a
				// wider font than the original's.
				sl.MaxWidth = r2(clampf(math.Max(sl.MaxWidth*1.8, 0.6), 0.2, 0.96))
			}
		} else if words != "" {
			sl.MaxWidth = r2(clampf(bl.Box.W+0.14, 0.2, 0.96))
		} else {
			sl.MaxWidth = r2(clampf(bl.Box.W+0.02, 0.2, 0.96))
		}
		sl.X, sl.Y = spec.F(r3(x)), spec.F(r3(y))
		if bl.In != "" {
			sl.In = &spec.Anim{Type: bl.In, Duration: r2(clampf(bl.InDur, 0.05, 2))}
		}
		if l.Emphasis != "" {
			sl.EmphasisColor = l.Emphasis
			sl.Emphasis = emphasized(rd, cropByID(o.Crops, bl.ID), l)
		}
		r.Texts = append(r.Texts, sl)
		bl.Words, bl.Slot = words, name
	}

	// Captions.
	if c := p.Captions; c != nil && c.Look != nil {
		l := c.Look
		cs := &recipe.CaptionStyle{Mode: c.Mode, MaxWords: c.MaxWords, Y: spec.F(r3(c.Y))}
		var sample []string
		for k := 1; k <= 3; k++ {
			sample = append(sample, reads[fmt.Sprintf("caption%d", k)].Lines...)
		}
		upper := isUpper(strings.Join(sample, " "))
		if len(sample) == 0 {
			upper = c.Mode == "pop"
		} else {
			// The words read from the samples say how many show at once.
			var counts []float64
			for _, s := range sample {
				counts = append(counts, float64(len(strings.Fields(s))))
			}
			if n := int(math.Round(median(counts))); n <= 1 {
				cs.Mode, cs.MaxWords = "pop", 1
			} else {
				cs.Mode, cs.MaxWords = "chunk", min(n, 6)
			}
			c.Mode, c.MaxWords = cs.Mode, cs.MaxWords
		}
		cs.Size = sizeFor(l.Glyph, H, min(W, H), strings.Join(sample, " "), upper, 1)
		cs.Font = l.Font
		if l.Heavy && upper {
			cs.Font = "anton"
		}
		if capPx := capHeight(l, H); capPx > 0 && upper {
			cs.Size = r2(clampf(capPx/engine.CapRatio(cs.Font)*1080/float64(min(W, H)), 8, 400))
		}
		cs.Color = l.Color
		cs.Stroke = "none"
		if l.Stroke != "" {
			cs.Stroke = l.Stroke
		}
		if l.Background != "" {
			cs.Background, cs.BackgroundOpacity = l.Background, spec.F(l.BgOpacity)
		} else {
			cs.Background = "none"
		}
		cs.Shadow = spec.B(false)
		cs.Uppercase = spec.B(upper)
		if l.Italic {
			cs.Italic = spec.B(true)
		}
		if c.Mode == "chunk" {
			cs.MaxDuration = 2.2
		}
		r.Captions = cs
	}

	// Logos become picture slots.
	for _, lg := range p.Logos {
		b := lg.Box
		ov := recipe.OverlaySlot{Slot: lg.ID, About: "your logo or watermark (PNG with transparency)", X: spec.F(r3(b.cx())), Y: spec.F(r3(b.cy())), Width: r3(clampf(b.W, 0.02, 1))}
		if lg.Start > 0.2 {
			ov.Start = r2(lg.Start)
		}
		if lg.End < p.ContentEnd-0.2 {
			ov.End = spec.F(r2(lg.End))
		}
		r.Overlays = append(r.Overlays, ov)
	}

	if music {
		vol := 100.0
		r.Music = &recipe.Music{Src: o.Music, Volume: &vol, Note: "the original's soundtrack; pass music to use your own or \"none\""}
		if p.Audio != nil && p.Audio.BPM > 0 && p.Audio.BPMConf >= 0.2 {
			r.Music.BPM = p.Audio.BPM
		}
	}

	res.Recipe = r
	res.Hints = a.hints(p, r, slotN, o)
	if skipped > 0 {
		res.Hints = append(res.Hints, fmt.Sprintf("%d laid-over mark(s) with no readable words (a timer, a channel bug, a watermark) were left out of the template", skipped))
	}
	return res
}

func (a *Analyzer) hints(p *Plan, r *recipe.Recipe, slots int, o Options) []string {
	var h []string
	kinds := map[string]int{}
	for _, e := range p.Events {
		kinds[e.Kind]++
	}
	h = append(h, fmt.Sprintf("%d shots (%d media slots) in %.1f s; %d hard cuts", len(p.Shots), slots, p.ContentEnd, kinds["cut"]))
	for _, k := range []string{"flash", "fadeblack", "dissolve", "zoom", "blur"} {
		if kinds[k] > 0 {
			h = append(h, fmt.Sprintf("%d %s transition(s)", kinds[k], k))
		}
	}
	missing := 0
	for _, bl := range p.Blocks {
		if bl.Words == "" {
			missing++
		}
	}
	if missing > 0 {
		h = append(h, fmt.Sprintf("%d text slot(s) have placeholder words: read the crops and pass texts.<slot> when applying", missing))
	}
	if p.Captions != nil {
		h = append(h, fmt.Sprintf("captions (%s, about %.1f changes a second): pass a transcript when applying", p.Captions.Mode, p.Captions.Changes))
	}
	for _, s := range r.Sections {
		if s.Audio == "next" {
			h = append(h, "the hook is silent B-roll with the talk already playing under it: feed it role hook clips and the talk as role body")
		}
	}
	if len(r.Overlays) > 0 {
		h = append(h, "a logo sits on the video the whole time: pass overlays.logo (a PNG) or it is left out")
	}
	if r.Music != nil {
		h = append(h, "the original's soundtrack is the default music; a song taken from someone else's video may be muted by the platform")
	}
	return h
}

// readable says whether OCR found real words, not noise read off a
// texture: a run of three letters or digits, and mostly letters.
func readable(s string) bool {
	run, best, alnum, total := 0, 0, 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			run = 0
			continue
		}
		total++
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnum++
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best >= 3 && alnum*10 >= total*6
}

// capHeight is the median measured capital height in pixels, or 0.
func capHeight(l *Look, H int) float64 {
	if len(l.Base) == 0 || len(l.Base) != len(l.CapTop) {
		return 0
	}
	var hs []float64
	for i := range l.Base {
		hs = append(hs, (l.Base[i]-l.CapTop[i])*float64(H))
	}
	return median(hs)
}

func isAboveCard(bl *Block, all []*Block) bool {
	for _, o := range all {
		if o == bl || o.Look == nil || len(o.Lines) < 2 {
			continue
		}
		gap := o.Box.Y - (bl.Box.Y + bl.Box.H)
		if gap > -0.01 && gap < 0.06 && math.Abs(o.Start-bl.Start) < 0.6 {
			return true
		}
	}
	return false
}

func placeholder(name string, lines int) string {
	switch name {
	case "headline":
		return strings.TrimSpace(strings.Repeat("Your headline here\n", max(1, min(lines, 3))))
	case "label":
		return "LABEL"
	case "cta":
		return "Follow for more"
	}
	return "Your text"
}

// sizeFor turns a measured line height into a font size at 1080: the
// height of letters depends on what they are (capitals, ascenders,
// descenders), so the words help when they are known.
func sizeFor(glyph float64, H, short int, words string, upper bool, lines int) float64 {
	if glyph <= 0 {
		return 64
	}
	ext := 0.95
	if words != "" {
		up, down := 0.53, 0.0
		for _, r := range words {
			switch {
			case unicode.IsUpper(r) || unicode.IsDigit(r) || strings.ContainsRune("bdfhklt", r):
				up = 0.72
			case strings.ContainsRune("gjpqy,", r):
				down = 0.22
			}
		}
		if upper {
			up, down = 0.72, 0
		}
		ext = up + down
	} else if upper || lines == 1 {
		ext = 0.75
	}
	return r2(clampf(glyph*float64(H)/ext*1080/float64(short), 8, 400))
}

func isUpper(s string) bool {
	up, letters := 0, 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				up++
			}
		}
	}
	return letters >= 3 && float64(up)/float64(letters) >= 0.85
}

func transitionName(kind string) string {
	switch kind {
	case "flash":
		return "flash"
	case "fadeblack":
		return "fadeblack"
	case "dissolve":
		return "dissolve"
	case "blur":
		return "hblur"
	case "zoom":
		return "zoomin"
	}
	return "fade"
}

func shotAbout(s ShotInfo, W, H int, c *CaptionBand) string {
	var parts []string
	switch {
	case s.Fit == "contain":
		cw, ch := s.Content.W*float64(W), s.Content.H*float64(H)
		shape := "landscape"
		switch analyze.Ratio(int(cw), int(ch)) {
		case "9:16", "3:4", "4:5":
			shape = "portrait"
		case "1:1":
			shape = "square"
		}
		parts = append(parts, fmt.Sprintf("%s footage shown whole on a %s background", shape, map[string]string{"blur": "blurred", "black": "black"}[s.Bg]))
	case s.Still:
		parts = append(parts, "a photo or screenshot")
	default:
		parts = append(parts, "full-screen footage")
	}
	if s.ZoomTo > s.Zoom && s.Zoom >= 1 {
		parts = append(parts, "slow push in")
	} else if s.ZoomTo > 0 && s.ZoomTo < s.Zoom {
		parts = append(parts, "slow pull out")
	}
	if s.Continue {
		parts = append(parts, "same take as the shot before, punched in")
	}
	if c != nil && s.Start < c.End && s.End > c.Start {
		parts = append(parts, "someone talking (captions run here)")
	}
	return strings.Join(parts, ", ")
}

func summarize(abouts []string) string {
	count := map[string]int{}
	var order []string
	for _, a := range abouts {
		if count[a] == 0 {
			order = append(order, a)
		}
		count[a]++
	}
	var out []string
	for _, a := range order {
		if count[a] > 1 {
			out = append(out, fmt.Sprintf("%dx %s", count[a], a))
		} else {
			out = append(out, a)
		}
	}
	return strings.Join(out, "; ")
}

func describeLook(l *Look) string {
	var p []string
	if l.Background != "" {
		kind := "box"
		if l.Card {
			kind = "card"
		}
		p = append(p, fmt.Sprintf("%s on a %s %s", l.Color, l.Background, kind))
	} else {
		p = append(p, l.Color)
	}
	if l.Stroke != "" {
		p = append(p, "outlined")
	}
	if l.Italic {
		p = append(p, "italic")
	}
	if l.Emphasis != "" {
		p = append(p, "key words in "+l.Emphasis)
	}
	return strings.Join(p, ", ")
}

func baseName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[i+1:]
	}
	return s
}

func cropByID(cs []Crop, id string) *Crop {
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i]
		}
	}
	return nil
}

// emphasized lists the words drawn mostly in the key-word colour.
func emphasized(rd Reading, c *Crop, l *Look) []string {
	if c == nil || c.Image == nil || len(rd.Words) == 0 {
		return nil
	}
	fill, emph := parseHex(l.Color), parseHex(l.Emphasis)
	var out []string
	for _, w := range rd.Words {
		nf, ne := 0, 0
		for y := max(0, w.Y); y < min(c.Image.H, w.Y+w.H); y++ {
			for x := max(0, w.X); x < min(c.Image.W, w.X+w.W); x++ {
				p := px(c.Image, x, y)
				if dist(p, emph) < 70 {
					ne++
				} else if dist(p, fill) < 70 {
					nf++
				}
			}
		}
		if ne > 8 && ne > nf {
			t := strings.Trim(w.Text, ".,!?:;\"'()")
			if t != "" && len(out) < 10 {
				out = append(out, t)
			}
		}
	}
	// Neighbouring key words read better as one phrase.
	return out
}
