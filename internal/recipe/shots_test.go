package recipe

import (
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// A cloned template: a silent B-roll hook with a headline card over three
// exact shots and a flash, then the talk in two shots (a punch-in on the
// same take), with a logo slot.
func clonedRecipe() *Recipe {
	return &Recipe{
		Recipe: Version, Name: "cloned",
		Sections: []Section{
			{Role: "hook", Fit: "cover", Audio: "next", TransitionOut: &spec.Transition{Type: "flash", Duration: 0.4},
				Shots: []Shot{{Dur: 2}, {Dur: 1.5, ZoomTo: 1.12}, {Dur: 2.4}}},
			{Role: "body", Fit: "contain", Shots: []Shot{{Dur: 4}, {Dur: 3, Continue: true, Zoom: 1.3, Fit: "cover"}}},
		},
		Texts:    []Slot{{Slot: "headline", Text: "Default words", End: spec.F(5.5)}},
		Overlays: []OverlaySlot{{Slot: "logo", X: spec.F(0.2), Y: spec.F(0.08), Width: 0.25}},
	}
}

func shotMedia() media.Set {
	m := media.Set{}
	for k, v := range mset {
		m[k] = v
	}
	m["m/b1.mp4"] = media.Info{Path: "/w/m/b1.mp4", Kind: "video", Duration: 6, Width: 1080, Height: 1920}
	m["m/b2.mp4"] = media.Info{Path: "/w/m/b2.mp4", Kind: "video", Duration: 6, Width: 1080, Height: 1920}
	m["m/logo.png"] = media.Info{Path: "/w/m/logo.png", Kind: "image", Width: 400, Height: 120}
	return m
}

func TestApplyShotsEachSlot(t *testing.T) {
	r := clonedRecipe()
	if issues := Validate(r); len(issues) > 0 {
		t.Fatal(issues)
	}
	in := Input{
		Clips: []Clip{
			{Src: "m/b1.mp4", Role: "hook"}, {Src: "m/b2.mp4", Role: "hook"}, {Src: "m/p1.jpg", Role: "hook"},
			{Src: "m/talk.mp4", From: 30, Role: "body"},
		},
		Overlays: map[string]string{"logo": "m/logo.png"},
	}
	m := shotMedia()
	p, warns, errs := Apply(r, in, m)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(p.Clips) != 5 {
		t.Fatalf("%d clips: %+v", len(p.Clips), p.Clips)
	}
	c := p.Clips
	// One clip per hook slot, each from its start, exact lengths, silent.
	if c[0].Src != "m/b1.mp4" || c[0].From != 0 || *c[0].To != 2 || !c[0].Mute {
		t.Errorf("slot 1 %+v", c[0])
	}
	if c[1].Src != "m/b2.mp4" || *c[1].To != 1.5 || c[1].ZoomTo != 1.12 {
		t.Errorf("slot 2 %+v", c[1])
	}
	if c[2].Src != "m/p1.jpg" || *c[2].Duration != 2.4 || c[2].Transition == nil || c[2].Transition.Type != "flash" {
		t.Errorf("slot 3 %+v", c[2])
	}
	// The talk lends 5.5 s (hook 5.9 minus the 0.4 s flash) to play under
	// the hook, then carries on: 35.5..39.5, then the punch-in 39.5..42.5.
	if c[3].Src != "m/talk.mp4" || !near(c[3].From, 35.5) || !near(*c[3].To, 39.5) || c[3].Fit != "contain" || c[3].Mute {
		t.Errorf("body slot %+v", c[3])
	}
	if c[4].Src != "m/talk.mp4" || !near(c[4].From, 39.5) || !near(*c[4].To, 42.5) || c[4].Zoom != 1.3 || c[4].Fit != "cover" {
		t.Errorf("punch-in %+v", c[4])
	}
	if len(p.Audio) != 1 || p.Audio[0].Src != "m/talk.mp4" || p.Audio[0].From != 30 || p.Audio[0].At != 0 || !near(*p.Audio[0].Duration, 5.5) {
		t.Errorf("lead audio %+v", p.Audio)
	}
	if len(p.Overlays) != 1 || p.Overlays[0].Src != "m/logo.png" || p.Overlays[0].Width != 0.25 {
		t.Errorf("overlays %+v", p.Overlays)
	}
	if len(p.Texts) != 1 || p.Texts[0].Text != "Default words" {
		t.Errorf("texts %+v", p.Texts)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "play twice") {
			t.Errorf("unexpected warning %s", w.Message)
		}
	}
	e, issues := engine.Resolve(p, m, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	// The same length as the template: 2+1.5+2.4-0.4 + 4+3.
	if !near(e.Total, 12.5) {
		t.Errorf("total %.3f", e.Total)
	}
}

func TestApplyShotsSequenceAndWrap(t *testing.T) {
	r := clonedRecipe()
	r.Sections[0].Audio = ""
	m := shotMedia()
	// One B-roll clip with 4 s left for 5.9 s of hook slots: it plays on,
	// then wraps to where it started.
	p, warns, errs := Apply(r, Input{Clips: []Clip{{Src: "m/b1.mp4", From: 2, Role: "hook"}, {Src: "m/talk.mp4", Role: "body"}}}, m)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	c := p.Clips
	if c[0].From != 2 || *c[0].To != 4 || c[1].From != 4 || *c[1].To != 5.5 {
		t.Errorf("sequence %+v %+v", c[0], c[1])
	}
	if c[2].Src != "m/b1.mp4" || c[2].From != 2 || *c[2].To != 4.4 {
		t.Errorf("wrap %+v", c[2])
	}
	found := false
	for _, w := range warns {
		found = found || strings.Contains(w.Message, "play twice")
	}
	if !found {
		t.Errorf("no wrap warning in %+v", warns)
	}
	if len(p.Overlays) != 0 {
		t.Errorf("logo without a picture should be left out: %+v", p.Overlays)
	}
}

func TestValidateShots(t *testing.T) {
	r := clonedRecipe()
	r.Sections[1].Audio = "next"
	r.Sections[0].Shots[0].Continue = true
	r.Sections[0].Shots[2].Join = &spec.Transition{Type: "fade"}
	r.Overlays = append(r.Overlays, OverlaySlot{Slot: "logo"})
	var got []string
	for _, is := range Validate(r) {
		got = append(got, is.Path)
	}
	for _, want := range []string{"/sections/1/audio", "/sections/0/shots/0/continue", "/sections/0/shots/2/join", "/overlays/1/slot"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("no issue at %s (got %v)", want, got)
		}
	}
}
