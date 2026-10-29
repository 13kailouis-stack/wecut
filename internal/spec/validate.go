package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Validate checks everything that can be checked without opening media
// files: required fields, ranges, and names from the catalog. Checks that
// need the files (do they exist, is `to` past the end) happen in the engine.
func Validate(p *Project) []Issue {
	v := &validator{}
	v.project(p)
	return v.out
}

type validator struct{ out []Issue }

func (v *validator) add(code, path, msg, hint string) {
	v.out = append(v.out, Issue{Code: code, Path: path, Message: msg, Hint: hint})
}

func (v *validator) rng(path string, val, lo, hi float64, what string) {
	if val < lo || val > hi {
		v.add("E_VALUE", path, fmt.Sprintf("%s must be between %s and %s, got %s", what, num(lo), num(hi), num(val)), "")
	}
}

func (v *validator) oneOf(path, val string, allowed []string, what string) bool {
	if slices.Contains(allowed, val) {
		return true
	}
	v.add("E_VALUE", path, fmt.Sprintf("unknown %s %q", what, val), "Use one of: "+strings.Join(allowed, ", ")+". Run `wecut list` for details.")
	return false
}

var hexRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (v *validator) color(path, c string, allowNone bool) {
	if c == "" || (allowNone && c == "none") || hexRe.MatchString(c) {
		return
	}
	hint := "Use #RRGGBB, for example #FFE14D."
	if allowNone {
		hint += ` Use "none" to turn it off.`
	}
	v.add("E_VALUE", path, fmt.Sprintf("invalid colour %q", c), hint)
}

// plainPath reports whether s looks like a usable workspace path.
func (v *validator) plainPath(path, s string) bool {
	switch {
	case strings.TrimSpace(s) == "":
		v.add("E_VALUE", path, "a file path is required", "Give a path relative to the workspace, for example \"media/clip.mp4\".")
	case strings.Contains(s, "://"):
		v.add("E_PATH", path, "URLs are not accepted", "Download the file into the workspace first, then use its relative path.")
	case strings.ContainsAny(s, "%\x00\n\r"):
		v.add("E_PATH", path, "path contains characters that are not allowed (%, control characters)", "Rename the file.")
	default:
		return true
	}
	return false
}

func (v *validator) src(path, s string, kinds string) {
	if !v.plainPath(path, s) {
		return
	}
	ext := strings.ToLower(filepath.Ext(s))
	if !slices.Contains(ExtsFor(kinds), ext) {
		v.add("E_MEDIA", path, fmt.Sprintf("unsupported file type %q", ext), "Allowed here: "+strings.Join(ExtsFor(kinds), " "))
	}
}

var (
	VideoExts = []string{".mp4", ".mov", ".m4v", ".mkv", ".webm"}
	ImageExts = []string{".png", ".jpg", ".jpeg", ".webp"}
	AudioExts = []string{".mp3", ".m4a", ".aac", ".wav", ".ogg", ".opus", ".flac"}
)

// ExtsFor returns the accepted extensions for a mix of kinds ("vi" = video
// or image, "a" = audio...).
func ExtsFor(kinds string) []string {
	var out []string
	if strings.Contains(kinds, "v") {
		out = append(out, VideoExts...)
	}
	if strings.Contains(kinds, "i") {
		out = append(out, ImageExts...)
	}
	if strings.Contains(kinds, "a") {
		out = append(out, AudioExts...)
		out = append(out, VideoExts...) // music can come from a video file
	}
	return out
}

func (v *validator) window(path string, start float64, end *float64) {
	if start < 0 {
		v.add("E_VALUE", path+"/start", "start must be 0 or more", "")
	}
	if end != nil && *end <= start {
		v.add("E_VALUE", path+"/end", fmt.Sprintf("end (%s) must be after start (%s)", num(*end), num(start)), "")
	}
}

