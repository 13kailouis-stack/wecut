// Package recipe is the WeCut template format (wecut.recipe/1): the shape of
// an edit without its content. A recipe says how a video is built (a hook
// section with a headline card, a flash, then the talk with one-word
// captions) but holds no footage, no words and no music, so it can be
// applied to new material again and again.
//
// Recipes come from three places: written by an agent after `wecut analyze`
// on a video that worked, made from a finished project (`wecut recipe
// from-project`, the "save as template" of an editor), or written by hand.
package recipe

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

const Version = "1"

type Recipe struct {
	Recipe      string        `json:"recipe"`
	Name        string        `json:"name,omitempty"`
	About       string        `json:"about,omitempty"`
	Output      *Output       `json:"output,omitempty"`
	MaxDuration float64       `json:"maxDuration,omitempty"` // seconds; default 60
	Sections    []Section     `json:"sections"`
	Texts       []Slot        `json:"texts,omitempty"` // timed against the whole video
	Captions    *CaptionStyle `json:"captions,omitempty"`
	Music       *Music        `json:"music,omitempty"`
	Effects     []EffectRule  `json:"effects,omitempty"`
	Filters     []spec.Filter `json:"filters,omitempty"`
}

type Output struct {
	Ratio      string `json:"ratio,omitempty"`
	Res        int    `json:"res,omitempty"`
	FPS        int    `json:"fps,omitempty"`
	Background string `json:"background,omitempty"` // behind contain clips: #RRGGBB or blur
	Blur       int    `json:"blur,omitempty"`
}

// Section is one stretch of the video built the same way, fed by the input
// clips that carry its role.
type Section struct {
	Role          string           `json:"role"`
	About         string           `json:"about,omitempty"`
	Duration      float64          `json:"duration,omitempty"` // 0 = whatever is left
	Fit           string           `json:"fit,omitempty"`      // cover or contain
	Shot          float64          `json:"shot,omitempty"`     // cut long clips into shots of about this many seconds (0 = keep whole)
	Zoom          float64          `json:"zoom,omitempty"`     // punch in on every other shot (cover only), e.g. 1.12
	Motion        string           `json:"motion,omitempty"`   // images: in, out, left, right, up, down or alternate
	FadeIn        float64          `json:"fadeIn,omitempty"`
	Mute          bool             `json:"mute,omitempty"`          // silence the footage's own sound (music only)
	Join          *spec.Transition `json:"join,omitempty"`          // between shots inside the section (default: hard cut)
	TransitionOut *spec.Transition `json:"transitionOut,omitempty"` // into the next section
	Texts         []Slot           `json:"texts,omitempty"`         // timed against this section
}

// Slot is a text whose words are filled in when the recipe is applied.
// Times are seconds from the start of its section (or of the video for
// recipe-level slots); FromEnd places it that many seconds before the end.
type Slot struct {
	Slot    string     `json:"slot"`
	About   string     `json:"about,omitempty"`
	Text    string     `json:"text,omitempty"` // example or default words
	Start   float64    `json:"start,omitempty"`
	End     *float64   `json:"end,omitempty"`
	FromEnd *float64   `json:"fromEnd,omitempty"`
	X       *float64   `json:"x,omitempty"`
	Y       *float64   `json:"y,omitempty"`
	Rotate  float64    `json:"rotate,omitempty"`
	In      *spec.Anim `json:"in,omitempty"`
	Out     *spec.Anim `json:"out,omitempty"`
	// EmphasisColor colours the words passed as emphasis when applying.
	EmphasisColor string `json:"emphasisColor,omitempty"`
	spec.Look
}

type CaptionStyle struct {
	Mode        string   `json:"mode,omitempty"`
	Highlight   string   `json:"highlight,omitempty"`
	MaxWords    int      `json:"maxWords,omitempty"`
	MaxDuration float64  `json:"maxDuration,omitempty"`
	X           *float64 `json:"x,omitempty"`
	Y           *float64 `json:"y,omitempty"`
	spec.Look
}

// Music is how the soundtrack is mixed. Src is the default track, like the
// song inside a CapCut template: a workspace audio file, or a video whose
// sound is used. The music passed at apply time replaces it; "none" drops it.
type Music struct {
	At       float64  `json:"at,omitempty"`       // output start time
	Duration *float64 `json:"duration,omitempty"` // output playback length
	Src      string   `json:"src,omitempty"`
	From     float64  `json:"from,omitempty"` // start point in the track, e.g. the drop
	Volume   *float64 `json:"volume,omitempty"`
	Duck     bool     `json:"duck,omitempty"`
	Loop     bool     `json:"loop,omitempty"`
	FadeIn   float64  `json:"fadeIn,omitempty"`
	FadeOut  float64  `json:"fadeOut,omitempty"`
	BPM      float64  `json:"bpm,omitempty"`
	Note     string   `json:"note,omitempty"` // e.g. "add the trending sound in the app when posting"
}

