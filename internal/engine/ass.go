package engine

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// All text in a WeCut render (titles and captions) is drawn by libass from
// one .ass file. Text never goes into the FFmpeg filter graph, so nothing an
// agent writes can change the graph.

type look struct {
	Font      spec.Font
	Size      float64 // pixels on the output canvas
	Color     string
	Stroke    string // "" = none
	Bg        string // "" = none
	BgOpacity float64
	Shadow    bool
	Opacity   float64 // 0..100
	Spacing   float64
	Upper     bool
	Italic    bool
	Align     string
	Card      bool // one box behind the whole block instead of one per line
	Anchor    int  // \an position, 0 = from Align (middle row)
}

func resolveLook(l spec.Look, base spec.TextStyle, scale float64) look {
	r := look{
		Size: base.Size, Color: base.Color, Stroke: base.Stroke, Bg: base.Background,
		BgOpacity: base.BgOpacity, Shadow: base.Shadow, Opacity: 100, Spacing: base.Spacing,
		Upper: base.Uppercase, Italic: base.Italic, Align: "center",
	}
	fontKey := base.Font
	if l.Font != "" {
		fontKey = l.Font
	}
	r.Font, _ = spec.FontByKey(fontKey)
	if l.Size != 0 {
		r.Size = l.Size
	}
	if l.Color != "" {
		r.Color = l.Color
	}
	switch l.Stroke {
	case "":
	case "none":
		r.Stroke = ""
	default:
		r.Stroke = l.Stroke
	}
	switch l.Background {
	case "":
	case "none":
		r.Bg = ""
	default:
		r.Bg = l.Background
		if r.BgOpacity == 0 {
			r.BgOpacity = 100
		}
	}
	if l.BackgroundOpacity != nil {
		r.BgOpacity = *l.BackgroundOpacity
	}
	if l.Shadow != nil {
		r.Shadow = *l.Shadow
	}
	if l.Opacity != nil {
		r.Opacity = *l.Opacity
	}
	if l.LetterSpacing != 0 {
		r.Spacing = l.LetterSpacing
	}
	if l.Italic != nil {
		r.Italic = *l.Italic
	}
	if l.Uppercase != nil {
		r.Upper = *l.Uppercase
	}
	if l.Align != "" {
		r.Align = l.Align
	}
	box := base.Box
	if l.Box != "" {
		box = l.Box
	}
	r.Card = box == "card" && r.Bg != ""
	r.Size *= scale
	r.Spacing *= scale
	return r
}

// assColor turns #RRGGBB plus an opacity (0..100) into &HAABBGGRR.
func assColor(hex string, opacity float64) string {
	if len(hex) != 7 {
		hex = "#FFFFFF"
	}
	a := 255 - int(math.Round(clamp(opacity, 0, 100)*2.55))
	return fmt.Sprintf("&H%02X%s%s%s", a, strings.ToUpper(hex[5:7]), strings.ToUpper(hex[3:5]), strings.ToUpper(hex[1:3]))
}

// assAlpha is the &HAA& form used by \alpha and \1a overrides.
func assAlpha(opacity float64) string {
	return fmt.Sprintf("&H%02X&", 255-int(math.Round(clamp(opacity, 0, 100)*2.55)))
}

// assBGR is the &HBBGGRR& form used by \c overrides.
func assBGR(hex string) string {
	return fmt.Sprintf("&H%s%s%s&", strings.ToUpper(hex[5:7]), strings.ToUpper(hex[3:5]), strings.ToUpper(hex[1:3]))
}

