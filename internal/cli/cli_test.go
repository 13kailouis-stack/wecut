package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests render real video, so they need FFmpeg on PATH. They make
// their own tiny media with lavfi and run in a temp workspace.

type result struct {
	OK       bool              `json:"ok"`
	Errors   []map[string]any  `json:"errors"`
	Warnings []map[string]any  `json:"warnings"`
	File     string            `json:"file"`
	Duration float64           `json:"duration"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	HasAudio bool              `json:"has_audio"`
	Frames   []map[string]any  `json:"frames"`
	Files    []json.RawMessage `json:"files"`
}

func run(t *testing.T, root string, stdin string, args ...string) (result, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), append(args, "--root", root), strings.NewReader(stdin), &out, &errOut)
	var r result
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("%v: output is not JSON: %q", args, out.String())
	}
	return r, code
}

func ffmpeg(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
	}
}

func testWorkspace(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	root := t.TempDir()
	m := filepath.Join(root, "media")
	os.MkdirAll(m, 0o755)
	ffmpeg(t, m, "-f", "lavfi", "-i", "testsrc=s=640x360:r=25:d=6", "-f", "lavfi", "-i", "sine=f=300:d=6", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", "talk.mp4")
	ffmpeg(t, m, "-f", "lavfi", "-i", "testsrc2=s=360x640:r=30:d=4", "-c:v", "libx264", "-preset", "ultrafast", "-an", "tall.mp4")
	ffmpeg(t, m, "-f", "lavfi", "-i", "sine=f=500:d=3", "-c:a", "libmp3lame", "beep.mp3")
	ffmpeg(t, m, "-f", "lavfi", "-i", "testsrc=s=800x600:d=1", "-frames:v", "1", "-update", "1", "photo.jpg")
	ffmpeg(t, m, "-f", "lavfi", "-i", "color=c=red:s=100x50", "-frames:v", "1", "-update", "1", "logo.png")
	os.WriteFile(filepath.Join(m, "talk.words.json"), []byte(`[{"word":"hello","start":1.0,"end":1.4},{"word":"agent","start":1.5,"end":2.0},{"word":"world.","start":2.1,"end":2.6},{"word":"later","start":4.5,"end":5}]`), 0o644)
	os.WriteFile(filepath.Join(m, "playlist.mp4"), []byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\ntalk.mp4\n#EXT-X-ENDLIST\n"), 0o644)
	return root
}

func TestRenderEndToEnd(t *testing.T) {
	root := testWorkspace(t)
	project := `{
	  "wecut": "1",
	  "output": {"file": "out/e2e.mp4", "res": 360, "quality": "draft"},
	  "clips": [
	    {"src": "media/talk.mp4", "from": 0.5, "to": 3.5, "focus": {"x": 0.3, "y": 0.5}, "transition": {"type": "slideleft", "duration": 0.4}},
	    {"src": "media/tall.mp4", "speed": 2, "fit": "contain"},
	    {"src": "media/photo.jpg", "duration": 1.5, "motion": "in", "fadeOut": 0.3}
	  ],
	  "texts": [{"text": "Hook {not a tag}", "end": 1.5, "style": "hook", "in": {"type": "pop"}, "out": {"type": "fade"}}],
	  "captions": {"words": "media/talk.words.json", "src": "media/talk.mp4", "style": "karaoke"},
	  "overlays": [{"src": "media/logo.png", "x": 0.8, "y": 0.1, "width": 0.2, "opacity": 80, "rotate": 10}],
	  "audio": [{"src": "media/beep.mp3", "at": 1, "volume": 50, "duck": true, "loop": true}],
	  "filters": [{"name": "warm", "amount": 50}],
	  "effects": [{"name": "flash", "start": 2.5}, {"name": "cinema", "start": 4}]
	}`
	os.WriteFile(filepath.Join(root, "p.json"), []byte(project), 0o644)

	v, code := run(t, root, "", "validate", "p.json")
	want := 3 - 0.4 + 2 + 1.5 // talk 3 s, tall 4 s at 2x, photo 1.5 s, minus the transition
	if code != 0 || !v.OK || v.Duration < want-0.01 || v.Duration > want+0.01 {
		t.Fatalf("validate: code %d %+v", code, v)
	}

	r, code := run(t, root, "", "render", "p.json")
	if code != 0 || !r.OK {
		t.Fatalf("render: code %d %+v", code, r)
	}
	if r.File != "out/e2e.mp4" || r.Width != 360 || r.Height != 640 || !r.HasAudio {
		t.Fatalf("render result %+v", r)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "out")); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	pv, code := run(t, root, "", "preview", "p.json", "--at", "0.2,2,5")
	if code != 0 || len(pv.Frames) != 3 {
		t.Fatalf("preview: code %d %+v", code, pv)
	}
	for _, f := range pv.Frames {
		if _, err := os.Stat(filepath.Join(root, f["file"].(string))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderGIFFromStdin(t *testing.T) {
	root := testWorkspace(t)
	r, code := run(t, root, `{"wecut":"1","output":{"file":"out/a.gif","ratio":"1:1","res":240},"clips":[{"src":"media/talk.mp4","to":2}],"texts":[{"text":"gif"}]}`, "render", "-")
	if code != 0 || r.Width != 240 || r.HasAudio {
		t.Fatalf("gif: code %d %+v", code, r)
	}
}

// A concat between transitions loses constant frame-rate metadata in FFmpeg.
// Exercise both the audio render and the video-only preview paths.
func TestRenderMixedCutsAndTransitions(t *testing.T) {
	root := testWorkspace(t)
	project := `{"wecut":"1","output":{"file":"out/mixed.mp4","res":240,"fps":30},"clips":[
	{"src":"media/talk.mp4","to":1,"transition":{"type":"fade","duration":0.2}},
	{"src":"media/tall.mp4","to":1},
	{"src":"media/photo.jpg","duration":1,"transition":{"type":"flash","duration":0.2}},
	{"src":"media/talk.mp4","from":2,"to":3}]}`
	os.WriteFile(filepath.Join(root, "mixed.json"), []byte(project), 0o644)
	r, code := run(t, root, "", "render", "mixed.json")
	if code != 0 || !r.OK || !r.HasAudio || r.Duration < 3.5 || r.Duration > 3.7 {
		t.Fatalf("mixed render: code %d %+v", code, r)
	}
	pv, code := run(t, root, "", "preview", "mixed.json", "--at", "0.5,2.7,3.3")
	if code != 0 || len(pv.Frames) != 3 {
		t.Fatalf("mixed preview: code %d %+v", code, pv)
	}
}

func TestRefusals(t *testing.T) {
	root := testWorkspace(t)
	cases := []struct {
		name, project, code string
		exit                int
	}{
		{"outside", `{"wecut":"1","clips":[{"src":"../x.mp4"}]}`, "E_PATH", 2},
		{"playlist in disguise", `{"wecut":"1","clips":[{"src":"media/playlist.mp4"}]}`, "E_MEDIA", 2},
		{"output outside", `{"wecut":"1","output":{"file":"../x.mp4"},"clips":[{"src":"media/talk.mp4"}]}`, "E_PATH", 2},
		{"overwrite input", `{"wecut":"1","output":{"file":"media/talk.mp4"},"clips":[{"src":"media/talk.mp4"}]}`, "E_PATH", 2},
		{"past the end", `{"wecut":"1","clips":[{"src":"media/talk.mp4","to":60}]}`, "E_RANGE", 2},
		{"typo", `{"wecut":"1","clips":[{"source":"media/talk.mp4"}]}`, "E_SCHEMA", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, code := run(t, root, c.project, "render", "-")
			if code != c.exit || r.OK || len(r.Errors) == 0 || r.Errors[0]["code"] != c.code {
				t.Fatalf("code %d %+v", code, r)
			}
		})
	}
}

func TestDryRunAndCommands(t *testing.T) {
	root := testWorkspace(t)
	var out bytes.Buffer
	if code := Run(context.Background(), []string{"render", "-", "--dry-run", "--root", root}, strings.NewReader(`{"wecut":"1","clips":[{"src":"media/talk.mp4"}]}`), &out, &out); code != 0 {
		t.Fatalf("dry-run: %s", out.String())
	}
	var d map[string]any
	json.Unmarshal(out.Bytes(), &d)
	if g := d["graph"].(string); !strings.Contains(g, "[vout]") || !strings.Contains(g, "loudnorm=I=-14") {
		t.Fatalf("dry-run graph: %v", d)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"render", "-", "--dry-run", "--no-loudnorm", "--root", root}, strings.NewReader(`{"wecut":"1","clips":[{"src":"media/talk.mp4"}]}`), &out, &out); code != 0 || strings.Contains(out.String(), "loudnorm=") {
		t.Fatalf("no-loudnorm: %s", out.String())
	}
	if r, code := run(t, root, "", "transcribe", "media/talk.mp4"); code != 3 || r.OK || len(r.Errors) == 0 || r.Errors[0]["code"] != "E_DEPENDENCY" {
		t.Fatalf("transcribe without a model: code %d %+v", code, r)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "wecut.mp4")); err == nil {
		t.Fatal("dry-run rendered a file")
	}
	for _, args := range [][]string{{"version"}, {"list"}, {"list", "fonts"}, {"example"}, {"doctor"}, {"probe", "media/talk.mp4"}} {
		r, code := run(t, root, "", args...)
		if code != 0 || !r.OK {
			t.Errorf("%v: code %d %+v", args, code, r)
		}
	}
	if _, code := run(t, root, "", "list", "nope"); code != 2 {
		t.Error("unknown list should exit 2")
	}
	if _, code := run(t, root, "", "explode"); code != 2 {
		t.Error("unknown command should exit 2")
	}
}

func TestExamplesPrint(t *testing.T) {
	for _, name := range []string{"podcast-clip", "slideshow", "product-promo"} {
		var out bytes.Buffer
		if code := Run(context.Background(), []string{"example", name}, nil, &out, &out); code != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("%s: %s", name, out.String())
		}
	}
}

// A video with hard cuts every 1.2 s and a beat, analysed, then a recipe
// applied to it, saved back as the user's own recipe and applied again.
func TestAnalyzeAndRecipes(t *testing.T) {
	root := testWorkspace(t)
	m := filepath.Join(root, "media")
	ffmpeg(t, m, "-f", "lavfi", "-i", "testsrc2=s=360x640:r=30:d=1.2", "-f", "lavfi", "-i", "smptebars=s=360x640:r=30:d=1.2",
		"-f", "lavfi", "-i", "testsrc2=s=360x640:r=30:d=1.2,negate", "-f", "lavfi", "-i", "aevalsrc='0.9*sin(2*PI*55*t)*exp(-25*mod(t,0.5))':s=22050:d=3.6",
		"-filter_complex", "[0:v][1:v][2:v]concat=n=3:v=1:a=0[v]", "-map", "[v]", "-map", "3:a", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", "cuts.mp4")

	var an map[string]any
	out := runRaw(t, root, "", "analyze", "media/cuts.mp4", "--frames", "4")
	json.Unmarshal(out, &an)
	if an["ok"] != true {
		t.Fatalf("analyze: %s", out)
	}
	if shots := an["shots"].([]any); len(shots) != 3 {
		t.Fatalf("shots %v", shots)
	}
	if _, err := os.Stat(filepath.Join(root, an["sheet"].(string))); err != nil {
		t.Fatal("no sheet:", err)
	}

	req := `{"recipe": "news-headline", "clips": [{"src": "media/cuts.mp4", "role": "hook"}, {"src": "media/talk.mp4", "from": 0.5, "to": 5}],
	  "texts": {"headline": {"text": "Big news today", "emphasis": ["news"]}, "label": {"text": "Page"}},
	  "transcript": "media/talk.words.json", "music": "media/beep.mp3", "write": "projects/one.json"}`
	var ap map[string]any
	out = runRaw(t, root, req, "recipe", "apply", "--input", "-")
	json.Unmarshal(out, &ap)
	if ap["ok"] != true || ap["file"] != "projects/one.json" {
		t.Fatalf("apply: %s", out)
	}
	r, code := run(t, root, "", "render", "projects/one.json")
	if code != 0 || r.File != "out/one.mp4" || !r.HasAudio {
		t.Fatalf("render applied project: code %d %+v", code, r)
	}

	var fp map[string]any
	out = runRaw(t, root, "", "recipe", "from-project", "projects/one.json", "--name", "mine", "--write", "recipes/mine.json")
	json.Unmarshal(out, &fp)
	if fp["ok"] != true {
		t.Fatalf("from-project: %s", out)
	}
	var again map[string]any
	out = runRaw(t, root, `{"recipe": "mine", "clips": [{"src": "media/talk.mp4", "from": 1}]}`, "recipe", "apply", "--input", "-")
	json.Unmarshal(out, &again)
	if again["ok"] != true {
		t.Fatalf("apply own recipe: %s", out)
	}
	if audio := again["project"].(map[string]any)["audio"].([]any); audio[0].(map[string]any)["src"] != "media/beep.mp3" {
		t.Fatalf("own recipe lost its music: %v", audio)
	}
	var list map[string]any
	json.Unmarshal(runRaw(t, root, "", "recipe", "list"), &list)
	found := false
	for _, it := range list["recipes"].([]any) {
		found = found || it.(map[string]any)["name"] == "mine"
	}
	if !found {
		t.Fatal("recipe list does not show the workspace recipe")
	}
}

func runRaw(t *testing.T, root, stdin string, args ...string) []byte {
	t.Helper()
	var out, errOut bytes.Buffer
	Run(context.Background(), append(args, "--root", root), strings.NewReader(stdin), &out, &errOut)
	return out.Bytes()
}
