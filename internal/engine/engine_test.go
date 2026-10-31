package engine

import (
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseTranscriptFormats(t *testing.T) {
	cases := map[string]struct {
		data, ext string
		first     Word
		n         int
	}{
		"word list":           {`[{"word":" Hi","start":0.5,"end":0.8},{"text":"there.","start":0.9,"end":1.2}]`, ".json", Word{"Hi", 0.5, 0.8}, 2},
		"verbose_json words":  {`{"text":"hi","words":[{"word":"hi","start":1,"end":1.5}]}`, ".json", Word{"hi", 1, 1.5}, 1},
		"segments with words": {`{"segments":[{"start":0,"end":2,"text":"a b","words":[{"word":"a","start":0,"end":1},{"word":"b","start":1,"end":2}]}]}`, ".json", Word{"a", 0, 1}, 2},
		"segments only":       {`{"segments":[{"start":0,"end":3,"text":"ab cd"}]}`, ".json", Word{"ab", 0, 1.5}, 2},
		"whisper.cpp":         {`{"transcription":[{"offsets":{"from":1000,"to":3000},"text":" one two"}]}`, ".json", Word{"one", 1, 2}, 2},
		"srt":                 {"1\r\n00:00:01,000 --> 00:00:03,000\r\n<i>one</i> two\r\n\r\n2\r\n00:00:04,000 --> 00:00:05,000\r\nthree\r\n", ".srt", Word{"one", 1, 2}, 3},
		"vtt":                 {"WEBVTT\n\n00:01.000 --> 00:03.000\none two\n", ".vtt", Word{"one", 1, 2}, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w, err := ParseTranscript([]byte(c.data), c.ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(w) != c.n || w[0].Text != c.first.Text || !near(w[0].Start, c.first.Start) || !near(w[0].End, c.first.End) {
				t.Fatalf("got %+v", w)
			}
		})
	}
	for _, bad := range []string{`{"foo":1}`, `[{"word":"a"}]`, `not json`, `[]`} {
		if _, err := ParseTranscript([]byte(bad), ".json"); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}
}

// words parses "text@start-end ..." into a word list.
func words(s string) []Word {
	var out []Word
	for _, f := range strings.Fields(s) {
		at := strings.LastIndex(f, "@")
		a, b, _ := strings.Cut(f[at+1:], "-")
		start, _ := strconv.ParseFloat(a, 64)
		end, _ := strconv.ParseFloat(b, 64)
		out = append(out, Word{f[:at], start, end})
	}
	return out
}

func TestChunks(t *testing.T) {
	ws := words("one@0-0.3 two@0.3-0.6 three@0.6-0.9 four.@0.9-1.2 five@2-2.3 six@2.3-2.6 seven@2.6-2.9")
	got := Chunks(ws, 10, 5, 2.6)
	var texts []string
	for _, c := range got {
		texts = append(texts, c.Text())
	}
	// "four." ends a sentence; the pause before "five" would split anyway.
	if want := []string{"one two three four.", "five six seven"}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("got %q", texts)
	}
	if got := Chunks(ws, 10, 2, 2.6); len(got) != 4 {
		t.Fatalf("maxWords 2: %d chunks", len(got))
	}
	// Chunks stay inside the video and never overlap.
	short := Chunks(words("a@0-0.1 b@0.1-0.2 c.@5-6"), 5.5, 5, 2.6)
	if len(short) != 2 || !near(short[0].End, 0.4) || !near(short[1].End, 5.5) {
		t.Fatalf("tidy: %+v", short)
	}
}