func (l look) styleLine(name string, W int) string {
	// fs is the visible (CSS-style) size; the style line gets libass units.
	fs := math.Max(1, l.Size)
	assSize := math.Max(1, math.Round(fs*l.Font.Em))
	bold := 0
	if l.Font.Bold {
		bold = -1
	}
	border, outline, outlineCol := 1, 0.0, assColor("#000000", l.Opacity)
	if l.Stroke != "" {
		outline = math.Max(2, math.Round(fs*0.07))
		outlineCol = assColor(l.Stroke, l.Opacity)
	}
	if l.Bg != "" {
		border = 3
		outline = math.Max(4, math.Round(fs*0.22))
		outlineCol = assColor(l.Bg, l.BgOpacity*l.Opacity/100)
	}
	shadow, backCol := 0.0, assColor("#000000", 0)
	if l.Shadow {
		shadow = math.Max(2, math.Round(fs*0.05))
		backCol = assColor("#000000", 62*l.Opacity/100)
	}
	if l.Card {
		// BorderStyle 4 (libass): one box in BackColour around the whole
		// event, Outline is its padding.
		border, shadow = 4, 0
		outline = math.Max(6, math.Round(fs*0.32))
		backCol = outlineCol
	}
	an := map[string]int{"left": 4, "center": 5, "right": 6}[l.Align]
	margin := int(math.Round(float64(W) * 0.06))
	primary := assColor(l.Color, l.Opacity)
	italic := 0
	if l.Italic {
		italic = -1
	}
	return fmt.Sprintf("Style: %s,%s,%s,%s,%s,%s,%s,%d,%d,0,0,100,100,%s,0,%d,%s,%s,%d,%d,%d,0,1",
		name, l.Font.Family, num(assSize), primary, primary, outlineCol, backCol, bold, italic,
		num(round3(l.Spacing)), border, num(outline), num(shadow), an, margin, margin)
}

