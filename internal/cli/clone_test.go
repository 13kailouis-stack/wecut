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
		File   string `json:"file"`
		Events []struct {
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
