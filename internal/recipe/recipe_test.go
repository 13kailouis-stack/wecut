package recipe

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

func load(t *testing.T, name string) *Recipe {
	t.Helper()
	b, ok := wecut.Recipe(name)
	if !ok {
		t.Fatalf("no built-in recipe %s", name)
	}
	var r Recipe
	if issues := spec.DecodeAs(b, &r); len(issues) > 0 {
		t.Fatalf("%s: %v", name, issues)
	}
	if issues := Validate(&r); len(issues) > 0 {
		t.Fatalf("%s: %v", name, issues)
	}
	return &r
}

func TestBuiltinRecipesAreValid(t *testing.T) {
	names := wecut.RecipeNames()
	if len(names) < 5 {
		t.Fatalf("only %d recipes", len(names))
	}
	for _, n := range names {
		r := load(t, n)
		if r.Name != n || r.About == "" {
			t.Errorf("%s: name %q, about %q", n, r.Name, r.About)
		}
	}
}

func TestRecipeSchemaMatchesTypes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "wecut.recipe.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	keys := func(path ...string) []string {
		var node any = s
		for _, p := range path {
			node = node.(map[string]any)[p]
		}
		var out []string
		for k := range node.(map[string]any) {
			out = append(out, k)
		}
		return out
	}
	both := func(a, b []string) []string {
		out := append(append([]string{}, a...), b...)
		sort.Strings(out)
		return slices.Compact(out)
	}
	check := func(name string, got []string, v any) {
		sort.Strings(got)
		if want := spec.JSONFieldNames(v); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: schema %v, Go %v", name, got, want)
		}
	}
	look := keys("$defs", "look", "properties")
	check("recipe", keys("properties"), Recipe{})
	check("output", keys("properties", "output", "properties"), Output{})
	check("section", keys("properties", "sections", "items", "properties"), Section{})
	check("slot", both(keys("$defs", "slot", "properties"), look), Slot{})
	check("captions", both(keys("properties", "captions", "properties"), look), CaptionStyle{})
	check("music", keys("properties", "music", "properties"), Music{})
	check("effect", keys("properties", "effects", "items", "properties"), EffectRule{})
	check("shot", keys("$defs", "shot", "properties"), Shot{})
	check("overlay", keys("properties", "overlays", "items", "properties"), OverlaySlot{})
	check("source", keys("properties", "source", "properties"), Source{})
	check("tempo", keys("properties", "tempo", "properties"), Tempo{})
}