func (v *validator) project(p *Project) {
	if p.Wecut != Version {
		got := p.Wecut
		if got == "" {
			got = "missing"
		}
		v.add("E_SCHEMA", "/wecut", fmt.Sprintf("format version must be %q (got %s)", Version, got), `Start the project with "wecut": "1".`)
	}
	v.output(&p.Output)

	if len(p.Clips) == 0 {
		v.add("E_VALUE", "/clips", "at least one clip is required", "Add {\"src\": \"media/video.mp4\"} to clips.")
	}
	v.limit("/clips", len(p.Clips), MaxClips)
	v.limit("/texts", len(p.Texts), MaxTexts)
	v.limit("/overlays", len(p.Overlays), MaxOverlays)
	v.limit("/audio", len(p.Audio), MaxTracks)
	v.limit("/filters", len(p.Filters)+len(p.Effects), MaxFilters)

	for i := range p.Clips {
		v.clip(fmt.Sprintf("/clips/%d", i), &p.Clips[i], i == len(p.Clips)-1)
	}
	for i := range p.Texts {
		v.text(fmt.Sprintf("/texts/%d", i), &p.Texts[i])
	}
	if p.Captions != nil {
		v.captions("/captions", p.Captions)
	}
	for i, o := range p.Overlays {
		path := fmt.Sprintf("/overlays/%d", i)
		v.src(path+"/src", o.Src, "i")
		v.window(path, o.Start, o.End)
		v.pos(path, o.X, o.Y)
		if o.Width != 0 {
			v.rng(path+"/width", o.Width, 0.02, 1, "width")
		}
		if o.Opacity != nil {
			v.rng(path+"/opacity", *o.Opacity, 0, 100, "opacity")
		}
		v.rng(path+"/rotate", o.Rotate, -360, 360, "rotate")
		v.rng(path+"/fadeIn", o.FadeIn, 0, 10, "fadeIn")
		v.rng(path+"/fadeOut", o.FadeOut, 0, 10, "fadeOut")
	}
	for i, a := range p.Audio {
		path := fmt.Sprintf("/audio/%d", i)
		v.src(path+"/src", a.Src, "a")
		if a.At < 0 {
			v.add("E_VALUE", path+"/at", "at must be 0 or more", "")
		}
		if a.From < 0 {
			v.add("E_VALUE", path+"/from", "from must be 0 or more", "")
		}
		if a.To != nil && *a.To <= a.From {
			v.add("E_VALUE", path+"/to", "to must be after from", "")
		}
		if a.Duration != nil && *a.Duration <= 0 {
			v.add("E_VALUE", path+"/duration", "duration must be more than 0", "")
		}
		if a.Volume != nil {
			v.rng(path+"/volume", *a.Volume, 0, 200, "volume")
		}
		v.rng(path+"/fadeIn", a.FadeIn, 0, 30, "fadeIn")
		v.rng(path+"/fadeOut", a.FadeOut, 0, 30, "fadeOut")
	}
	tints := make([]string, len(Tints))
	for i, t := range Tints {
		tints[i] = t.Key
	}
	for i, f := range p.Filters {
		path := fmt.Sprintf("/filters/%d", i)
		v.oneOf(path+"/name", f.Name, tints, "filter")
		if f.Amount != nil {
			v.rng(path+"/amount", *f.Amount, 0, 100, "amount")
		}
		v.window(path, f.Start, f.End)
	}
	effects := make([]string, len(Effects))
	for i, e := range Effects {
		effects[i] = e.Key
	}
	for i, e := range p.Effects {
		path := fmt.Sprintf("/effects/%d", i)
		v.oneOf(path+"/name", e.Name, effects, "effect")
		v.window(path, e.Start, e.End)
	}
}

func (v *validator) limit(path string, n, max int) {
	if n > max {
		v.add("E_LIMIT", path, fmt.Sprintf("too many items (%d, the limit is %d)", n, max), "Split the edit into several projects.")
	}
}

func (v *validator) output(o *Output) {
	if o.File != "" {
		ext := strings.ToLower(filepath.Ext(o.File))
		if ext != ".mp4" && ext != ".gif" {
			v.add("E_VALUE", "/output/file", fmt.Sprintf("output must end in .mp4 or .gif, got %q", o.File), "")
		}
		if strings.Contains(o.File, "://") || strings.ContainsAny(o.File, "%\x00\n\r") {
			v.add("E_PATH", "/output/file", "output must be a plain file path inside the workspace", "")
		}
	}
	if o.Ratio != "" {
		v.oneOf("/output/ratio", o.Ratio, Ratios, "ratio")
	}
	if o.Res != 0 && (o.Res < 240 || o.Res > 2160) {
		v.add("E_VALUE", "/output/res", fmt.Sprintf("res is the short side in pixels, 240 to 2160 (got %d)", o.Res), "Common values: 720, 1080.")
	}
	if o.FPS != 0 && (o.FPS < 1 || o.FPS > 60) {
		v.add("E_VALUE", "/output/fps", fmt.Sprintf("fps must be 1 to 60 (got %d)", o.FPS), "Use 30 for social video.")
	}
	if o.Fit != "" {
		v.oneOf("/output/fit", o.Fit, []string{"cover", "contain"}, "fit")
	}
	if o.Background != "" && o.Background != "blur" {
		if !hexRe.MatchString(o.Background) {
			v.add("E_VALUE", "/output/background", fmt.Sprintf("background must be #RRGGBB or \"blur\", got %q", o.Background), "")
		}
	}
	if o.Blur != 0 && (o.Blur < 1 || o.Blur > 4) {
		v.add("E_VALUE", "/output/blur", "blur must be 1 to 4", "")
	}
	if o.Quality != "" {
		v.oneOf("/output/quality", o.Quality, Qualities, "quality")
	}
}

