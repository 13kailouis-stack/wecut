package clone

import (
	"strings"
	"testing"
)

func TestCheckAnswers(t *testing.T) {
	p := &Plan{
		Blocks:   []*Block{{ID: "text1"}},
		Logos:    []*Block{{ID: "logo"}},
		Captions: &CaptionBand{},
		Shots:    []ShotInfo{{Join: "zoom"}, {}},
	}
	crops := []Crop{{ID: "caption1", Kind: "caption"}}
	good := map[string]Reading{
		"text1":    {Lines: []string{"hello"}, Font: "roboto", Color: "#112233", Stroke: "none", EmphasisColor: "#E8003D"},
		"logo":     {Drop: true},
		"caption1": {Lines: []string{"WORD"}},
		"captions": {Font: "creepster", Mode: "pop"},
		"slot1":    {About: "wide shot", Join: "slideleft"},
		"slot2":    {About: "close up"},
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
	} {
		got := CheckAnswers(p, crops, map[string]Reading{id: tc.rd})
		if len(got) != 1 || !strings.Contains(got[0], tc.want) {
			t.Errorf("%s: got %v, want %q", id, got, tc.want)
		}
	}
}
