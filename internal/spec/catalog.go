package spec

import "sort"

// Everything an agent can name in a project lives here, with a one-line
// description that `wecut list` prints. Keys are stable: renaming one breaks
// projects that agents have already written.

type Font struct {
	Key, File, Family, About string
	Bold                     bool
	// Em converts a CSS-style font size into the size libass expects. libass
	// sizes a font by usWinAscent+usWinDescent, so the same number draws
	// Poppins at 57% of its CSS size. Em = (winAscent+winDescent)/unitsPerEm,
	// measured from the bundled files.
	Em float64
}

// Fonts ship inside the binary (Latin subsets from Google Fonts). Family is
// the name libass matches; the Montserrat file really is called "Montserrat
// Thin" with a Bold face.
var Fonts = []Font{
	{"roboto", "roboto-700.ttf", "Roboto", "neutral sans, bold", true, 1.2002},
	{"montserrat", "montserrat-700.ttf", "Montserrat Thin", "geometric sans, bold (default)", true, 1.5620},
	{"poppins", "poppins-700.ttf", "Poppins", "round geometric sans, bold", true, 1.7620},
	{"oswald", "oswald-700.ttf", "Oswald", "condensed sans, bold", true, 1.7020},
	{"anton", "anton-400.ttf", "Anton", "tall heavy condensed, for hooks", false, 1.7334},
	{"bangers", "bangers-400.ttf", "Bangers", "comic book caps", false, 1.7570},
	{"archivo-black", "archivo-black-400.ttf", "Archivo Black", "wide heavy sans", false, 1.3470},
	{"pacifico", "pacifico-400.ttf", "Pacifico", "casual brush script", false, 1.9350},
	{"lobster", "lobster-400.ttf", "Lobster", "bold retro script", false, 1.4900},
	{"permanent-marker", "permanent-marker-400.ttf", "Permanent Marker", "handwritten marker", false, 1.4268},
	{"playfair", "playfair-display-700.ttf", "Playfair Display", "elegant serif, bold", true, 1.4100},
	{"dancing-script", "dancing-script-700.ttf", "Dancing Script", "flowing script, bold", true, 1.4020},
	{"caveat", "caveat-400.ttf", "Caveat", "quick handwriting", false, 1.2890},
}

func FontByKey(k string) (Font, bool) {
	for _, f := range Fonts {
		if f.Key == k {
			return f, true
		}
	}
	return Font{}, false
}

// TextStyle is a ready-made look. Size is pixels at 1080p.
type TextStyle struct {
	Key, About string
	Font       string
	Size       float64
	Color      string
	Stroke     string // "" = none
	Background string // "" = none
	BgOpacity  float64
	Shadow     bool
	Uppercase  bool
	Italic     bool
	Spacing    float64
}

var TextStyles = []TextStyle{
	{Key: "outline", About: "white with black outline (default)", Font: "montserrat", Size: 64, Color: "#FFFFFF", Stroke: "#000000"},
	{Key: "plain", About: "white, no outline", Font: "montserrat", Size: 64, Color: "#FFFFFF"},
	{Key: "outline-dark", About: "near-black with white outline", Font: "montserrat", Size: 64, Color: "#111111", Stroke: "#FFFFFF"},
	{Key: "yellow", About: "yellow with black outline", Font: "montserrat", Size: 64, Color: "#FFE14D", Stroke: "#000000"},
	{Key: "box-black", About: "white on a black box", Font: "poppins", Size: 60, Color: "#FFFFFF", Background: "#000000", BgOpacity: 85},
	{Key: "box-white", About: "black on a white box", Font: "poppins", Size: 60, Color: "#111111", Background: "#FFFFFF", BgOpacity: 100},
	{Key: "box-red", About: "white on a red box", Font: "poppins", Size: 60, Color: "#FFFFFF", Background: "#FE2C55", BgOpacity: 100},
	{Key: "box-yellow", About: "black on a yellow box", Font: "poppins", Size: 60, Color: "#111111", Background: "#FFE14D", BgOpacity: 100},
	{Key: "shadow", About: "white with a soft drop shadow", Font: "poppins", Size: 64, Color: "#FFFFFF", Shadow: true},
	{Key: "pink", About: "pink with white outline", Font: "montserrat", Size: 64, Color: "#FF5C9A", Stroke: "#FFFFFF"},
	{Key: "cyan", About: "cyan with black outline", Font: "montserrat", Size: 64, Color: "#27E0F5", Stroke: "#000000"},
	{Key: "hook", About: "big heavy caps for the first 3 seconds", Font: "anton", Size: 110, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true},
	{Key: "title", About: "clean bold title", Font: "archivo-black", Size: 78, Color: "#FFFFFF", Shadow: true},
	{Key: "comic", About: "comic caps with outline", Font: "bangers", Size: 90, Color: "#FFE14D", Stroke: "#000000", Spacing: 2},
	{Key: "headline", About: "news-style headline card: dark bold italic on a white box (add emphasis for red words)", Font: "poppins", Size: 52, Color: "#111111", Background: "#FFFFFF", BgOpacity: 100, Italic: true},
	{Key: "label", About: "small tag above a headline: white italic on a red box", Font: "poppins", Size: 34, Color: "#FFFFFF", Background: "#E53935", BgOpacity: 100, Italic: true},
	{Key: "quote", About: "dark text on white boxes, line by line, like a quoted comment", Font: "montserrat", Size: 46, Color: "#111111", Background: "#FFFFFF", BgOpacity: 92},
}

