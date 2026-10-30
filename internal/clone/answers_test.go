package clone

import (
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

func TestCheckAnswers(t *testing.T) {
	p := &Plan{
		Blocks:   []*Block{{ID: "text1"}},
		Logos:    []*Block{{ID: "logo"}},
		Captions: &CaptionBand{},
		Shots:    []ShotInfo{{Join: "zoom"}, {}},
	}
	crops := []Crop{{ID: "caption1", Kind: "caption"}}
	half, tooFar := 0.5, 1.5
	good := map[string]Reading{
		"text1":    {Lines: []string{"hello"}, Font: "roboto", Color: "#112233", Stroke: "none", EmphasisColor: "#E8003D"},
		"logo":     {Drop: true},
		"caption1": {Lines: []string{"WORD"}},
		"captions": {Font: "creepster", Mode: "karaoke", Highlight: "#FFFFFF", Background: "none", Size: 60, Y: &half},
		"slot1":    {About: "wide shot", Join: "slideleft"},
		"slot2":    {About: "close up", Continue: true, Inset: &spec.Inset{X: 0.05, Y: 0.3, W: 0.9, H: 0.3, Grid: true}},
		"filter1":  {Color: "#FF6A00", Start: &half, Opacity: &half},
		"filter2":  {Name: "warm"},
	}
	if got := CheckAnswers(p, crops, good); len(got) != 0 {
		t.Fatalf("good answers refused: %v", got)
	}
	for id, tc := range map[string]struct {
		rd   Reading
		want string
	}{
		"text9":    {Reading{Lines: []string{"x"}}, "not in this video"},
		"slot3":    {Reading{About: "x"}, "slots 1 to 2"},
		"slotx":    {Reading{About: "x"}, "slots 1 to 2"},
		"slot2":    {Reading{Join: "slideleft"}, "hard cut"},
		"slot1":    {Reading{Join: "nope"}, "unknown transition"},
		"text1":    {Reading{Font: "comic-sans"}, "unknown font"},
		"logo":     {Reading{Color: "red"}, "#RRGGBB"},
		"caption1": {Reading{Stroke: "blue"}, "stroke"},
		"captions": {Reading{Mode: "dance"}, "mode"},
		"logo2":    {Reading{About: "x"}, "not in this video"},
		"filterx":  {Reading{Name: "warm"}, "numbered"},
		"filter3":  {Reading{Name: "nope"}, "unknown filter"},
		"filter4":  {Reading{Start: &half}, "name (a filter) or color"},
	} {
		got := CheckAnswers(p, crops, map[string]Reading{id: tc.rd})
		if len(got) != 1 || !strings.Contains(got[0], tc.want) {
			t.Errorf("%s: got %v, want %q", id, got, tc.want)
		}
	}
	for want, rd := range map[string]Reading{"background": {Background: "red"}, "fraction": {Y: &tooFar}, "size": {Size: 900}, "colour": {Highlight: "white"}} {
		got := CheckAnswers(p, crops, map[string]Reading{"captions": rd})
		if len(got) != 1 || !strings.Contains(got[0], want) {
			t.Errorf("%s: got %v", want, got)
		}
	}
	if got := CheckAnswers(p, crops, map[string]Reading{"slot1": {Continue: true}}); len(got) != 1 {
		t.Errorf("continue on the first slot: %v", got)
	}
	if got := CheckAnswers(p, crops, map[string]Reading{"slot2": {Inset: &spec.Inset{X: 0.5, W: 0.8, H: 0.2}}}); len(got) != 1 {
		t.Errorf("inset off the frame: %v", got)
	}
	if got := CheckAnswers(p, crops, map[string]Reading{"captions": {Accent: &spec.Accent{Font: "nope", Scale: 900, Color: "red"}}}); len(got) != 3 {
		t.Errorf("accent: %v", got)
	}
	// Captions the detector missed can still be answered.
	if got := CheckAnswers(&Plan{}, nil, map[string]Reading{"captions": {Font: "roboto"}}); len(got) != 0 {
		t.Errorf("captions without a band: %v", got)
	}
}
