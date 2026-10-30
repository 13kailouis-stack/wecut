package spec

import "sort"

// Everything an agent can name in a project lives here, with a one-line
// description that `wecut list` prints. Keys are stable: renaming one breaks
// projects that agents have already written.

type Font struct {
	Key, File, Family, About string
	// Italic is the real italic face of the same family, when the font has
	// one; libass picks it for italic text instead of slanting the letters.
	Italic string
	Bold   bool
	// Em converts a CSS-style font size into the size libass expects. libass
	// sizes a font by usWinAscent+usWinDescent, so the same number draws
	// Poppins at 57% of its CSS size. Em = (winAscent+winDescent)/unitsPerEm,
	// measured from the bundled files.
	Em float64
	// Cap is the capital height as a share of the CSS size (OS/2
	// sCapHeight), for turning a measured letter height into a size.
	Cap    float64
	Weight int    // OS/2 weight class: 400 regular ... 900 black
	Kind   string // sans, condensed, display, horror, serif, script, hand
}

// Fonts ship inside the binary: Latin subsets, except families whose
// licence reserves their name, which ship unmodified (LICENSES.md). Family
// is the name libass matches; the Montserrat file really is called
// "Montserrat Thin" with a Bold face.
var Fonts = []Font{
	{Key: "roboto", File: "roboto-700.ttf", Italic: "", Family: "Roboto", About: "neutral sans, bold", Bold: true, Em: 1.2002, Cap: 0.7109, Weight: 700, Kind: "sans"},
	{Key: "montserrat", File: "montserrat-700.ttf", Italic: "", Family: "Montserrat Thin", About: "geometric sans, bold (default)", Bold: true, Em: 1.5620, Cap: 0.7000, Weight: 700, Kind: "sans"},
	{Key: "poppins-medium", File: "poppins-500.ttf", Italic: "poppins-500-italic.ttf", Family: "Poppins Medium", About: "round geometric sans, medium; real italic", Bold: false, Em: 1.7620, Cap: 0.6970, Weight: 500, Kind: "sans"},
	{Key: "poppins-semibold", File: "poppins-600.ttf", Italic: "poppins-600-italic.ttf", Family: "Poppins SemiBold", About: "round geometric sans, semibold; real italic (news cards)", Bold: false, Em: 1.7620, Cap: 0.7010, Weight: 600, Kind: "sans"},
	{Key: "poppins", File: "poppins-700.ttf", Italic: "poppins-700-italic.ttf", Family: "Poppins", About: "round geometric sans, bold; real italic", Bold: true, Em: 1.7620, Cap: 0.7050, Weight: 700, Kind: "sans"},
	{Key: "poppins-extrabold", File: "poppins-800.ttf", Italic: "poppins-800-italic.ttf", Family: "Poppins ExtraBold", About: "round geometric sans, extra bold; real italic", Bold: false, Em: 1.7620, Cap: 0.7090, Weight: 800, Kind: "sans"},
	{Key: "poppins-black", File: "poppins-900.ttf", Italic: "poppins-900-italic.ttf", Family: "Poppins Black", About: "round geometric sans, black; real italic", Bold: false, Em: 1.7620, Cap: 0.7130, Weight: 900, Kind: "sans"},
	{Key: "kanit", File: "kanit-700.ttf", Italic: "kanit-700-italic.ttf", Family: "Kanit", About: "squarish sans, bold, loved on SEA TikTok; real italic", Bold: true, Em: 1.5800, Cap: 0.6440, Weight: 700, Kind: "sans"},
	{Key: "kanit-black", File: "kanit-900.ttf", Italic: "kanit-900-italic.ttf", Family: "Kanit Black", About: "squarish sans, black; real italic", Bold: false, Em: 1.5800, Cap: 0.6440, Weight: 900, Kind: "sans"},
	{Key: "oswald", File: "oswald-700.ttf", Italic: "", Family: "Oswald", About: "condensed sans, bold", Bold: true, Em: 1.7020, Cap: 0.8100, Weight: 700, Kind: "condensed"},
	{Key: "barlow-condensed", File: "barlow-condensed-700.ttf", Italic: "barlow-condensed-700-italic.ttf", Family: "Barlow Condensed", About: "narrow sans, bold; real italic", Bold: true, Em: 1.3490, Cap: 0.7000, Weight: 700, Kind: "condensed"},
	{Key: "barlow-condensed-extrabold", File: "barlow-condensed-800.ttf", Italic: "", Family: "Barlow Condensed ExtraBold", About: "narrow sans, extra bold", Bold: false, Em: 1.3490, Cap: 0.7000, Weight: 800, Kind: "condensed"},
	{Key: "bebas", File: "bebas-neue-400.ttf", Italic: "", Family: "Bebas Neue", About: "tall all-caps condensed titles (Bebas Neue)", Bold: false, Em: 1.3000, Cap: 0.7000, Weight: 400, Kind: "condensed"},
	{Key: "anton", File: "anton-400.ttf", Italic: "", Family: "Anton", About: "tall heavy condensed, for hooks", Bold: false, Em: 1.7334, Cap: 0.8594, Weight: 400, Kind: "condensed"},
	{Key: "archivo-black", File: "archivo-black-400.ttf", Italic: "", Family: "Archivo Black", About: "wide heavy sans", Bold: false, Em: 1.3470, Cap: 0.6880, Weight: 400, Kind: "display"},
	{Key: "russo", File: "russo-one-400.ttf", Italic: "", Family: "Russo One", About: "techy rounded-square sans", Bold: false, Em: 1.2050, Cap: 0.7000, Weight: 400, Kind: "display"},
	{Key: "rubik-mono", File: "rubik-mono-one-400.ttf", Italic: "", Family: "Rubik Mono One", About: "wide blocky caps", Bold: false, Em: 1.2380, Cap: 0.7000, Weight: 400, Kind: "display"},
	{Key: "righteous", File: "righteous-400.ttf", Italic: "", Family: "Righteous", About: "retro rounded display", Bold: false, Em: 1.2417, Cap: 0.7002, Weight: 400, Kind: "display"},
	{Key: "bungee", File: "bungee-400.ttf", Italic: "", Family: "Bungee", About: "urban signage caps", Bold: false, Em: 2.5740, Cap: 0.7200, Weight: 400, Kind: "display"},
	{Key: "passion", File: "passion-one-700.ttf", Italic: "", Family: "Passion One", About: "chunky playful display, bold", Bold: true, Em: 1.1010, Cap: 0.6210, Weight: 700, Kind: "display"},
	{Key: "lilita", File: "lilita-one-400.ttf", Italic: "", Family: "Lilita One", About: "friendly heavy rounded display", Bold: false, Em: 1.1430, Cap: 0.7040, Weight: 400, Kind: "display"},
	{Key: "titan", File: "titan-one-400.ttf", Italic: "", Family: "Titan One", About: "bubbly rounded display", Bold: false, Em: 1.1450, Cap: 0.7100, Weight: 400, Kind: "display"},
	{Key: "luckiest", File: "luckiest-guy-400.ttf", Italic: "", Family: "Luckiest Guy", About: "cartoon bubble caps (Luckiest Guy)", Bold: false, Em: 1.2256, Cap: 0.6953, Weight: 400, Kind: "display"},
	{Key: "bangers", File: "bangers-400.ttf", Italic: "", Family: "Bangers", About: "comic book caps", Bold: false, Em: 1.7570, Cap: 0.7210, Weight: 400, Kind: "display"},
	{Key: "black-ops", File: "black-ops-one-400.ttf", Italic: "", Family: "Black Ops One", About: "stencil military caps", Bold: false, Em: 1.4263, Cap: 0.6479, Weight: 400, Kind: "display"},
	{Key: "creepster", File: "creepster-400.ttf", Italic: "", Family: "Creepster", About: "dripping horror letters, for fire or spooky captions", Bold: false, Em: 1.1689, Cap: 0.7334, Weight: 400, Kind: "horror"},
	{Key: "nosifer", File: "nosifer-400.ttf", Italic: "", Family: "Nosifer", About: "blood-drip horror letters", Bold: false, Em: 1.7402, Cap: 0.4170, Weight: 400, Kind: "horror"},
	{Key: "alfa-slab", File: "alfa-slab-one-400.ttf", Italic: "", Family: "Alfa Slab One", About: "heavy slab serif", Bold: false, Em: 1.5650, Cap: 0.7780, Weight: 400, Kind: "serif"},
	{Key: "abril", File: "abril-fatface-400.ttf", Italic: "", Family: "Abril Fatface", About: "high-contrast fat serif headlines", Bold: false, Em: 1.3490, Cap: 0.7000, Weight: 400, Kind: "serif"},
	{Key: "dm-serif", File: "dm-serif-display-400.ttf", Italic: "dm-serif-display-400-italic.ttf", Family: "DM Serif Display", About: "elegant display serif; real italic", Bold: false, Em: 1.3710, Cap: 0.6600, Weight: 400, Kind: "serif"},
	{Key: "playfair", File: "playfair-display-700.ttf", Italic: "", Family: "Playfair Display", About: "elegant serif, bold", Bold: true, Em: 1.4100, Cap: 0.7080, Weight: 700, Kind: "serif"},
	{Key: "shrikhand", File: "shrikhand-400.ttf", Italic: "", Family: "Shrikhand", About: "bold italic retro script-display", Bold: false, Em: 1.4580, Cap: 0.6690, Weight: 400, Kind: "script"},
	{Key: "pacifico", File: "pacifico-400.ttf", Italic: "", Family: "Pacifico", About: "casual brush script", Bold: false, Em: 1.9350, Cap: 0.8400, Weight: 400, Kind: "script"},
	{Key: "lobster", File: "lobster-400.ttf", Italic: "", Family: "Lobster", About: "bold retro script", Bold: false, Em: 1.4900, Cap: 0.7480, Weight: 400, Kind: "script"},
	{Key: "dancing-script", File: "dancing-script-700.ttf", Italic: "", Family: "Dancing Script", About: "flowing script, bold", Bold: true, Em: 1.4020, Cap: 0.7200, Weight: 700, Kind: "script"},
	{Key: "great-vibes", File: "great-vibes-400.ttf", Italic: "", Family: "Great Vibes", About: "formal wedding script", Bold: false, Em: 1.7500, Cap: 0.7500, Weight: 400, Kind: "script"},
	{Key: "satisfy", File: "satisfy-400.ttf", Italic: "", Family: "Satisfy", About: "casual signature script", Bold: false, Em: 1.4238, Cap: 0.7490, Weight: 400, Kind: "script"},
	{Key: "permanent-marker", File: "permanent-marker-400.ttf", Italic: "", Family: "Permanent Marker", About: "handwritten marker", Bold: false, Em: 1.4268, Cap: 0.7402, Weight: 400, Kind: "hand"},
	{Key: "caveat", File: "caveat-400.ttf", Italic: "", Family: "Caveat", About: "quick handwriting", Bold: false, Em: 1.2890, Cap: 0.6100, Weight: 400, Kind: "hand"},
	{Key: "comic-neue", File: "comic-neue-700.ttf", Italic: "", Family: "Comic Neue", About: "friendly comic handwriting, bold", Bold: true, Em: 1.3500, Cap: 0.6700, Weight: 700, Kind: "hand"},
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
	Box        string // "card": one box behind the whole block
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
	{Key: "news-card", About: "news card: dark semibold italic on one white card (add emphasis for red words)", Font: "poppins-semibold", Size: 54, Color: "#111111", Background: "#FFFFFF", BgOpacity: 100, Italic: true, Box: "card"},
	{Key: "news-label", About: "small caps label over a news card: white italic on a red card", Font: "poppins-semibold", Size: 36, Color: "#FFFFFF", Background: "#E8003D", BgOpacity: 100, Italic: true, Uppercase: true, Box: "card"},
	{Key: "bebas", About: "tall condensed caps title with a soft shadow", Font: "bebas", Size: 130, Color: "#FFFFFF", Shadow: true, Uppercase: true, Spacing: 2},
	{Key: "bubble", About: "cartoon yellow caps with a thick black outline", Font: "luckiest", Size: 96, Color: "#FFD60A", Stroke: "#000000", Uppercase: true},
	{Key: "fire", About: "dripping orange caps with a dark red outline, fire or horror", Font: "creepster", Size: 110, Color: "#FF9F1C", Stroke: "#6A0A00", Uppercase: true},
	{Key: "kanit", About: "heavy squarish caps, white with black outline (SEA TikTok)", Font: "kanit-black", Size: 88, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true},
	{Key: "rounded", About: "friendly rounded white with a soft shadow", Font: "lilita", Size: 90, Color: "#FFFFFF", Shadow: true},
	{Key: "elegant", About: "high-contrast serif headline, white with shadow", Font: "abril", Size: 84, Color: "#FFFFFF", Shadow: true},
	{Key: "script", About: "formal script, white with a soft shadow", Font: "great-vibes", Size: 110, Color: "#FFFFFF", Shadow: true},
	{Key: "signature", About: "casual signature script", Font: "satisfy", Size: 96, Color: "#FFFFFF", Shadow: true},
	{Key: "slab", About: "heavy slab serif on a yellow card", Font: "alfa-slab", Size: 70, Color: "#111111", Background: "#FFE14D", BgOpacity: 100, Box: "card"},
	{Key: "retro", About: "retro rounded neon pink with a deep outline", Font: "righteous", Size: 84, Color: "#FF4FD8", Stroke: "#2A0033"},
	{Key: "stencil", About: "military stencil caps, olive on a black box", Font: "black-ops", Size: 76, Color: "#C9D17A", Background: "#000000", BgOpacity: 80},
	{Key: "poster", About: "wide blocky caps, black on a white card", Font: "rubik-mono", Size: 64, Color: "#111111", Background: "#FFFFFF", BgOpacity: 100, Uppercase: true, Box: "card"},
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
	{TextStyle{Key: "fire", About: "one dripping orange word at a time, dark red outline", Font: "creepster", Size: 96, Color: "#FF9F1C", Stroke: "#6A0A00", Uppercase: true}, "pop", "", 1, 0.62},
	{TextStyle{Key: "bubble", About: "one cartoon yellow word at a time, black outline", Font: "luckiest", Size: 84, Color: "#FFD60A", Stroke: "#000000", Uppercase: true}, "pop", "", 1, 0.62},
	{TextStyle{Key: "kanit", About: "heavy caps, the spoken word lights up yellow", Font: "kanit-black", Size: 70, Color: "#FFFFFF", Stroke: "#000000", Uppercase: true}, "karaoke", "#FFD60A", 3, 0.7},
	{TextStyle{Key: "bebas", About: "tall condensed caps, three words at a time", Font: "bebas", Size: 100, Color: "#FFFFFF", Shadow: true, Uppercase: true, Spacing: 1}, "chunk", "", 3, 0.7},
	{TextStyle{Key: "news", About: "white semibold on a dark box, like TV subtitles", Font: "poppins-semibold", Size: 52, Color: "#FFFFFF", Background: "#000000", BgOpacity: 75}, "chunk", "", 5, 0.8},
	{TextStyle{Key: "rounded", About: "friendly rounded two-word pops", Font: "lilita", Size: 80, Color: "#FFFFFF", Stroke: "#000000"}, "pop", "", 2, 0.65},
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
	MaxDuration  = 600.0 // seconds of output
	MaxClips     = 100   // each clip is an FFmpeg input; segment rendering (later) lifts this
	MaxTexts     = 500
	MaxOverlays  = 50
	MaxGradients = 8
	MaxTracks    = 30
	MaxFilters   = 100
	MaxWords     = 20000
)