// escapeASS makes user text literal. libass has escapes for braces (\{ \})
// but none for a backslash, and \N, \n, \h in plain text are line breaks and
// hard spaces. A word joiner (U+2060, invisible) after every backslash keeps
// it from pairing with the next character. Newlines become \N.
func escapeASS(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, `\`, "\\⁠")
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	return strings.ReplaceAll(s, "\n", `\N`)
}

func assTime(t float64) string {
	cs := int(math.Round(math.Max(t, 0) * 100))
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}

type assDoc struct {
	W, H   int
	styles []string
	events []string
	fonts  map[string]bool
}

func (d *assDoc) event(start, end float64, style, text string) {
	d.eventM(start, end, style, 0, text)
}

// eventM writes an event whose lines wrap at the canvas width minus margin
// on each side (0 = the style's margins).
func (d *assDoc) eventM(start, end float64, style string, margin int, text string) {
	if end-start < 0.01 {
		return
	}
	d.events = append(d.events, fmt.Sprintf("Dialogue: 0,%s,%s,%s,,%d,%d,0,,%s", assTime(start), assTime(end), style, margin, margin, text))
}

func (d *assDoc) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\nScaledBorderAndShadow: yes\nYCbCr Matrix: TV.709\n\n", d.W, d.H)
	b.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	for _, s := range d.styles {
		b.WriteString(s + "\n")
	}
	b.WriteString("\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, e := range d.events {
		b.WriteString(e + "\n")
	}
	return b.String()
}

// gradients draws each fade as thin stacked rectangles of rising opacity. They
// come first, so they lie under every text.
func (d *assDoc) gradients(e *Edit, scale float64) {
	cards := false
	for i := range e.Clips {
		cards = cards || (e.Clips[i].Clip.Inset != nil && e.Clips[i].Clip.Inset.Radius > 0)
	}
	if len(e.P.Gradients) == 0 && !cards {
		return
	}
	base := spec.TextStyles[0]
	l := resolveLook(spec.Look{}, base, scale)
	d.styles = append(d.styles, l.styleLine("G", e.W))
	d.fonts[l.Font.Key] = true
	// Rounded card corners: the background's colour over each corner, cut by
	// a curve. Drawn first, under the fades and the text.
	for i := range e.Clips {
		c := &e.Clips[i]
		ins := c.Clip.Inset
		if ins == nil || ins.Radius <= 0 {
			continue
		}
		r := math.Max(2, math.Round(ins.Radius*scale))
		k := r * 0.45
		x0, y0 := math.Round(ins.X*float64(e.W)), math.Round(ins.Y*float64(e.H))
		x1, y1 := x0+math.Round(ins.W*float64(e.W)), y0+math.Round(ins.H*float64(e.H))
		color := assBGR(orStr(ins.Background, "#0E0E0E"))
		for _, corner := range []struct {
			x, y float64
			path string
		}{
			{x0, y0, fmt.Sprintf("m 0 0 l %s 0 b %s 0 0 %s 0 %s", num(r), num(k), num(k), num(r))},
			{x1 - r, y0, fmt.Sprintf("m %s 0 l %s %s b %s %s %s 0 0 0", num(r), num(r), num(r), num(r), num(k), num(r-k))},
			{x0, y1 - r, fmt.Sprintf("m 0 %s l 0 0 b 0 %s %s %s %s %s", num(r), num(r-k), num(k), num(r), num(r), num(r))},
			{x1 - r, y1 - r, fmt.Sprintf("m %s %s l 0 %s b %s %s %s %s %s 0", num(r), num(r), num(r), num(r-k), num(r), num(r), num(r-k), num(r))},
		} {
			d.event(c.Offset, c.Offset+c.Dur, "G", fmt.Sprintf(`{\an7\pos(%s,%s)\bord0\shad0\1c%s\p1}%s{\p0}`, num(corner.x), num(corner.y), color, corner.path))
		}
	}
	rect := func(start, end float64, y0, y1 int, color string, opacity float64) {
		if y1 <= y0 || opacity <= 0 {
			return
		}
		d.event(start, end, "G", fmt.Sprintf(`{\an7\pos(0,%d)\bord0\shad0\1c%s\1a%s\p1}m 0 0 l %d 0 %d %d 0 %d{\p0}`, y0, assBGR(color), assAlpha(opacity), e.W, e.W, y1-y0, y1-y0))
	}
	for _, g := range e.P.Gradients {
		if g.Start >= e.Total {
			continue
		}
		end := e.Total
		if g.End != nil {
			end = math.Min(*g.End, e.Total)
		}
		color := orStr(g.Color, "#000000")
		full := 100.0
		if g.Opacity != nil {
			full = *g.Opacity
		}
		from, to := int(math.Round(g.From*float64(e.H))), int(math.Round(g.To*float64(e.H)))
		span := to - from
		if span < 0 {
			span = -span
		}
		n := max(1, min(96, span/3))
		for k := 0; k < n; k++ {
			a, b := from+(to-from)*k/n, from+(to-from)*(k+1)/n
			if a > b {
				a, b = b, a
			}
			rect(g.Start, end, a, b, color, full*(float64(k)+0.5)/float64(n))
		}
		if to > from {
			rect(g.Start, end, to, e.H, color, full)
		} else {
			rect(g.Start, end, 0, to, color, full)
		}
	}
}

// buildASS returns the subtitle file for all texts and captions, or "" when
// the project has none, plus the font keys it uses.
func buildASS(e *Edit) (string, []string) {
	d := &assDoc{W: e.W, H: e.H, fonts: map[string]bool{}}
	scale := float64(min(e.W, e.H)) / 1080
	d.gradients(e, scale)
	for i, t := range e.P.Texts {
		if t.Start >= e.Total {
			continue
		}
		base, ok := spec.TextStyleByKey(orStr(t.Style, "outline"))
		if !ok {
			base = spec.TextStyles[0]
		}
		l := resolveLook(t.Look, base, scale)
		name := fmt.Sprintf("T%d", i)
		d.styles = append(d.styles, l.styleLine(name, e.W))
		d.fonts[l.Font.Key] = true
		end := e.Total
		if t.End != nil {
			end = math.Min(*t.End, e.Total)
		}
		x, y := 0.5, 0.5
		if t.X != nil {
			x = *t.X
		}
		if t.Y != nil {
			y = *t.Y
		}
		text := t.Text
		if l.Upper {
			text = strings.ToUpper(text)
		}
		body := escapeASS(text)
		if len(t.Emphasis) > 0 {
			color := t.EmphasisColor
			if color == "" {
				color = "#E53935"
			}
			body = emphasize(text, t.Emphasis, color, l.Color)
		}
		if t.LineHeight > 0 && t.Rotate == 0 && d.layoutText(e, &e.P.Texts[i], name, l, scale, t.Start, end, x*float64(e.W), y*float64(e.H)) {
			continue
		}
		margin := 0
		if t.MaxWidth > 0 {
			margin = max(1, int(math.Round(float64(e.W)*(1-t.MaxWidth)/2)))
		}
		d.textEvents(name, l, t.Start, end, x*float64(e.W), y*float64(e.H), t.Rotate, margin, text, body, t.In, t.Out)
	}
	if c := e.P.Captions; c != nil && len(e.Words) > 0 {
		d.captions(e, c, scale)
	}
	if len(d.events) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(d.fonts))
	for _, f := range spec.Fonts {
		if d.fonts[f.Key] {
			keys = append(keys, f.Key)
		}
	}
	return d.String(), keys
}

// layoutText draws a text line by line at an exact spacing, measuring each
// line with the font's own widths, and draws its background as vector
// rectangles sized to the measured text: one card behind the block, or one
// box per line. It reports false (draw it the plain way) when the font
// cannot be measured.
func (d *assDoc) layoutText(e *Edit, t *spec.Text, name string, l look, scale, start, end, px, py float64) bool {
	m, err := metricsFor(l.Font.Key)
	if err != nil {
		return false
	}
	fs := math.Max(1, l.Size)
	text := t.Text
	if l.Upper {
		text = strings.ToUpper(text)
	}
	// Padding: [all], [x, y], or [top, right, bottom, left] like CSS.
	pt, pr, pb, pl := fs*0.32, fs*0.32, fs*0.32, fs*0.32
	switch p := t.Padding; len(p) {
	case 1:
		pt, pr, pb, pl = p[0]*scale, p[0]*scale, p[0]*scale, p[0]*scale
	case 2:
		pl, pr, pt, pb = p[0]*scale, p[0]*scale, p[1]*scale, p[1]*scale
	case 4:
		pt, pr, pb, pl = p[0]*scale, p[1]*scale, p[2]*scale, p[3]*scale
	}
	maxW := float64(e.W) * 0.88
	if t.MaxWidth > 0 {
		maxW = float64(e.W) * t.MaxWidth
	}
	if l.Bg != "" {
		maxW -= pl + pr
	}
	lines := m.wrap(text, fs, l.Spacing, math.Max(fs, maxW))
	n := len(lines)
	pitch := t.LineHeight * fs
	k := fs / m.upm
	asc, capH, cell := m.winAsc*k, m.capH*k, fs*l.Font.Em

	type placed struct {
		text     string
		x0, w, b float64 // left edge, width, baseline
	}
	var ps []placed
	for i, ln := range lines {
		w := m.width(ln, fs, l.Spacing)
		b := py + capH/2 - float64(n-1)*pitch/2 + float64(i)*pitch
		x0 := px
		switch l.Align {
		case "center":
			x0 = px - w/2
		case "right":
			x0 = px - w
		}
		ps = append(ps, placed{ln, x0, w, b})
	}

	// The background first, so the letters draw over it.
	if l.Bg != "" {
		box := l
		box.Bg, box.Card, box.Stroke, box.Shadow, box.Anchor = "", false, "", false, 5
		box.Color, box.Opacity = l.Bg, l.BgOpacity*l.Opacity/100
		bname := name + "B"
		d.styles = append(d.styles, box.styleLine(bname, e.W))
		rect := func(x0, y0, x1, y1 float64) {
			w, h := math.Round(x1-x0), math.Round(y1-y0)
			draw := fmt.Sprintf(`{\p1}m 0 0 l %s 0 %s %s 0 %s{\p0}`, num(w), num(w), num(h), num(h))
			d.textEvents(bname, box, start, end, round1((x0+x1)/2), round1((y0+y1)/2), 0, 0, "", draw, animOnly(t.In), animOnly(t.Out))
		}
		if l.Card || n == 1 {
			x0, x1 := math.Inf(1), math.Inf(-1)
			for _, p := range ps {
				x0, x1 = math.Min(x0, p.x0), math.Max(x1, p.x0+p.w)
			}
			rect(x0-pl, ps[0].b-capH-pt, x1+pr, ps[n-1].b+pb)
		} else {
			for _, p := range ps {
				if strings.TrimSpace(p.text) != "" {
					rect(p.x0-pl, p.b-capH-pt, p.x0+p.w+pr, p.b+pb)
				}
			}
		}
	}

	txt := l
	txt.Bg, txt.Card, txt.Anchor = "", false, 5
	tname := name + "L"
	d.styles = append(d.styles, txt.styleLine(tname, e.W))
	color := t.EmphasisColor
	if color == "" {
		color = "#E53935"
	}
	for _, p := range ps {
		if strings.TrimSpace(p.text) == "" {
			continue
		}
		body := escapeASS(p.text)
		if len(t.Emphasis) > 0 {
			body = emphasize(p.text, t.Emphasis, color, l.Color)
		}
		// \an5 puts the middle of the line's cell (winAscent above the
		// baseline to winDescent below) at the point.
		cy := p.b - asc + cell/2
		d.textEvents(tname, txt, start, end, round1(p.x0+p.w/2), round1(cy), 0, 0, p.text, body, t.In, t.Out)
	}
	return true
}

// animOnly keeps an animation for a background box; typing has no letters
// to reveal there, so the box fades in instead.
func animOnly(a *spec.Anim) *spec.Anim {
	if a == nil || a.Type != "type" {
		return a
	}
	return &spec.Anim{Type: "fade", Duration: math.Min(0.3, animDur(a))}
}

// emphasize escapes text and colours every case-insensitive match of the
// phrases, longest match first, without overlapping.
func emphasize(text string, phrases []string, color, base string) string {
	rs := []rune(text)
	var ps [][]rune
	for _, p := range phrases {
		if p = strings.TrimSpace(p); p != "" {
			ps = append(ps, []rune(p))
		}
	}
	on, off := `{\1c`+assBGR(color)+`}`, `{\1c`+assBGR(base)+`}`
	var b strings.Builder
	plain := 0
	for i := 0; i < len(rs); {
		best := 0
		for _, p := range ps {
			if len(p) > best && i+len(p) <= len(rs) && strings.EqualFold(string(rs[i:i+len(p)]), string(p)) {
				best = len(p)
			}
		}
		if best == 0 {
			i++
			continue
		}
		b.WriteString(escapeASS(string(rs[plain:i])))
		b.WriteString(on + escapeASS(string(rs[i:i+best])) + off)
		i += best
		plain = i
	}
	b.WriteString(escapeASS(string(rs[plain:])))
	return b.String()
}

// textEvents writes one text with optional in/out animations. An animation
// gets its own event so in and out can both move the text. body is the
// escaped (and maybe coloured) text; text is the raw text for typing.
func (d *assDoc) textEvents(style string, l look, start, end, px, py, rot float64, margin int, text, body string, in, out *spec.Anim) {
	event := func(start, end float64, style, text string) { d.eventM(start, end, style, margin, text) }
	inDur, outDur := 0.0, 0.0
	if in != nil {
		inDur = animDur(in)
	}
	if out != nil {
		outDur = animDur(out)
	}
	if span := end - start; inDur+outDur > span {
		k := span / (inDur + outDur)
		inDur, outDur = inDur*k, outDur*k
	}
	an := map[string]int{"left": 4, "center": 5, "right": 6}[l.Align]
	if l.Anchor > 0 {
		an = l.Anchor
	}
	head := func(pos string) string {
		s := fmt.Sprintf(`{\an%d%s`, an, pos)
		if rot != 0 {
			s += `\frz` + num(-rot)
		}
		return s
	}
	pos := fmt.Sprintf(`\pos(%s,%s)`, num(round1(px)), num(round1(py)))
	shift := math.Round(float64(d.H) * 0.04)

	if in != nil && inDur > 0 {
		ms := int(math.Round(inDur * 1000))
		switch in.Type {
		case "fade":
			event(start, start+inDur, style, head(pos)+fmt.Sprintf(`\fad(%d,0)}`, ms)+body)
		case "pop":
			a, b := ms*6/10, ms
			event(start, start+inDur, style, head(pos)+fmt.Sprintf(`\fad(%d,0)\fscx40\fscy40\t(0,%d,\fscx112\fscy112)\t(%d,%d,\fscx100\fscy100)}`, min(120, ms/2), a, a, b)+body)
		case "up":
			mv := fmt.Sprintf(`\move(%s,%s,%s,%s,0,%d)`, num(round1(px)), num(round1(py+shift)), num(round1(px)), num(round1(py)), ms)
			event(start, start+inDur, style, head(mv)+fmt.Sprintf(`\fad(%d,0)}`, ms/2)+body)
		case "type":
			event(start, start+inDur, style, head(pos)+"}"+typewriter(text, ms, l.Opacity))
		}
		start += inDur
	}
	midEnd := end - outDur
	event(start, midEnd, style, head(pos)+"}"+body)
	if out != nil && outDur > 0 {
		ms := int(math.Round(outDur * 1000))
		switch out.Type {
		case "fade":
			event(midEnd, end, style, head(pos)+fmt.Sprintf(`\fad(0,%d)}`, ms)+body)
		case "down":
			mv := fmt.Sprintf(`\move(%s,%s,%s,%s,0,%d)`, num(round1(px)), num(round1(py)), num(round1(px)), num(round1(py+shift)), ms)
			event(midEnd, end, style, head(mv)+fmt.Sprintf(`\fad(0,%d)}`, ms)+body)
		case "shrink":
			event(midEnd, end, style, head(pos)+fmt.Sprintf(`\t(0,%d,\fscx10\fscy10)\fad(0,%d)}`, ms, ms/2)+body)
		}
	}
}

func animDur(a *spec.Anim) float64 {
	if a.Duration > 0 {
		return a.Duration
	}
	if a.Type == "type" {
		return 1
	}
	return 0.4
}

// typewriter reveals text one character at a time over ms milliseconds.
func typewriter(text string, ms int, opacity float64) string {
	n := utf8.RuneCountInString(strings.ReplaceAll(text, "\n", ""))
	if n == 0 {
		return ""
	}
	var b strings.Builder
	hidden, shown := assAlpha(0), assAlpha(opacity)
	i := 0
	for _, r := range text {
		if r == '\n' {
			b.WriteString(`\N`)
			continue
		}
		t := ms * i / n
		fmt.Fprintf(&b, `{\alpha%s\t(%d,%d,\alpha%s)}%s`, hidden, t, t+1, shown, escapeASS(string(r)))
		i++
	}
	return b.String()
}

func (d *assDoc) captions(e *Edit, c *spec.Captions, scale float64) {
	st, ok := spec.CaptionStyleByKey(orStr(c.Style, "classic"))
	if !ok {
		st = spec.CaptionStyles[0]
	}
	l := resolveLook(c.Look, st.TextStyle, scale)
	d.styles = append(d.styles, l.styleLine("CAP", e.W))
	d.fonts[l.Font.Key] = true
	mode := orStr(c.Mode, st.Mode)
	maxWords := st.MaxWords
	if c.Mode != "" && c.Mode != st.Mode {
		maxWords = map[string]int{"chunk": 5, "karaoke": 3, "pop": 1, "reveal": 5}[mode]
	}
	if c.MaxWords > 0 {
		maxWords = c.MaxWords
	}
	maxDur := 2.6
	if c.MaxDuration > 0 {
		maxDur = c.MaxDuration
	}
	hl := st.Highlight
	if c.Highlight != "" {
		hl = c.Highlight
	}
	if hl == "none" || hl == "" {
		hl = "#FFE14D"
	}
	x, y := 0.5, st.Y
	if c.X != nil {
		x = *c.X
	}
	if c.Y != nil {
		y = *c.Y
	}
	an := map[string]int{"left": 4, "center": 5, "right": 6}[l.Align]
	pos := fmt.Sprintf(`{\an%d\pos(%s,%s)`, an, num(round1(x*float64(e.W))), num(round1(y*float64(e.H))))
	word := func(w Word) string {
		if l.Upper {
			return escapeASS(strings.ToUpper(w.Text))
		}
		return escapeASS(w.Text)
	}
	// Key words: named ones, or the longest word of each phrase.
	named := map[string]bool{}
	if c.Accent != nil {
		for _, w := range c.Accent.Words {
			named[accentKey(w)] = true
		}
		if c.Accent.Font != "" {
			d.fonts[c.Accent.Font] = true
		}
	}
	isKey := func(ch Chunk, w Word) bool {
		if c.Accent == nil {
			return false
		}
		if len(named) > 0 {
			return named[accentKey(w.Text)]
		}
		best := ""
		for _, o := range ch.Words {
			if k := accentKey(o.Text); len([]rune(k)) > len([]rune(best)) {
				best = k
			}
		}
		return len([]rune(best)) >= 4 && accentKey(w.Text) == best
	}
	// paint draws one word: lit in the spoken colour, a key word set apart.
	paint := func(ch Chunk, w Word, lit bool) string {
		tags, color := "", ""
		if lit {
			color = hl
		}
		if isKey(ch, w) {
			a := c.Accent
			if a.Scale > 0 {
				tags += fmt.Sprintf(`\fscx%s\fscy%s`, num(a.Scale), num(a.Scale))
			}
			if a.Italic {
				tags += `\i1`
			}
			if f, ok := spec.FontByKey(a.Font); ok {
				tags += `\fn` + f.Family
			}
			if a.Color != "" && (lit || mode != "reveal") {
				color = a.Color
			}
		}
		if color != "" {
			tags += `\1c` + assBGR(color)
		}
		if tags == "" {
			return word(w)
		}
		return "{" + tags + "}" + word(w) + `{\r}`
	}

	for _, ch := range Chunks(e.Words, e.Total, maxWords, maxDur) {
		switch mode {
		case "chunk":
			parts := make([]string, len(ch.Words))
			for i, w := range ch.Words {
				parts[i] = paint(ch, w, false)
			}
			d.event(ch.Start, ch.End, "CAP", pos+"}"+punctSpace.ReplaceAllString(strings.Join(parts, " "), "$1"))
		case "reveal":
			// The words light up one after another and stay lit.
			for i, w := range ch.Words {
				s := math.Max(w.Start, ch.Start)
				if i == 0 {
					s = ch.Start
				}
				end := ch.End
				if i+1 < len(ch.Words) {
					end = math.Min(ch.Words[i+1].Start, ch.End)
				}
				parts := make([]string, len(ch.Words))
				for j, o := range ch.Words {
					parts[j] = paint(ch, o, j <= i)
				}
				d.event(s, end, "CAP", pos+"}"+strings.Join(parts, " "))
			}
		case "karaoke":
			for i, w := range ch.Words {
				s := math.Max(w.Start, ch.Start)
				if i == 0 {
					s = ch.Start
				}
				end := ch.End
				if i+1 < len(ch.Words) {
					end = math.Min(ch.Words[i+1].Start, ch.End)
				}
				parts := make([]string, len(ch.Words))
				for j, o := range ch.Words {
					if j == i {
						parts[j] = fmt.Sprintf(`{\1c%s\fscx108\fscy108}%s{\r}`, assBGR(hl), word(o))
					} else {
						parts[j] = paint(ch, o, false)
					}
				}
				d.event(s, end, "CAP", pos+"}"+strings.Join(parts, " "))
			}
		case "pop":
			for i, w := range ch.Words {
				s := math.Max(w.Start, ch.Start)
				if i == 0 {
					s = ch.Start
				}
				end := ch.End
				if i+1 < len(ch.Words) {
					end = math.Min(ch.Words[i+1].Start, ch.End)
				}
				d.event(s, end, "CAP", pos+`\fscx125\fscy125\t(0,90,\fscx100\fscy100)}`+word(w))
			}
		}
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// accentKey compares words without their case and punctuation.
func accentKey(s string) string {
	return strings.ToLower(strings.Trim(s, ".,!?:;\"'()"))
}