func (v *validator) clip(path string, c *Clip, last bool) {
	v.src(path+"/src", c.Src, "vi")
	if c.From < 0 {
		v.add("E_VALUE", path+"/from", "from must be 0 or more", "")
	}
	if c.To != nil && *c.To <= c.From {
		v.add("E_VALUE", path+"/to", fmt.Sprintf("to (%s) must be after from (%s)", num(*c.To), num(c.From)), "")
	}
	if c.Duration != nil {
		v.rng(path+"/duration", *c.Duration, 0.1, MaxDuration, "duration")
	}
	if c.Speed != 0 {
		v.rng(path+"/speed", c.Speed, 0.25, 4, "speed")
	}
	if c.Volume != nil {
		v.rng(path+"/volume", *c.Volume, 0, 200, "volume")
	}
	if c.Fit != "" {
		v.oneOf(path+"/fit", c.Fit, []string{"cover", "contain"}, "fit")
	}
	if c.Focus != nil {
		v.rng(path+"/focus/x", c.Focus.X, 0, 1, "focus.x")
		v.rng(path+"/focus/y", c.Focus.Y, 0, 1, "focus.y")
	}
	if c.Zoom != 0 {
		v.rng(path+"/zoom", c.Zoom, 1, 5, "zoom")
	}
	if c.ZoomTo != 0 {
		v.rng(path+"/zoomTo", c.ZoomTo, 1, 5, "zoomTo")
		if c.Motion != "" {
			v.add("E_VALUE", path+"/zoomTo", "zoomTo and motion both move the picture; use one", "motion is a preset for photos, zoomTo works on video and photos.")
		}
	}
	if !slices.Contains([]int{0, 90, 180, 270, -90}, c.Rotate) {
		v.add("E_VALUE", path+"/rotate", fmt.Sprintf("rotate must be 0, 90, 180 or 270 (got %d)", c.Rotate), "")
	}
	v.rng(path+"/fadeIn", c.FadeIn, 0, 10, "fadeIn")
	v.rng(path+"/fadeOut", c.FadeOut, 0, 10, "fadeOut")
	if c.Motion != "" {
		v.oneOf(path+"/motion", c.Motion, Motions, "motion")
	}
	if c.Transition != nil {
		if _, ok := Transitions[c.Transition.Type]; !ok {
			v.add("E_VALUE", path+"/transition/type", fmt.Sprintf("unknown transition %q", c.Transition.Type), "Common: fade, fadeblack, flash, slideleft, wipeleft, zoomin. Run `wecut list transitions`.")
		}
		if c.Transition.Duration != 0 {
			v.rng(path+"/transition/duration", c.Transition.Duration, 0.1, 3, "transition duration")
		}
		if last {
			v.add("E_VALUE", path+"/transition", "the last clip has no next clip to transition into", "Remove the transition, or move it to the clip before.")
		}
	}
}

func (v *validator) pos(path string, x, y *float64) {
	if x != nil {
		v.rng(path+"/x", *x, 0, 1, "x")
	}
	if y != nil {
		v.rng(path+"/y", *y, 0, 1, "y")
	}
}

func (v *validator) look(path string, l *Look, styles []string) {
	if l.Style != "" {
		v.oneOf(path+"/style", l.Style, styles, "style")
	}
	if l.Font != "" {
		keys := make([]string, len(Fonts))
		for i, f := range Fonts {
			keys[i] = f.Key
		}
		v.oneOf(path+"/font", l.Font, keys, "font")
	}
	if l.Size != 0 {
		v.rng(path+"/size", l.Size, 8, 400, "size")
	}
	v.color(path+"/color", l.Color, false)
	v.color(path+"/stroke", l.Stroke, true)
	v.color(path+"/background", l.Background, true)
	if l.BackgroundOpacity != nil {
		v.rng(path+"/backgroundOpacity", *l.BackgroundOpacity, 0, 100, "backgroundOpacity")
	}
	if l.Opacity != nil {
		v.rng(path+"/opacity", *l.Opacity, 0, 100, "opacity")
	}
	v.rng(path+"/letterSpacing", l.LetterSpacing, -10, 50, "letterSpacing")
	if l.Align != "" {
		v.oneOf(path+"/align", l.Align, Aligns, "align")
	}
	if l.Box != "" {
		v.oneOf(path+"/box", l.Box, Boxes, "box")
	}
}

