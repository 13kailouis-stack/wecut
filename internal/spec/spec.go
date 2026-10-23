// Package spec defines the WeCut project format (wecut.project/1): the JSON
// document an agent writes to describe an edit. Everything here is pure data
// and checks; nothing touches the disk or runs FFmpeg.
//
// Time is always in seconds. Times on the output timeline (texts, overlays,
// audio, filters) are seconds of the finished video. Clip `from`/`to` are
// seconds of the SOURCE file. Positions are fractions of the canvas (0..1,
// 0.5 = centre). Font sizes are pixels at 1080p and scale with the output.
package spec

import "encoding/json"

// Version is the only project format version this build understands.
const Version = "1"

type Project struct {
	Wecut    string    `json:"wecut"`
	Output   Output    `json:"output"`
	Clips    []Clip    `json:"clips"`
	Texts    []Text    `json:"texts,omitempty"`
	Captions *Captions `json:"captions,omitempty"`
	Overlays []Overlay `json:"overlays,omitempty"`
	Audio    []Track   `json:"audio,omitempty"`
	Filters  []Filter  `json:"filters,omitempty"`
	Effects  []Effect  `json:"effects,omitempty"`
}

type Output struct {
	File       string `json:"file,omitempty"`       // .mp4 or .gif, inside the workspace
	Ratio      string `json:"ratio,omitempty"`      // 9:16 (default), 16:9, 1:1, 4:5, 4:3, 3:4, 2:1, original
	Res        int    `json:"res,omitempty"`        // short side in pixels: 480, 540, 720, 1080 (default), 1440, 2160
	FPS        int    `json:"fps,omitempty"`        // 24, 25, 30 (default), 50, 60
	Fit        string `json:"fit,omitempty"`        // cover (default) or contain
	Background string `json:"background,omitempty"` // behind "contain" clips: #RRGGBB (default #000000) or "blur"
	Blur       int    `json:"blur,omitempty"`       // blur strength 1..4 when background is "blur" (default 2)
	Quality    string `json:"quality,omitempty"`    // draft, final (default), high
}

type Clip struct {
	Src        string      `json:"src"`
	From       float64     `json:"from,omitempty"`     // video: source start
	To         *float64    `json:"to,omitempty"`       // video: source end (default: end of file)
	Duration   *float64    `json:"duration,omitempty"` // image: seconds on screen (default 3)
	Speed      float64     `json:"speed,omitempty"`    // 0.25..4 (default 1)
	Volume     *float64    `json:"volume,omitempty"`   // percent, 0..200 (default 100)
	Mute       bool        `json:"mute,omitempty"`
	Fit        string      `json:"fit,omitempty"`   // overrides output.fit
	Focus      *Point      `json:"focus,omitempty"` // cover crop centre (default 0.5, 0.5)
	Zoom       float64     `json:"zoom,omitempty"`  // >= 1, cover only (default 1)
	Rotate     int         `json:"rotate,omitempty"`
	Flip       bool        `json:"flip,omitempty"`
	FadeIn     float64     `json:"fadeIn,omitempty"`
	FadeOut    float64     `json:"fadeOut,omitempty"`
	Motion     string      `json:"motion,omitempty"` // image only: in, out, left, right, up, down
	Transition *Transition `json:"transition,omitempty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Transition blends this clip into the NEXT one. The two clips overlap by
// `duration`, so the video gets that much shorter.
type Transition struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration,omitempty"` // default 0.5
}

// Look holds the visual fields shared by texts and captions. Empty strings
// and nil pointers mean "use the style's value".
type Look struct {
	Style             string   `json:"style,omitempty"`
	Font              string   `json:"font,omitempty"`
	Size              float64  `json:"size,omitempty"`
	Color             string   `json:"color,omitempty"`
	Stroke            string   `json:"stroke,omitempty"`     // #RRGGBB or "none"
	Background        string   `json:"background,omitempty"` // #RRGGBB or "none"
	BackgroundOpacity *float64 `json:"backgroundOpacity,omitempty"`
	Shadow            *bool    `json:"shadow,omitempty"`
	Opacity           *float64 `json:"opacity,omitempty"`
	LetterSpacing     float64  `json:"letterSpacing,omitempty"`
	Uppercase         *bool    `json:"uppercase,omitempty"`
	Align             string   `json:"align,omitempty"` // left, center, right
}

type Text struct {
	Text   string   `json:"text"`
	Start  float64  `json:"start,omitempty"`
	End    *float64 `json:"end,omitempty"` // default: end of video
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	Rotate float64  `json:"rotate,omitempty"`
	In     *Anim    `json:"in,omitempty"`
	Out    *Anim    `json:"out,omitempty"`
	Look
}

type Anim struct {
	Type     string  `json:"type"`               // in: fade, pop, up, type; out: fade, down, shrink
	Duration float64 `json:"duration,omitempty"` // default 0.4 (type: 1.0)
}

type Captions struct {
	// Words is a path to a transcript file (.json word list, Whisper JSON,
	// .srt, .vtt) or an inline array of {word, start, end}.
	Words       json.RawMessage `json:"words"`
	Src         string          `json:"src,omitempty"`  // transcript times are seconds of THIS source file
	Mode        string          `json:"mode,omitempty"` // chunk, karaoke, pop (default from style)
	Highlight   string          `json:"highlight,omitempty"`
	MaxWords    int             `json:"maxWords,omitempty"`
	MaxDuration float64         `json:"maxDuration,omitempty"`
	X           *float64        `json:"x,omitempty"`
	Y           *float64        `json:"y,omitempty"`
	Look
}

type Overlay struct {
	Src     string   `json:"src"` // png, jpg or webp
	Start   float64  `json:"start,omitempty"`
	End     *float64 `json:"end,omitempty"`
	X       *float64 `json:"x,omitempty"`
	Y       *float64 `json:"y,omitempty"`
	Width   float64  `json:"width,omitempty"` // fraction of canvas width (default 0.3)
	Opacity *float64 `json:"opacity,omitempty"`
	Rotate  float64  `json:"rotate,omitempty"`
	FadeIn  float64  `json:"fadeIn,omitempty"`
	FadeOut float64  `json:"fadeOut,omitempty"`
}

type Track struct {
	Src      string   `json:"src"`
	At       float64  `json:"at,omitempty"`   // output time where it starts
	From     float64  `json:"from,omitempty"` // seconds into the audio file
	To       *float64 `json:"to,omitempty"`
	Duration *float64 `json:"duration,omitempty"` // cap on how long it plays
	Volume   *float64 `json:"volume,omitempty"`   // percent, default 100
	FadeIn   float64  `json:"fadeIn,omitempty"`
	FadeOut  float64  `json:"fadeOut,omitempty"`
	Loop     bool     `json:"loop,omitempty"`
	Duck     bool     `json:"duck,omitempty"` // lower this track while clips have sound
}

type Filter struct {
	Name   string   `json:"name"`
	Amount *float64 `json:"amount,omitempty"` // 0..100, default 100
	Start  float64  `json:"start,omitempty"`
	End    *float64 `json:"end,omitempty"`
}

type Effect struct {
	Name  string   `json:"name"`
	Start float64  `json:"start,omitempty"`
	End   *float64 `json:"end,omitempty"` // default: start + the effect's own length
}

// Issue is one problem found in a project. Path is a JSON pointer into the
// project ("/clips/0/to"); Hint says how to fix it.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func F(v float64) *float64 { return &v }
func B(v bool) *bool       { return &v }