func TestMapWordsThroughClips(t *testing.T) {
	clips := []ClipT{
		{Src: "talk.mp4", Kind: "video", From: 10, To: 20, Speed: 1, Offset: 0},
		{Src: "other.mp4", Kind: "video", From: 0, To: 5, Speed: 1, Offset: 10},
		{Src: "talk.mp4", Kind: "video", From: 30, To: 34, Speed: 2, Offset: 15},
	}
	got := MapWords(words("before@5-6 in@12-12.5 edge@19.8-20.4 fast@31-32 after@40-41"), clips, "talk.mp4")
	want := []Word{{"in", 2, 2.5}, {"fast", 15.5, 16}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}

func project(t *testing.T, js string) *spec.Project {
	t.Helper()
	p, issues := spec.Decode([]byte(js))
	if len(issues) == 0 {
		issues = spec.Validate(p)
	}
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	return p
}

var fakeMedia = media.Set{
	"media/podcast.mp4": {Path: "/w/media/podcast.mp4", Kind: "video", Duration: 840, Width: 1280, Height: 720, HasAudio: true, FPS: 30},
	"media/a.mp4":       {Path: "/w/media/a.mp4", Kind: "video", Duration: 8, Width: 1280, Height: 720, HasAudio: true, FPS: 25},
	"media/b.mp4":       {Path: "/w/media/b.mp4", Kind: "video", Duration: 6, Width: 1080, Height: 1920, FPS: 30},
	"media/photo1.jpg":  {Path: "/w/media/photo1.jpg", Kind: "image", Width: 1600, Height: 1200},
	"media/photo2.jpg":  {Path: "/w/media/photo2.jpg", Kind: "image", Width: 1600, Height: 1200},
	"media/photo3.jpg":  {Path: "/w/media/photo3.jpg", Kind: "image", Width: 1600, Height: 1200},
	"media/logo.png":    {Path: "/w/media/logo.png", Kind: "image", Width: 400, Height: 200},
	"media/music.mp3":   {Path: "/w/media/music.mp3", Kind: "audio", Duration: 20, HasAudio: true},
	"media/voice.mp4":   {Path: "/w/media/voice.mp4", Kind: "audio", Duration: 30, HasAudio: true},
	"media/moving.png":  {Path: "/w/media/moving.png", Kind: "video", Duration: 2, Width: 100, Height: 100}, // an APNG
}

func TestResolveTimeline(t *testing.T) {
	p := project(t, `{"wecut":"1","clips":[
		{"src":"media/a.mp4","from":2,"to":6,"transition":{"type":"flash","duration":0.3}},
		{"src":"media/b.mp4","to":5,"speed":1.5},
		{"src":"media/photo1.jpg","duration":2}]}`)
	e, issues := Resolve(p, fakeMedia, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if e.W != 1080 || e.H != 1920 || e.FPS != 30 {
		t.Fatalf("canvas %dx%d@%d", e.W, e.H, e.FPS)
	}
	offs := []float64{e.Clips[0].Offset, e.Clips[1].Offset, e.Clips[2].Offset}
	if !near(offs[1], 3.7) || !near(e.Clips[1].Dur, 5/1.5) || !near(e.Total, round3(3.7+5/1.5+2)) {
		t.Fatalf("offsets %v total %v", offs, e.Total)
	}
	if e.Clips[0].Trans != "fadewhite" {
		t.Fatalf("alias not resolved: %q", e.Clips[0].Trans)
	}
}

func TestResolveRejectsMediaProblems(t *testing.T) {
	cases := map[string]string{
		`{"wecut":"1","clips":[{"src":"media/a.mp4","to":30}]}`:                                                                "E_RANGE /clips/0/to",
		`{"wecut":"1","clips":[{"src":"media/a.mp4","from":9}]}`:                                                               "E_RANGE /clips/0/from",
		`{"wecut":"1","clips":[{"src":"media/voice.mp4"}]}`:                                                                    "E_MEDIA /clips/0/src",
		`{"wecut":"1","clips":[{"src":"media/a.mp4"}],"audio":[{"src":"media/b.mp4"}]}`:                                        "E_MEDIA /audio/0/src",
		`{"wecut":"1","clips":[{"src":"media/a.mp4"}],"overlays":[{"src":"media/moving.png"}]}`:                                "E_MEDIA /overlays/0/src",
		`{"wecut":"1","clips":[{"src":"media/a.mp4","to":1,"transition":{"type":"fade","duration":2}},{"src":"media/b.mp4"}]}`: "E_VALUE /clips/0/transition/duration",
		`{"wecut":"1","clips":[{"src":"media/a.mp4","to":1,"fadeIn":1,"fadeOut":1}]}`:                                          "E_VALUE /clips/0/fadeIn",
	}
	for js, want := range cases {
		_, issues := Resolve(project(t, js), fakeMedia, nil)
		if len(issues) == 0 || issues[0].Code+" "+issues[0].Path != want {
			t.Errorf("%s: got %v, want %s", js, issues, want)
		}
	}
}

func TestEscapeASS(t *testing.T) {
	got := escapeASS("{\\pos(0,0)}a\\Nb\nc}")
	want := "\\{\\⁠pos(0,0)\\}a\\⁠Nb\\Nc\\}"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestColours(t *testing.T) {
	if got := assColor("#FFE14D", 100); got != "&H004DE1FF" {
		t.Errorf("assColor %s", got)
	}
	if got := assColor("#000000", 70); got != "&H4C000000" {
		t.Errorf("assColor alpha %s", got)
	}
	// White at full opacity is limited-range white.
	if got := tint("#FFFFFF", 1, "1"); got != "lutyuv=y='val*0+235':u='val*0+128':v='val*0+128':enable='1'" {
		t.Errorf("tint %s", got)
	}
}

func TestLoudnormDefault(t *testing.T) {
	e, issues := Resolve(project(t, `{"wecut":"1","clips":[{"src":"media/a.mp4","to":1}]}`), fakeMedia, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	plan, err := Compile(e, Options{Encoder: "libx264", OutPath: "/w/out/tmp.mp4", FFmpegMajor: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Graph, "loudnorm=I=-14:TP=-1.5:LRA=11") {
		t.Fatalf("graph missing loudnorm:\n%s", plan.Graph)
	}
	off, err := Compile(e, Options{Encoder: "libx264", OutPath: "/w/out/tmp.mp4", FFmpegMajor: 8, NoLoudnorm: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off.Graph, "loudnorm") {
		t.Fatalf("no-loudnorm still normalised:\n%s", off.Graph)
	}
}

func TestATempoChains(t *testing.T) {
	cases := map[float64][]string{1: nil, 1.5: {"atempo=1.5"}, 4: {"atempo=2", "atempo=2"}, 0.25: {"atempo=0.5", "atempo=0.5"}}
	for s, want := range cases {
		if got := atempo(s); !reflect.DeepEqual(got, want) {
			t.Errorf("%v: %v", s, got)
		}
	}
}

func TestCanvas(t *testing.T) {
	cases := []struct {
		r         float64
		res, w, h int
	}{{9.0 / 16, 1080, 1080, 1920}, {16.0 / 9, 720, 1280, 720}, {1, 1080, 1080, 1080}, {9.0 / 16, 2160, 2160, 3840}, {2, 2160, 3840, 1920}}
	for _, c := range cases {
		if w, h := canvas(c.r, c.res); w != c.w || h != c.h {
			t.Errorf("%v@%d: %dx%d", c.r, c.res, w, h)
		}
	}
}

// Golden plans for the bundled examples: any change to the FFmpeg graph or
// subtitle output shows up as a diff here. Run `go test ./internal/engine
// -update` after an intended change, and render the examples to check them.
func TestGoldenPlans(t *testing.T) {
	transcript, err := os.ReadFile(filepath.Join("testdata", "podcast.words.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"podcast-clip", "slideshow", "product-promo"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			p := project(t, string(raw))
			var ws []Word
			if p.Captions != nil {
				if ws, err = ParseTranscript(transcript, ".json"); err != nil {
					t.Fatal(err)
				}
			}
			e, issues := Resolve(p, fakeMedia, ws)
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			plan, err := Compile(e, Options{Encoder: "libx264", OutPath: "/w/out/tmp", FFmpegMajor: 8})
			if err != nil {
				t.Fatal(err)
			}
			got := "# args\n" + strings.Join(plan.Args, "\n") + "\n\n# graph\n" + plan.Graph + "\n# ass\n" + plan.ASS
			path := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if string(want) != got {
				t.Fatalf("plan changed; run `go test ./internal/engine -update` if intended.\n--- got ---\n%s", got)
			}
		})
	}
}

func TestPreviewPlanSelectsFrames(t *testing.T) {
	p := project(t, `{"wecut":"1","clips":[{"src":"media/a.mp4"}],"texts":[{"text":"hi"}]}`)
	e, _ := Resolve(p, fakeMedia, nil)
	plan, err := Compile(e, Options{Frames: []float64{3, 0.5, 3.01, 99}, ResOverride: 540, FramePath: "/w/out/p-%02d.jpg", FFmpegMajor: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Graph, "select='eq(n,15)+eq(n,90)+eq(n,238)'") {
		t.Fatalf("graph: %s", plan.Graph)
	}
	if plan.W != 540 || plan.H != 960 || plan.Audio {
		t.Fatalf("preview plan %dx%d audio=%v", plan.W, plan.H, plan.Audio)
	}
	if got := FrameTimes([]float64{3, 0.5, 3.01, 99}, e.Total, e.FPS); !reflect.DeepEqual(got, []float64{0.5, 3, 7.933}) {
		t.Fatalf("frame times %v", got)
	}
}

func TestEmphasize(t *testing.T) {
	got := emphasize("Banyak MULAI speak up! {x}", []string{"mulai speak up", "x"}, "#FF0000", "#FFFFFF")
	want := `Banyak {\1c&H0000FF&}MULAI speak up{\1c&HFFFFFF&}! \{{\1c&H0000FF&}x{\1c&HFFFFFF&}\}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