func (v *validator) text(path string, t *Text) {
	if strings.TrimSpace(t.Text) == "" {
		v.add("E_VALUE", path+"/text", "text is empty", "")
	}
	v.window(path, t.Start, t.End)
	v.pos(path, t.X, t.Y)
	v.rng(path+"/rotate", t.Rotate, -360, 360, "rotate")
	if t.MaxWidth != 0 {
		v.rng(path+"/maxWidth", t.MaxWidth, 0.1, 1, "maxWidth")
	}
	if t.LineHeight != 0 {
		v.rng(path+"/lineHeight", t.LineHeight, 0.6, 3, "lineHeight")
	}
	if n := len(t.Padding); n == 3 || n > 4 {
		v.add("E_VALUE", path+"/padding", "padding is one number, [x, y] or [top, right, bottom, left]", "")
	}
	for i, p := range t.Padding {
		v.rng(fmt.Sprintf("%s/padding/%d", path, i), p, 0, 300, "padding")
	}
	keys := make([]string, len(TextStyles))
	for i, s := range TextStyles {
		keys[i] = s.Key
	}
	v.look(path, &t.Look, keys)
	if t.In != nil && v.oneOf(path+"/in/type", t.In.Type, AnimIn, "in animation") && t.In.Duration != 0 {
		v.rng(path+"/in/duration", t.In.Duration, 0.05, 5, "in.duration")
	}
	if t.Out != nil && v.oneOf(path+"/out/type", t.Out.Type, AnimOut, "out animation") && t.Out.Duration != 0 {
		v.rng(path+"/out/duration", t.Out.Duration, 0.05, 5, "out.duration")
	}
	if len(t.Emphasis) > 10 {
		v.add("E_LIMIT", path+"/emphasis", "at most 10 emphasised phrases per text", "")
	}
	for i, e := range t.Emphasis {
		if strings.TrimSpace(e) == "" {
			v.add("E_VALUE", fmt.Sprintf("%s/emphasis/%d", path, i), "empty phrase", "")
		}
	}
	v.color(path+"/emphasisColor", t.EmphasisColor, false)
	if len(t.Emphasis) > 0 && t.In != nil && t.In.Type == "type" {
		v.add("E_VALUE", path+"/emphasis", "emphasis cannot be combined with the typing animation", "Use pop, up or fade, or drop the emphasis.")
	}
}

func (v *validator) captions(path string, c *Captions) {
	w := bytes.TrimSpace(c.Words)
	switch {
	case len(w) == 0 || string(w) == "null":
		v.add("E_VALUE", path+"/words", "words is required", "Give a transcript path (.json, .srt, .vtt) or an inline array of {\"word\", \"start\", \"end\"}.")
	case w[0] == '"':
		var s string
		if json.Unmarshal(w, &s) == nil && v.plainPath(path+"/words", s) {
			ext := strings.ToLower(filepath.Ext(s))
			if !slices.Contains([]string{".json", ".srt", ".vtt"}, ext) {
				v.add("E_MEDIA", path+"/words", fmt.Sprintf("transcript must be .json, .srt or .vtt, got %q", ext), "")
			}
		}
	case w[0] != '[':
		v.add("E_SCHEMA", path+"/words", "words must be a file path or an array", "")
	}
	if c.Src != "" {
		v.src(path+"/src", c.Src, "v")
	}
	if c.Mode != "" {
		v.oneOf(path+"/mode", c.Mode, CaptionModes, "caption mode")
	}
	v.color(path+"/highlight", c.Highlight, true)
	if c.MaxWords != 0 && (c.MaxWords < 1 || c.MaxWords > 12) {
		v.add("E_VALUE", path+"/maxWords", "maxWords must be 1 to 12", "")
	}
	if c.MaxDuration != 0 {
		v.rng(path+"/maxDuration", c.MaxDuration, 0.5, 10, "maxDuration")
	}
	v.pos(path, c.X, c.Y)
	keys := make([]string, len(CaptionStyles))
	for i, s := range CaptionStyles {
		keys[i] = s.Key
	}
	v.look(path, &c.Look, keys)
}

func num(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", f), "0"), ".")
}