// EffectRule places an effect: at every cut, at every section change, or
// at a time (negative = seconds before the end).
type EffectRule struct {
	Name  string  `json:"name"`
	On    string  `json:"on"` // cuts, sections, time
	Every int     `json:"every,omitempty"`
	At    float64 `json:"at,omitempty"`
}

var (
	EffectOn = []string{"cuts", "sections", "time"}
	Motions  = append(append([]string{}, spec.Motions...), "alternate")
)

// Validate checks a recipe on its own (no media involved).
func Validate(r *Recipe) []spec.Issue {
	var out []spec.Issue
	add := func(code, path, msg, hint string) {
		out = append(out, spec.Issue{Code: code, Path: path, Message: msg, Hint: hint})
	}
	if r.Recipe != Version {
		add("E_SCHEMA", "/recipe", fmt.Sprintf("recipe format must be %q", Version), `Start the recipe with "recipe": "1".`)
	}
	if r.MaxDuration != 0 && (r.MaxDuration < 3 || r.MaxDuration > spec.MaxDuration) {
		add("E_VALUE", "/maxDuration", fmt.Sprintf("maxDuration must be 3 to %.0f seconds", spec.MaxDuration), "")
	}
	if o := r.Output; o != nil {
		if o.Ratio != "" && !slices.Contains(spec.Ratios, o.Ratio) {
			add("E_VALUE", "/output/ratio", "unknown ratio "+o.Ratio, "Use one of: "+strings.Join(spec.Ratios, ", "))
		}
		if o.Res != 0 && (o.Res < 240 || o.Res > 2160) {
			add("E_VALUE", "/output/res", "res must be 240 to 2160", "")
		}
		if o.FPS != 0 && (o.FPS < 1 || o.FPS > 60) {
			add("E_VALUE", "/output/fps", "fps must be 1 to 60", "")
		}
		if o.Background != "" && o.Background != "blur" && !isHex(o.Background) {
			add("E_VALUE", "/output/background", "background must be #RRGGBB or blur", "")
		}
		if o.Blur != 0 && (o.Blur < 1 || o.Blur > 4) {
			add("E_VALUE", "/output/blur", "blur must be 1 to 4", "")
		}
	}
	if len(r.Sections) == 0 {
		add("E_VALUE", "/sections", "a recipe needs at least one section", `Add {"role": "body"}.`)
	}
	roles := map[string]bool{}
	slots := map[string]bool{}
	for i, s := range r.Sections {
		p := fmt.Sprintf("/sections/%d", i)
		switch {
		case strings.TrimSpace(s.Role) == "":
			add("E_VALUE", p+"/role", "role is required", `Name what feeds the section, e.g. "hook" or "body".`)
		case roles[s.Role]:
			add("E_VALUE", p+"/role", "role "+s.Role+" is used twice", "Give each section its own role.")
		}
		roles[s.Role] = true
		if s.Duration < 0 || s.Duration > spec.MaxDuration {
			add("E_VALUE", p+"/duration", "duration out of range", "")
		}
		if s.Fit != "" && s.Fit != "cover" && s.Fit != "contain" {
			add("E_VALUE", p+"/fit", "fit must be cover or contain", "")
		}
		if s.Shot != 0 && (s.Shot < 0.3 || s.Shot > 60) {
			add("E_VALUE", p+"/shot", "shot must be 0.3 to 60 seconds (0 keeps clips whole)", "")
		}
		if s.Zoom != 0 && (s.Zoom < 1 || s.Zoom > 3) {
			add("E_VALUE", p+"/zoom", "zoom must be 1 to 3", "")
		}
		if s.Motion != "" && !slices.Contains(Motions, s.Motion) {
			add("E_VALUE", p+"/motion", "unknown motion "+s.Motion, "Use one of: "+strings.Join(Motions, ", "))
		}
		if s.FadeIn < 0 || s.FadeIn > 5 {
			add("E_VALUE", p+"/fadeIn", "fadeIn must be 0 to 5", "")
		}
		if t := s.Join; t != nil {
			if _, ok := spec.Transitions[t.Type]; !ok {
				add("E_VALUE", p+"/join/type", "unknown transition "+t.Type, "Run `wecut list transitions`.")
			}
		}
		if t := s.TransitionOut; t != nil {
			if _, ok := spec.Transitions[t.Type]; !ok {
				add("E_VALUE", p+"/transitionOut/type", "unknown transition "+t.Type, "Run `wecut list transitions`.")
			}
			if t.Duration != 0 && (t.Duration < 0.1 || t.Duration > 3) {
				add("E_VALUE", p+"/transitionOut/duration", "transition duration must be 0.1 to 3", "")
			}
			if i == len(r.Sections)-1 {
				add("E_VALUE", p+"/transitionOut", "the last section has no next section", "")
			}
		}
		for j := range s.Texts {
			out = append(out, validateSlot(&s.Texts[j], fmt.Sprintf("%s/texts/%d", p, j), slots)...)
		}
	}
	for j := range r.Texts {
		out = append(out, validateSlot(&r.Texts[j], fmt.Sprintf("/texts/%d", j), slots)...)
	}
	if c := r.Captions; c != nil {
		if c.Style != "" {
			if _, ok := spec.CaptionStyleByKey(c.Style); !ok {
				add("E_VALUE", "/captions/style", "unknown caption style "+c.Style, "Run `wecut list caption-styles`.")
			}
		}
		if c.Mode != "" && !slices.Contains(spec.CaptionModes, c.Mode) {
			add("E_VALUE", "/captions/mode", "mode must be chunk, karaoke or pop", "")
		}
	}
	if m := r.Music; m != nil {
		if m.At < 0 || m.At > spec.MaxDuration {
			add("E_VALUE", "/music/at", "at must be 0 to 600", "")
		}
		if m.Duration != nil && (*m.Duration <= 0 || *m.Duration > spec.MaxDuration) {
			add("E_VALUE", "/music/duration", "duration must be more than 0 and at most 600", "")
		}
		if m.Volume != nil && (*m.Volume < 0 || *m.Volume > 200) {
			add("E_VALUE", "/music/volume", "volume must be 0 to 200", "")
		}
		if strings.Contains(m.Src, "://") || m.Src == "none" {
			add("E_VALUE", "/music/src", "music.src must be a file in the workspace", "Download the track first; leave src out for no default music.")
		}
		if m.From < 0 {
			add("E_VALUE", "/music/from", "from must be 0 or more", "")
		}
	}
	for i, e := range r.Effects {
		p := fmt.Sprintf("/effects/%d", i)
		if _, ok := spec.EffectByKey(e.Name); !ok {
			add("E_VALUE", p+"/name", "unknown effect "+e.Name, "Run `wecut list effects`.")
		}
		if !slices.Contains(EffectOn, e.On) {
			add("E_VALUE", p+"/on", "on must be cuts, sections or time", "")
		}
		if e.Every < 0 {
			add("E_VALUE", p+"/every", "every must be 1 or more", "")
		}
	}
	for i, f := range r.Filters {
		if _, ok := spec.TintByKey(f.Name); !ok {
			add("E_VALUE", fmt.Sprintf("/filters/%d/name", i), "unknown filter "+f.Name, "Run `wecut list filters`.")
		}
	}
	return out
}