func TextStyleByKey(k string) (TextStyle, bool) {
	for _, s := range TextStyles {
		if s.Key == k {
			return s, true
		}
	}
	return TextStyle{}, false
}

// CaptionStyle is a TextStyle plus how words appear over time.
type CaptionStyle struct {
	TextStyle
	Mode      string // chunk, karaoke, pop
	Highlight string // active word colour (karaoke)
	MaxWords  int
	Y         float64
}

var CaptionStyles = []CaptionStyle{
	{TextStyle{Key: "classic", About: "white with outline, a short phrase at a time (default)", Font: "montserrat", Size: 58, Color: "#FFFFFF", Stroke: "#000000"}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "yellow", About: "yellow with outline", Font: "montserrat", Size: 58, Color: "#FFE14D", Stroke: "#000000"}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "box-black", About: "white on a translucent black box", Font: "poppins", Size: 56, Color: "#FFFFFF", Background: "#000000", BgOpacity: 70}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "box-white", About: "black on a white box", Font: "poppins", Size: 56, Color: "#111111", Background: "#FFFFFF", BgOpacity: 100}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "bold", About: "heavy condensed with outline", Font: "anton", Size: 66, Color: "#FFFFFF", Stroke: "#000000"}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "comic", About: "comic caps", Font: "bangers", Size: 70, Color: "#FFFFFF", Stroke: "#000000", Spacing: 2}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "shadow", About: "white with drop shadow", Font: "poppins", Size: 58, Color: "#FFFFFF", Shadow: true}, "chunk", "", 5, 0.78},
	{TextStyle{Key: "karaoke", About: "caps, the spoken word lights up yellow", Font: "montserrat", Size: 72, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true}, "karaoke", "#FFE14D", 3, 0.7},
	{TextStyle{Key: "karaoke-green", About: "caps, the spoken word lights up green", Font: "poppins", Size: 72, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true}, "karaoke", "#39FF6A", 3, 0.7},
	{TextStyle{Key: "pop", About: "one big word at a time that pops in", Font: "anton", Size: 104, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true}, "pop", "", 1, 0.62},
	{TextStyle{Key: "word", About: "one plain word at a time in the middle, white with outline", Font: "montserrat", Size: 60, Color: "#FFFFFF", Stroke: "#000000"}, "pop", "", 1, 0.58},
	{TextStyle{Key: "phrase", About: "short caps phrases with a soft shadow", Font: "poppins", Size: 50, Color: "#FFFFFF", Shadow: true, Uppercase: true}, "chunk", "", 4, 0.6},
}

func CaptionStyleByKey(k string) (CaptionStyle, bool) {
	for _, s := range CaptionStyles {
		if s.Key == k {
			return s, true
		}
	}
	return CaptionStyle{}, false
}

var CaptionModes = []string{"chunk", "karaoke", "pop"}

