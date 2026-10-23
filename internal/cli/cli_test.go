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
	if !strings.Contains(d["graph"].(string), "[vout]") {
		t.Fatalf("dry-run graph: %v", d)
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
