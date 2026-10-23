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
	Align     string
}

func resolveLook(l spec.Look, base spec.TextStyle, scale float64) look {
	r := look{
		Size: base.Size, Color: base.Color, Stroke: base.Stroke, Bg: base.Background,
		BgOpacity: base.BgOpacity, Shadow: base.Shadow, Opacity: 100, Spacing: base.Spacing,
		Upper: base.Uppercase, Align: "center",
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
	if l.Uppercase != nil {
		r.Upper = *l.Uppercase
	}
	if l.Align != "" {
		r.Align = l.Align
	}
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
	an := map[string]int{"left": 4, "center": 5, "right": 6}[l.Align]
	margin := int(math.Round(float64(W) * 0.06))
	primary := assColor(l.Color, l.Opacity)
	return fmt.Sprintf("Style: %s,%s,%s,%s,%s,%s,%s,%d,0,0,0,100,100,%s,0,%d,%s,%s,%d,%d,%d,0,1",
		name, l.Font.Family, num(assSize), primary, primary, outlineCol, backCol, bold,
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
	if end-start < 0.01 {
		return
	}
	d.events = append(d.events, fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s", assTime(start), assTime(end), style, text))
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

// buildASS returns the subtitle file for all texts and captions, or "" when
// the project has none, plus the font keys it uses.
func buildASS(e *Edit) (string, []string) {
	d := &assDoc{W: e.W, H: e.H, fonts: map[string]bool{}}
	scale := float64(min(e.W, e.H)) / 1080
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
		d.textEvents(name, l, t.Start, end, x*float64(e.W), y*float64(e.H), t.Rotate, text, t.In, t.Out)
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

// textEvents writes one text with optional in/out animations. An animation
// gets its own event so in and out can both move the text.
func (d *assDoc) textEvents(style string, l look, start, end, px, py, rot float64, text string, in, out *spec.Anim) {
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
	head := func(pos string) string {
		s := fmt.Sprintf(`{\an%d%s`, an, pos)
		if rot != 0 {
			s += `\frz` + num(-rot)
		}
		return s
	}
	pos := fmt.Sprintf(`\pos(%s,%s)`, num(round1(px)), num(round1(py)))
	body := escapeASS(text)
	shift := math.Round(float64(d.H) * 0.04)

	if in != nil && inDur > 0 {
		ms := int(math.Round(inDur * 1000))
		switch in.Type {
		case "fade":
			d.event(start, start+inDur, style, head(pos)+fmt.Sprintf(`\fad(%d,0)}`, ms)+body)
		case "pop":
			a, b := ms*6/10, ms
			d.event(start, start+inDur, style, head(pos)+fmt.Sprintf(`\fad(%d,0)\fscx40\fscy40\t(0,%d,\fscx112\fscy112)\t(%d,%d,\fscx100\fscy100)}`, min(120, ms/2), a, a, b)+body)
		case "up":
			mv := fmt.Sprintf(`\move(%s,%s,%s,%s,0,%d)`, num(round1(px)), num(round1(py+shift)), num(round1(px)), num(round1(py)), ms)
			d.event(start, start+inDur, style, head(mv)+fmt.Sprintf(`\fad(%d,0)}`, ms/2)+body)
		case "type":
			d.event(start, start+inDur, style, head(pos)+"}"+typewriter(text, ms, l.Opacity))
		}
		start += inDur
	}
	midEnd := end - outDur
	d.event(start, midEnd, style, head(pos)+"}"+body)
	if out != nil && outDur > 0 {
		ms := int(math.Round(outDur * 1000))
		switch out.Type {
		case "fade":
			d.event(midEnd, end, style, head(pos)+fmt.Sprintf(`\fad(0,%d)}`, ms)+body)
		case "down":
			mv := fmt.Sprintf(`\move(%s,%s,%s,%s,0,%d)`, num(round1(px)), num(round1(py)), num(round1(px)), num(round1(py+shift)), ms)
			d.event(midEnd, end, style, head(mv)+fmt.Sprintf(`\fad(0,%d)}`, ms)+body)
		case "shrink":
			d.event(midEnd, end, style, head(pos)+fmt.Sprintf(`\t(0,%d,\fscx10\fscy10)\fad(0,%d)}`, ms, ms/2)+body)
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
		maxWords = map[string]int{"chunk": 5, "karaoke": 3, "pop": 1}[mode]
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

	for _, ch := range Chunks(e.Words, e.Total, maxWords, maxDur) {
		switch mode {
		case "chunk":
			parts := make([]string, len(ch.Words))
			for i, w := range ch.Words {
				parts[i] = word(w)
			}
			d.event(ch.Start, ch.End, "CAP", pos+"}"+punctSpace.ReplaceAllString(strings.Join(parts, " "), "$1"))
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
						parts[j] = word(o)
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
