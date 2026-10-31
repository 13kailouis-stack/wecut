package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/recipe"
)

// TestCloneRoundTrip renders a video whose edit is known (a hard cut at
// 2 s, a flash into the third shot, a headline card over the first two
// shots, a logo mark the whole time), clones it, and checks the template found
// that edit; then fills the template with other footage and renders it.
func TestCloneRoundTrip(t *testing.T) {
	root := testWorkspace(t)
	m := filepath.Join(root, "media")
	ffmpeg(t, m, "-f", "lavfi", "-i", "smptebars=s=360x640:r=30:d=3", "-c:v", "libx264", "-preset", "ultrafast", "bars.mp4")
	ffmpeg(t, m, "-f", "lavfi", "-i", "mandelbrot=s=360x640:r=30", "-t", "3", "-c:v", "libx264", "-preset", "ultrafast", "fractal.mp4")
	project := `{"wecut": "1", "output": {"file": "out/known.mp4", "res": 540},
	  "clips": [
	    {"src": "media/tall.mp4", "from": 0, "to": 2},
	    {"src": "media/fractal.mp4", "from": 0, "to": 2.4, "transition": {"type": "flash", "duration": 0.4}},
	    {"src": "media/bars.mp4", "from": 0, "to": 2}
	  ],
	  "texts": [{"text": "Breaking news today\nsecond line of the card", "x": 0.08, "y": 0.7, "align": "left", "end": 3.6,
	    "font": "poppins", "size": 52, "color": "#111111", "background": "#FFFFFF", "box": "card", "stroke": "none", "lineHeight": 1.25},
	    {"text": "WeCut News", "x": 0.25, "y": 0.08, "size": 44, "style": "outline"}]}`
	os.WriteFile(filepath.Join(root, "known.json"), []byte(project), 0o644)
	if r, code := run(t, root, "", "render", "known.json"); code != 0 {
		t.Fatalf("render: %+v", r)
	}

	var out bytes.Buffer
	code := Run(context.Background(), []string{"clone", "out/known.mp4", "--name", "known", "--ocr", "off", "--root", root}, strings.NewReader(""), &out, &out)
	if code != 0 {
		t.Fatalf("clone: %s", out.String())
	}
	var res struct {
		Status  string `json:"status"`
		Preview string `json:"template_preview_project"`
		File    string `json:"file"`
		Events  []struct {
			At         float64
			Kind       string
			Start, End float64
		} `json:"events"`
		Slots   []json.RawMessage `json:"slots"`
		Compare string            `json:"compare"`
		Timing  struct {
			Events, Matched int
			MaxMs           float64 `json:"max_error_ms"`
		} `json:"timing"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	// A cut at 2 s, a flash over 4.0..4.4 s (the second clip's last 0.4 s).
	var cut, flash bool
	for _, e := range res.Events {
		cut = cut || (e.Kind == "cut" && math.Abs(e.At-2) < 0.07)
		flash = flash || (e.Kind == "flash" && e.Start > 3.8 && e.End < 4.6)
	}
	if !cut || !flash || len(res.Slots) != 3 {
		t.Fatalf("events %+v, %d slots", res.Events, len(res.Slots))
	}
	if res.Compare == "" {
		t.Error("no comparison sheet")
	}
	if res.Status != "needs_visual_review" || res.Preview == "" {
		t.Errorf("clone must expose its review state and stand-in preview: %+v", res)
	}
	// The template, rendered and measured again, cuts where the original did.
	if res.Timing.Events == 0 || res.Timing.Matched != res.Timing.Events || res.Timing.MaxMs > 200 {
		t.Errorf("timing check %+v", res.Timing)
	}

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res.File)))
	if err != nil {
		t.Fatal(err)
	}
	var r recipe.Recipe
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	var card *recipe.Slot
	for i := range r.Texts {
		if r.Texts[i].Box == "card" {
			card = &r.Texts[i]
		}
	}
	if card == nil || card.End == nil || math.Abs(*card.End-3.6) > 0.25 || card.LineHeight == 0 || card.Align != "left" {
		t.Fatalf("headline card not found: %s", raw)
	}
	if math.Abs(*card.X-0.08) > 0.03 || math.Abs(*card.Y-0.7) > 0.03 {
		t.Errorf("card at %.3f, %.3f", *card.X, *card.Y)
	}
	if len(r.Overlays) != 1 || math.Abs(*r.Overlays[0].Y-0.08) > 0.04 {
		t.Errorf("logo slot %+v", r.Overlays)
	}

	// A model looks at the review and answers; the analysis is not redone.
	var first struct {
		Review struct {
			Pending int `json:"pending"`
			Items   []struct {
				ID    string          `json:"id"`
				Kind  string          `json:"kind"`
				Image string          `json:"image"`
				Now   json.RawMessage `json:"now"`
			} `json:"items"`
			Limits []string `json:"limits"`
		} `json:"review"`
	}
	if err := json.Unmarshal(out.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Review.Pending == 0 || len(first.Review.Limits) == 0 {
		t.Fatalf("review: %+v", first.Review)
	}
	cardID, kinds := "", map[string]bool{}
	for _, it := range first.Review.Items {
		kinds[it.Kind] = true
		var now struct{ Slot string }
		json.Unmarshal(it.Now, &now)
		if it.Kind == "text" && now.Slot == card.Slot {
			cardID = it.ID
			if it.Image == "" || !fileExists(filepath.Join(root, filepath.FromSlash(it.Image))) {
				t.Errorf("no image for %s: %+v", it.ID, it)
			}
		}
	}
	if cardID == "" || !kinds["shots"] || !kinds["transitions"] {
		t.Fatalf("review items: %+v", first.Review.Items)
	}
	answers := `{"` + cardID + `": {"lines": ["Words read by eye", "on the same card"], "font": "abril", "emphasis": ["eye"], "emphasis_color": "#E8003D"},
	  "slot1": {"about": "a wide shot"}, "slot2": {"join": "slideleft"}}`
	os.WriteFile(filepath.Join(root, "answers.json"), []byte(answers), 0o644)
	out.Reset()
	if code := Run(context.Background(), []string{"clone", "out/known.mp4", "--name", "known", "--force", "--read", "answers.json", "--root", root}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("clone with answers: %s", out.String())
	}
	var second struct {
		Reused bool `json:"analysis_reused"`
		Slots  []struct {
			About string `json:"about"`
			Join  string `json:"join"`
		} `json:"slots"`
	}
	json.Unmarshal(out.Bytes(), &second)
	if !second.Reused || second.Slots[0].About != "a wide shot" || second.Slots[1].Join != "slideleft" {
		t.Fatalf("answers not used: %s", out.String())
	}
	raw, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(res.File)))
	var answered recipe.Recipe
	if err := json.Unmarshal(raw, &answered); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range answered.Texts {
		if s.Slot == card.Slot {
			found = s.Text == "Words read by eye\non the same card" && s.Font == "abril" && len(s.Emphasis) == 1
		}
	}
	if !found {
		t.Fatalf("answers not in the recipe: %s", raw)
	}
	// Answers for another video are refused, and nothing is written.
	os.WriteFile(filepath.Join(root, "answers.json"), []byte(`{"text77": ["nope"]}`), 0o644)
	out.Reset()
	if code := Run(context.Background(), []string{"clone", "out/known.mp4", "--name", "known", "--force", "--read", "answers.json", "--root", root}, strings.NewReader(""), &out, &out); code == 0 {
		t.Fatalf("wrong id accepted: %s", out.String())
	}

	// Fill it with other footage, words and a logo; render.
	req := `{"recipe": "known", "clips": [{"src": "media/talk.mp4"}, {"src": "media/tall.mp4"}, {"src": "media/photo.jpg"}],
	  "texts": {"` + card.Slot + `": {"text": "A new headline\nfor the same card"}},
	  "overlays": {"logo": "media/logo.png"}, "write": "filled.json"}`
	if r, code := run(t, root, req, "recipe", "apply", "--input", "-"); code != 0 {
		t.Fatalf("apply: %+v", r)
	}
	r2, code := run(t, root, "", "render", "filled.json", "--preset", "draft")
	if code != 0 || math.Abs(r2.Duration-6) > 0.15 {
		t.Fatalf("render filled: %d %+v", code, r2)
	}
}

// A card on a grid, a colour wash and revealed captions render, and a slot
// marked as the same source asks for one file.
func TestCardWashAndOneSource(t *testing.T) {
	root := testWorkspace(t)
	project := `{"wecut": "1", "output": {"file": "out/card.mp4", "res": 540},
	  "clips": [{"src": "media/tall.mp4", "from": 0, "to": 1},
	    {"src": "media/tall.mp4", "from": 1, "to": 2, "inset": {"x": 0.05, "y": 0.35, "w": 0.9, "h": 0.3, "background": "#101010", "grid": true, "radius": 24}}],
	  "filters": [{"name": "color", "color": "#FF6A00", "amount": 40, "start": 1, "end": 1.5}],
	  "captions": {"words": [{"word": "jual", "start": 0, "end": 0.5}, {"word": "tempat", "start": 0.5, "end": 1}],
	    "mode": "reveal", "color": "#808080", "highlight": "#FFFFFF", "accent": {"color": "#FFD60A", "scale": 130, "italic": true}}}`
	os.WriteFile(filepath.Join(root, "card.json"), []byte(project), 0o644)
	if r, code := run(t, root, "", "render", "card.json", "--preset", "draft"); code != 0 {
		t.Fatalf("render: %+v", r)
	}
	bad := strings.Replace(project, `"name": "color", "color": "#FF6A00"`, `"name": "color"`, 1)
	os.WriteFile(filepath.Join(root, "bad.json"), []byte(bad), 0o644)
	if _, code := run(t, root, "", "validate", "bad.json"); code == 0 {
		t.Error("a color filter without a colour was accepted")
	}

	recipeJSON := `{"recipe": "1", "name": "one-source", "sections": [{"role": "body", "shots": [{"dur": 0.5}, {"dur": 0.5, "continue": true}, {"dur": 0.5, "continue": true}]}]}`
	os.MkdirAll(filepath.Join(root, "recipes"), 0o755)
	os.WriteFile(filepath.Join(root, "recipes", "one-source.json"), []byte(recipeJSON), 0o644)
	req := `{"recipe": "one-source", "clips": [{"src": "media/tall.mp4"}], "write": "one.json"}`
	if r, code := run(t, root, req, "recipe", "apply", "--input", "-"); code != 0 {
		t.Fatalf("apply: %+v", r)
	}
	// Two clips, no roles: one for each section.
	two := `{"recipe": "1", "name": "two-sources", "sections": [{"role": "hook", "shots": [{"dur": 0.5}, {"dur": 0.5, "continue": true}]}, {"role": "body", "shots": [{"dur": 0.5}, {"dur": 0.5, "continue": true}]}]}`
	os.WriteFile(filepath.Join(root, "recipes", "two-sources.json"), []byte(two), 0o644)
	req2 := `{"recipe": "two-sources", "clips": [{"src": "media/tall.mp4"}, {"src": "media/talk.mp4"}], "write": "two.json"}`
	if r, code := run(t, root, req2, "recipe", "apply", "--input", "-"); code != 0 {
		t.Fatalf("apply two: %+v", r)
	}
	bt, _ := os.ReadFile(filepath.Join(root, "two.json"))
	var pt struct{ Clips []struct{ Src string } }
	json.Unmarshal(bt, &pt)
	if len(pt.Clips) != 4 || pt.Clips[0].Src != "media/tall.mp4" || pt.Clips[1].Src != "media/tall.mp4" || pt.Clips[2].Src != "media/talk.mp4" || pt.Clips[3].Src != "media/talk.mp4" {
		t.Fatalf("each section should take its own clip: %s", bt)
	}

	b, _ := os.ReadFile(filepath.Join(root, "one.json"))
	var p struct {
		Clips []struct {
			Src  string
			From float64
		}
	}
	json.Unmarshal(b, &p)
	if len(p.Clips) != 3 || p.Clips[1].From < 0.45 || p.Clips[2].From < 0.95 {
		t.Fatalf("one file should play on through the slots: %s", b)
	}
}

// Crops have common names and old ones are cleared: never in someone's folder.
func TestCloneLeavesOtherFoldersAlone(t *testing.T) {
	root := testWorkspace(t)
	mine := filepath.Join(root, "mine")
	os.MkdirAll(mine, 0o755)
	for _, f := range []string{"logo.png", "text1.png", "compare.jpg"} {
		os.WriteFile(filepath.Join(mine, f), []byte("mine"), 0o644)
	}
	var out bytes.Buffer
	code := Run(context.Background(), []string{"clone", "media/tall.mp4", "--name", "x", "--out", "mine", "--ocr", "off", "--root", root}, strings.NewReader(""), &out, &out)
	if code == 0 || !strings.Contains(out.String(), "E_EXISTS") {
		t.Fatalf("code %d: %s", code, out.String())
	}
	for _, f := range []string{"logo.png", "text1.png", "compare.jpg"} {
		if b, _ := os.ReadFile(filepath.Join(mine, f)); string(b) != "mine" {
			t.Errorf("%s was touched", f)
		}
	}
	// A new folder is fine, and running again in it too.
	for i := 0; i < 2; i++ {
		out.Reset()
		code = Run(context.Background(), []string{"clone", "media/tall.mp4", "--name", "x", "--out", "fresh", "--ocr", "off", "--force", "--root", root}, strings.NewReader(""), &out, &out)
		if code != 0 {
			t.Fatalf("run %d: %s", i, out.String())
		}
	}
}

// Saving a project keeps its cards, fades and caption look, however short it is.
func TestFromProjectKeepsNewLayers(t *testing.T) {
	root := testWorkspace(t)
	project := `{"wecut": "1", "clips": [{"src": "media/tall.mp4", "to": 1, "inset": {"x": 0.1, "y": 0.3, "w": 0.8, "h": 0.3, "grid": true, "radius": 20}}, {"src": "media/tall.mp4", "from": 1, "to": 2}],
	  "gradients": [{"from": 0.6, "to": 0.8}], "filters": [{"name": "color", "color": "#FF6A00", "amount": 40, "start": 0.5, "end": 1}],
	  "captions": {"words": [{"word": "a", "start": 0, "end": 1}], "mode": "reveal", "highlight": "#FFFFFF", "accent": {"color": "#FFD60A"}}}`
	os.WriteFile(filepath.Join(root, "p.json"), []byte(project), 0o644)
	if r, code := run(t, root, "", "recipe", "from-project", "p.json", "--name", "kept", "--write", "recipes/kept.json"); code != 0 {
		t.Fatalf("from-project: %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(root, "recipes", "kept.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r recipe.Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	inset := false
	for _, s := range r.Sections {
		for _, sh := range s.Shots {
			inset = inset || sh.Inset != nil
		}
	}
	if !inset || len(r.Gradients) != 1 || len(r.Filters) != 1 || r.Filters[0].Color != "#FF6A00" || r.Captions == nil || r.Captions.Accent == nil || r.Captions.Mode != "reveal" {
		t.Fatalf("lost something: %s", b)
	}
}
