package clone

import (
	"github.com/13kailouis-stack/wecut/internal/spec"
	"testing"
)

func TestLayoutAnswersOverrideMeasurements(t *testing.T) {
	a := New(Config{W: 1080, H: 1920, Duration: 4, LowFPS: 30})
	a.plan = &Plan{Duration: 4, ContentEnd: 4, Shots: []ShotInfo{{Start: 0, End: 4}}, Blocks: []*Block{{ID: "text1", Start: 0, End: 3, Box: Box{X: 0.2, Y: 0.3, W: 0.6, H: 0.2}, Look: &Look{Font: "roboto", Color: "#FFFFFF", Align: "center"}}}}
	read := map[string]Reading{"text1": {Given: true, Lines: []string{"A headline"}, X: spec.F(0), Y: spec.F(0.6), Size: 42, Align: "left", MaxWidth: spec.F(0.7), LineHeight: spec.F(1.1), Start: spec.F(0.4), End: spec.F(2.5)}, "captions": {Given: true, X: spec.F(0.3), Y: spec.F(0.8)}}
	if issues := CheckAnswers(a.plan, nil, read); len(issues) > 0 {
		t.Fatal(issues)
	}
	r := a.Build(Options{Name: "layout", Read: read}).Recipe
	s := r.Texts[0]
	if *s.X != 0 || *s.Y != 0.6 || s.Size != 42 || s.Align != "left" || s.MaxWidth != 0.7 || s.LineHeight != 1.1 || s.Start != 0.4 || *s.End != 2.5 {
		t.Fatalf("layout not preserved: %+v", s)
	}
	if r.Captions == nil || *r.Captions.X != 0.3 || *r.Captions.Y != 0.8 {
		t.Fatalf("captions: %+v", r.Captions)
	}
	for _, rd := range []Reading{{X: spec.F(1.2)}, {MaxWidth: spec.F(0)}, {LineHeight: spec.F(3)}, {Align: "middle"}, {Start: spec.F(3.5)}, {End: spec.F(5)}} {
		if len(CheckAnswers(a.plan, nil, map[string]Reading{"text1": rd})) == 0 {
			t.Fatalf("accepted invalid answer %+v", rd)
		}
	}
}