// Transitions are FFmpeg xfade names, plus friendly aliases.
var Transitions = map[string]string{
	"fade": "fade", "dissolve": "dissolve", "fadeblack": "fadeblack", "fadewhite": "fadewhite",
	"black": "fadeblack", "white": "fadewhite", "flash": "fadewhite",
	"wipeleft": "wipeleft", "wiperight": "wiperight", "wipeup": "wipeup", "wipedown": "wipedown",
	"slideleft": "slideleft", "slideright": "slideright", "slideup": "slideup", "slidedown": "slidedown",
	"smoothleft": "smoothleft", "smoothright": "smoothright", "smoothup": "smoothup", "smoothdown": "smoothdown",
	"circleopen": "circleopen", "circleclose": "circleclose", "circlecrop": "circlecrop", "rectcrop": "rectcrop",
	"radial": "radial", "pixelize": "pixelize", "hblur": "hblur", "zoomin": "zoomin",
	"squeezeh": "squeezeh", "squeezev": "squeezev", "distance": "distance", "fadegrays": "fadegrays",
	"diagtl": "diagtl", "diagtr": "diagtr", "diagbl": "diagbl", "diagbr": "diagbr",
	"vertopen": "vertopen", "vertclose": "vertclose", "horzopen": "horzopen", "horzclose": "horzclose",
}

func TransitionNames() []string {
	out := make([]string, 0, len(Transitions))
	for k := range Transitions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Tint is a translucent colour laid over the frame, the same filters the
// WeCut app offers. A is the opacity at amount 100.
type Tint struct {
	Key, Color string
	A          float64
}

var Tints = []Tint{
	{"warm", "#FF9A3C", 0.2}, {"cool", "#3C8CFF", 0.18}, {"dusk", "#FF5E3A", 0.22}, {"gold", "#FFC83C", 0.2},
	{"mint", "#3CFFB4", 0.14}, {"leaf", "#7AC943", 0.15}, {"rose", "#FF5C8A", 0.17}, {"lavender", "#9A6BFF", 0.16},
	{"coffee", "#704214", 0.3}, {"pale", "#FFFFFF", 0.22}, {"fog", "#B8C4CC", 0.28}, {"dark", "#000000", 0.28},
	{"retro", "#E8A87C", 0.22}, {"teal", "#00A6A6", 0.15}, {"candy", "#FF8FB1", 0.16}, {"neon", "#FF00E5", 0.11},
	{"ocean", "#0077BE", 0.17}, {"cream", "#F5DEB3", 0.24}, {"sunset", "#FF7F50", 0.18}, {"forest", "#1F5E3A", 0.2},
}

func TintByKey(k string) (Tint, bool) {
	for _, t := range Tints {
		if t.Key == k {
			return t, true
		}
	}
	return Tint{}, false
}

type EffectDef struct {
	Key, About string
	Dur        float64
}

var Effects = []EffectDef{
	{"flash", "white flash, good on a beat or a cut", 0.3},
	{"dim", "darken the frame", 2},
	{"cinema", "black bars top and bottom", 3},
	{"frame", "thin white border", 3},
	{"night", "deep blue night tint", 3},
	{"sepia", "warm old-film tint", 3},
}

func EffectByKey(k string) (EffectDef, bool) {
	for _, e := range Effects {
		if e.Key == k {
			return e, true
		}
	}
	return EffectDef{}, false
}

var (
	Motions     = []string{"in", "out", "left", "right", "up", "down"}
	Ratios      = []string{"9:16", "16:9", "1:1", "4:5", "4:3", "3:4", "2:1", "original"}
	Resolutions = []int{480, 540, 720, 1080, 1440, 2160}
	FrameRates  = []int{24, 25, 30, 50, 60}
	Qualities   = []string{"draft", "final", "high"}
	AnimIn      = []string{"fade", "pop", "up", "type"}
	AnimOut     = []string{"fade", "down", "shrink"}
	Aligns      = []string{"left", "center", "right"}
	Boxes       = []string{"line", "card"}
)

// Limits keep a render from eating the host machine.
const (
	MaxDuration = 600.0 // seconds of output
	MaxClips    = 100   // each clip is an FFmpeg input; segment rendering (later) lifts this
	MaxTexts    = 500
	MaxOverlays = 50
	MaxTracks   = 30
	MaxFilters  = 100
	MaxWords    = 20000
)