func validateSlot(s *Slot, p string, seen map[string]bool) []spec.Issue {
	var out []spec.Issue
	add := func(code, path, msg, hint string) {
		out = append(out, spec.Issue{Code: code, Path: path, Message: msg, Hint: hint})
	}
	switch {
	case strings.TrimSpace(s.Slot) == "":
		add("E_VALUE", p+"/slot", "slot name is required", `e.g. "headline", "label", "cta".`)
	case seen[s.Slot]:
		add("E_VALUE", p+"/slot", "slot "+s.Slot+" is used twice", "")
	}
	seen[s.Slot] = true
	if s.Start < 0 || (s.End != nil && *s.End <= s.Start) {
		add("E_VALUE", p+"/end", "end must be after start", "")
	}
	if s.FromEnd != nil && *s.FromEnd <= 0 {
		add("E_VALUE", p+"/fromEnd", "fromEnd must be more than 0", "")
	}
	if s.Style != "" {
		if _, ok := spec.TextStyleByKey(s.Style); !ok {
			add("E_VALUE", p+"/style", "unknown style "+s.Style, "Run `wecut list styles`.")
		}
	}
	if s.EmphasisColor != "" && !isHex(s.EmphasisColor) {
		add("E_VALUE", p+"/emphasisColor", "invalid colour", "Use #RRGGBB.")
	}
	return out
}

func isHex(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
