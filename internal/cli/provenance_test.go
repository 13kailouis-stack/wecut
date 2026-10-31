package cli

import (
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationSourcesSeparateReferenceAndFootage(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"reference.mp4", "campaign.mp4"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := workspace.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := &recipe.Recipe{Source: &recipe.Source{File: "reference.mp4"}}
	for _, tc := range []struct {
		src    string
		reused bool
	}{{"campaign.mp4", false}, {"reference.mp4", true}, {"./reference.mp4", true}} {
		got, reused := applicationSources(root, r, &spec.Project{Clips: []spec.Clip{{Src: tc.src}}})
		if reused != tc.reused || got["reference_video"] != "reference.mp4" {
			t.Fatalf("%s: %+v", tc.src, got)
		}
	}
}