func TestValidateCatchesMistakes(t *testing.T) {
	r := &Recipe{Recipe: "1", Sections: []Section{
		{Role: "hook", Fit: "fill", TransitionOut: &spec.Transition{Type: "spin"}, Texts: []Slot{{Slot: "a", Look: spec.Look{Style: "nope"}}}},
		{Role: "hook", Texts: []Slot{{Slot: "a"}}, TransitionOut: &spec.Transition{Type: "fade"}},
	}, Effects: []EffectRule{{Name: "flash", On: "beats"}}}
	var got []string
	for _, i := range Validate(r) {
		got = append(got, i.Path)
	}
	want := []string{"/sections/0/fit", "/sections/0/transitionOut/type", "/sections/0/texts/0/style", "/sections/1/role", "/sections/1/transitionOut", "/sections/1/texts/0/slot", "/effects/0/on"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

var mset = media.Set{
	"m/talk.mp4":  {Path: "/w/m/talk.mp4", Kind: "video", Duration: 600, Width: 1920, Height: 1080, HasAudio: true},
	"m/broll.mp4": {Path: "/w/m/broll.mp4", Kind: "video", Duration: 8, Width: 1080, Height: 1920},
	"m/p1.jpg":    {Path: "/w/m/p1.jpg", Kind: "image", Width: 1600, Height: 1200},
	"m/p2.jpg":    {Path: "/w/m/p2.jpg", Kind: "image", Width: 1600, Height: 1200},
	"m/p3.jpg":    {Path: "/w/m/p3.jpg", Kind: "image", Width: 1600, Height: 1200},
	"m/music.mp3": {Path: "/w/m/music.mp3", Kind: "audio", Duration: 30, HasAudio: true},
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestApplyNewsHeadline(t *testing.T) {
	r := load(t, "news-headline")
	in := Input{
		Clips:      []Clip{{Src: "m/broll.mp4", Role: "hook"}, {Src: "m/talk.mp4", From: 100, To: spec.F(140)}},
		Texts:      map[string]SlotText{"headline": {Text: "Viral!! Toko ini diduga curang", Emphasis: []string{"diduga curang"}}, "nope": {Text: "x"}},
		Transcript: "m/talk.words.json",
	}
	p, warns, errs := Apply(r, in, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(p.Clips) != 2 || p.Clips[0].Src != "m/broll.mp4" || *p.Clips[0].To != 5 || p.Clips[0].Transition.Type != "flash" || p.Clips[0].FadeIn != 0.4 {
		t.Fatalf("hook clip %+v", p.Clips[0])
	}
	if p.Clips[1].Fit != "contain" || p.Clips[1].From != 100 || *p.Clips[1].To != 140 {
		t.Fatalf("body clip %+v", p.Clips[1])
	}
	if len(p.Texts) != 1 || p.Texts[0].Style != "headline" || !near(*p.Texts[0].End, 5) || p.Texts[0].Emphasis[0] != "diduga curang" {
		t.Fatalf("texts %+v", p.Texts)
	}
	if p.Captions == nil || p.Captions.Src != "m/talk.mp4" || p.Captions.Style != "word" {
		t.Fatalf("captions %+v", p.Captions)
	}
	msgs := ""
	for _, w := range warns {
		msgs += w.Message + "|"
	}
	for _, want := range []string{`slot "label" has no text`, `no slot "nope"`, "the recipe has music"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("missing warning %q in %s", want, msgs)
		}
	}
	// The project it makes must resolve against the media.
	if _, issues := engine.Resolve(p, mset, nil); len(issues) > 0 {
		t.Fatal(issues)
	}
}

func TestApplyBorrowsHookFromBody(t *testing.T) {
	r := load(t, "news-headline")
	p, _, errs := Apply(r, Input{Clips: []Clip{{Src: "m/talk.mp4", From: 10, To: spec.F(40)}}}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	// hook = talk 10..15, body continues 15..40
	if len(p.Clips) != 2 || p.Clips[0].From != 10 || *p.Clips[0].To != 15 || p.Clips[1].From != 15 || *p.Clips[1].To != 40 {
		t.Fatalf("clips %+v %+v", p.Clips[0], p.Clips[1])
	}
}

func TestApplyPodcastPunch(t *testing.T) {
	r := load(t, "podcast-punch")
	p, _, errs := Apply(r, Input{
		Clips: []Clip{{Src: "m/talk.mp4", From: 0, To: spec.F(20)}},
		Texts: map[string]SlotText{"hook": {Text: "Watch this"}},
		Music: "m/music.mp3",
	}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(p.Clips) != 8 {
		t.Fatalf("%d shots", len(p.Clips))
	}
	for i, c := range p.Clips {
		if (i%2 == 1) != (c.Zoom == 1.12) || !near(*c.To-c.From, 2.5) {
			t.Fatalf("shot %d %+v", i, c)
		}
	}
	var cta *spec.Text
	for i := range p.Texts {
		if p.Texts[i].Text == "Follow for part 2" {
			cta = &p.Texts[i]
		}
	}
	if cta == nil || !near(cta.Start, 17.5) || !near(*cta.End, 20) {
		t.Fatalf("cta %+v", cta)
	}
	if len(p.Audio) != 1 || !p.Audio[0].Duck || *p.Audio[0].Volume != 15 {
		t.Fatalf("music %+v", p.Audio)
	}
}

func TestApplySlideshow(t *testing.T) {
	r := load(t, "photo-slideshow")
	p, _, errs := Apply(r, Input{Clips: []Clip{{Src: "m/p1.jpg"}, {Src: "m/p2.jpg"}, {Src: "m/p3.jpg"}}, Texts: map[string]SlotText{"title": {Text: "Bali"}}}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	motions := []string{p.Clips[0].Motion, p.Clips[1].Motion, p.Clips[2].Motion}
	if !reflect.DeepEqual(motions, []string{"in", "left", "out"}) || p.Clips[0].Transition == nil || p.Clips[2].Transition != nil {
		t.Fatalf("clips %+v", p.Clips)
	}
	e, issues := engine.Resolve(p, mset, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if !near(e.Total, 9-0.5-0.5) {
		t.Fatalf("total %v", e.Total)
	}
}

func TestApplyErrors(t *testing.T) {
	r := load(t, "news-headline")
	_, _, errs := Apply(r, Input{Clips: []Clip{{Src: "m/talk.mp4", Role: "intro"}, {Src: "m/music.mp3"}}}, mset)
	if len(errs) != 2 || errs[0].Path != "/clips/0/role" || errs[1].Path != "/clips/1/src" {
		t.Fatalf("%v", errs)
	}
}

// A project turned into a recipe and applied to the same footage keeps its
// structure: sections, transitions, text slots and style.
func TestFromEditRoundTrip(t *testing.T) {
	r := load(t, "news-headline")
	in := Input{
		Clips: []Clip{{Src: "m/broll.mp4", Role: "hook"}, {Src: "m/talk.mp4", From: 100, To: spec.F(130)}},
		Texts: map[string]SlotText{"headline": {Text: "Big news"}, "label": {Text: "Page"}},
	}
	p, _, errs := Apply(r, in, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	e, issues := engine.Resolve(p, mset, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	d := FromEdit(e, "mine")
	if issues := Validate(d); len(issues) > 0 {
		t.Fatal(issues)
	}
	if len(d.Sections) != 2 || d.Sections[0].Role != "hook" || d.Sections[0].TransitionOut == nil || d.Sections[1].Fit != "contain" {
		t.Fatalf("sections %+v", d.Sections)
	}
	if len(d.Sections[0].Texts) != 2 || d.Sections[0].Texts[0].Slot != "label" || d.Sections[0].Texts[1].Slot != "headline" || d.Sections[0].Texts[1].Text != "Big news" {
		t.Fatalf("slots %+v", d.Sections[0].Texts)
	}
	again, _, errs := Apply(d, Input{Clips: []Clip{{Src: "m/broll.mp4", Role: "hook"}, {Src: "m/talk.mp4", From: 200, To: spec.F(230)}}}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(again.Clips) != 2 || again.Clips[1].From != 200 || len(again.Texts) != 2 {
		t.Fatalf("re-applied %+v", again)
	}
}

func TestRecipeMusicDefault(t *testing.T) {
	r := load(t, "podcast-punch")
	r.Music.Src, r.Music.From = "m/music.mp3", 4
	clips := []Clip{{Src: "m/talk.mp4", To: spec.F(10)}}
	p, warns, _ := Apply(r, Input{Clips: clips}, mset)
	if len(p.Audio) != 1 || p.Audio[0].Src != "m/music.mp3" || p.Audio[0].From != 4 || len(warns) == 0 {
		t.Fatalf("default track: %+v %v", p.Audio, warns)
	}
	p, _, _ = Apply(r, Input{Clips: clips, Music: "m/broll.mp4"}, mset)
	if p.Audio[0].Src != "m/broll.mp4" || p.Audio[0].From != 0 {
		t.Fatalf("override: %+v", p.Audio)
	}
	p, _, _ = Apply(r, Input{Clips: clips, Music: "none"}, mset)
	if len(p.Audio) != 0 {
		t.Fatalf("none: %+v", p.Audio)
	}
}

func TestApplyKeepsTextAnimation(t *testing.T) {
	r := &Recipe{Recipe: "1", Name: "card", Sections: []Section{{Role: "body", Duration: 4}},
		Texts: []Slot{{Slot: "title", Text: "Hi", Start: 0.2, End: spec.F(2), In: &spec.Anim{Type: "pop", Duration: 0.28}}}}
	if issues := Validate(r); len(issues) > 0 {
		t.Fatal(issues)
	}
	p, _, errs := Apply(r, Input{Clips: []Clip{{Src: "m/talk.mp4", To: spec.F(10)}}}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(p.Texts) != 1 || p.Texts[0].In == nil || p.Texts[0].In.Type != "pop" || p.Texts[0].In.Duration != 0.28 {
		t.Fatalf("text %+v", p.Texts)
	}
}

func TestApplySnapsShotsToNewTempo(t *testing.T) {
	r := &Recipe{
		Recipe: "1", Name: "beat", MaxDuration: 8,
		Tempo: &Tempo{BPM: 120, Offset: 0.1},
		Sections: []Section{{Role: "body", Shots: []Shot{
			{Dur: 1, Beat: spec.F(0), Join: &spec.Transition{Type: "fade", Duration: 0.2}},
			{Dur: 1, Beat: spec.F(2)},
			{Dur: 1, Beat: spec.F(4)},
		}}},
		Texts: []Slot{{Slot: "title", Text: "Hi", Start: 0, End: spec.F(1), StartBeat: spec.F(0), EndBeat: spec.F(2)}},
		Music: &Music{Volume: spec.F(40), Duck: true},
	}
	if issues := Validate(r); len(issues) > 0 {
		t.Fatal(issues)
	}
	p, warns, errs := Apply(r, Input{
		Clips:       []Clip{{Src: "m/talk.mp4", To: spec.F(30)}},
		Music:       "m/music.mp3",
		MusicBPM:    60,
		MusicOffset: 0.05,
	}, mset)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	// 120 → 60 BPM doubles every second. Phase 0.1*2 - 0.05 = 0.15, so the
	// new track waits 0.15 s and its beat lands on the scaled edit.
	if len(p.Clips) != 3 || !near(*p.Clips[0].To-p.Clips[0].From, 2) || !near(*p.Clips[2].To-p.Clips[2].From, 2) {
		t.Fatalf("clips %+v", p.Clips)
	}
	if p.Clips[0].Transition == nil || !near(p.Clips[0].Transition.Duration, 0.4) {
		t.Fatalf("join %+v", p.Clips[0].Transition)
	}
	if len(p.Texts) != 1 || !near(*p.Texts[0].End, 2) {
		t.Fatalf("text %+v", p.Texts)
	}
	if len(p.Audio) != 1 || p.Audio[0].Src != "m/music.mp3" || !near(p.Audio[0].At, 0.15) || p.Audio[0].From != 0 || *p.Audio[0].Volume != 40 {
		t.Fatalf("music %+v", p.Audio)
	}
	var note string
	for _, w := range warns {
		note += w.Message
	}
	if !strings.Contains(note, "60 BPM") {
		t.Fatalf("warning %q", note)
	}
	// The caller's recipe is unchanged, and no new grid means no snap.
	if r.Sections[0].Shots[0].Dur != 1 {
		t.Fatalf("recipe mutated: %v", r.Sections[0].Shots[0].Dur)
	}
	same, _, errs := Apply(r, Input{Clips: []Clip{{Src: "m/talk.mp4", To: spec.F(30)}}, Music: "m/music.mp3"}, mset)
	if len(errs) > 0 || !near(*same.Clips[0].To-same.Clips[0].From, 1) {
		t.Fatalf("without a measured tempo: %+v %v", same.Clips[0], errs)
	}
	// Same song, far from the template's tempo, keeps the seconds.
	far, warns, errs := Apply(r, Input{Clips: []Clip{{Src: "m/talk.mp4", To: spec.F(30)}}, Music: "m/music.mp3", MusicBPM: 300}, mset)
	if len(errs) > 0 || !near(*far.Clips[0].To-far.Clips[0].From, 1) {
		t.Fatalf("too far: %+v %v", far.Clips[0], errs)
	}
	if joined := warns[0].Message; !strings.Contains(joined, "left off") {
		t.Fatalf("warning %q", joined)
	}
}
