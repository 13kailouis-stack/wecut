package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleAcrossWorkspaces(t *testing.T) {
	human, agent := t.TempDir(), t.TempDir()
	original := []byte("music from the human device")
	os.WriteFile(filepath.Join(human, "song.mp3"), original, 0o644)
	os.WriteFile(filepath.Join(human, "recipe.json"), []byte(`{"recipe":"1","name":"portable","sections":[{"role":"body"}],"music":{"src":"song.mp3","volume":30}}`), 0o644)
	if r, c := run(t, human, "", "recipe", "pack", "recipe.json", "--write", "shared.wecut.json"); c != 0 {
		t.Fatalf("pack: %+v", r)
	}
	data, err := os.ReadFile(filepath.Join(human, "shared.wecut.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(agent, "received.wecut.json"), data, 0o644)
	if r, c := run(t, agent, "", "recipe", "unpack", "received.wecut.json", "--write", "templates/style"); c != 0 {
		t.Fatalf("unpack: %+v", r)
	}
	copied, err := os.ReadFile(filepath.Join(agent, "templates/style/assets/music.mp3"))
	if err != nil || string(copied) != string(original) {
		t.Fatalf("music: %q %v", copied, err)
	}
	recipeData, _ := os.ReadFile(filepath.Join(agent, "templates/style/recipe.json"))
	var r struct {
		Music struct {
			Src string `json:"src"`
		} `json:"music"`
	}
	json.Unmarshal(recipeData, &r)
	if r.Music.Src != "templates/style/assets/music.mp3" {
		t.Fatalf("path was not rebased: %s", r.Music.Src)
	}
	if _, c := run(t, agent, "", "recipe", "unpack", "received.wecut.json", "--write", "templates/style"); c == 0 {
		t.Fatal("overwrote an existing import")
	}
	if _, c := run(t, human, "", "recipe", "pack", "recipe.json", "--write", "shared.wecut.json"); c == 0 {
		t.Fatal("overwrote an existing package")
	}
}

func TestBundleRejectsUnsafeInputs(t *testing.T) {
	for _, body := range []string{
		`{"bundle":"wecut.bundle/1","recipe":{"recipe":"1","sections":[{"role":"body"}],"music":{"src":"../../outside.mp3"}},"assets":[]}`,
		`{"bundle":"wecut.bundle/1","recipe":{"recipe":"1","sections":[{"role":"body"}],"music":{"src":"assets/../evil.mp3"}},"assets":[{"name":"../evil.mp3","data":"YQ=="}]}`,
		`{"bundle":"wecut.bundle/1","recipe":{"recipe":"1","sections":[{"role":"body"}],"music":{"src":"assets/music.mp3"}},"assets":[{"name":"music.mp3","data":"not base64!"}]}`,
		`{"bundle":"wecut.bundle/2","recipe":{"recipe":"1","sections":[{"role":"body"}]},"assets":[]}`,
		`{"bundle":"wecut.bundle/1","recipe":{"recipe":"1","sections":[{"role":"body"}],"unknown":true},"assets":[]}`,
		`{"bundle":"wecut.bundle/1","recipe":{"recipe":"1","sections":[{"role":"body"}]},"assets":[]} {}`,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "bad.json"), []byte(body), 0o644)
		if r, c := run(t, dir, "", "recipe", "unpack", "bad.json", "--write", "new"); c == 0 {
			t.Fatalf("accepted unsafe bundle: %+v", r)
		}
		if _, err := os.Stat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
			t.Fatal("invalid import left a directory")
		}
	}
}

func TestBundleWithoutMusic(t *testing.T) {
	dir := t.TempDir()
	if r, c := run(t, dir, "", "recipe", "pack", "podcast-punch", "--write", "plain.json"); c != 0 {
		t.Fatalf("pack: %+v", r)
	}
	if r, c := run(t, dir, "", "recipe", "unpack", "plain.json", "--write", "template"); c != 0 {
		t.Fatalf("unpack: %+v", r)
	}
}

func TestBundleWorkspaceAndSizeLimits(t *testing.T) {
	dir := t.TempDir()
	if r, c := run(t, dir, "", "recipe", "pack", "podcast-punch", "--write", "bundle.json"); c != 0 {
		t.Fatalf("pack: %+v", r)
	}
	if _, c := run(t, dir, "", "recipe", "unpack", "bundle.json", "--write", "../outside"); c == 0 {
		t.Fatal("allowed a destination outside the workspace")
	}
	if _, err := readBounded(filepath.Join(dir, "bundle.json"), 2); err == nil {
		t.Fatal("read limit ignored")
	}
	if r, c := run(t, dir, "", "recipe", "unpack", "bundle.json", "--write", "inside"); c != 0 {
		t.Fatalf("unpack: %+v", r)
	}
	// Repack an imported recipe: no dependency on the original sender remains.
	if r, c := run(t, dir, "", "recipe", "pack", "inside/recipe.json", "--write", "again.json"); c != 0 {
		t.Fatalf("repack: %+v", r)
	}
}

func TestBundleRenderOnOtherDevice(t *testing.T) {
	human := testWorkspace(t)
	agent := t.TempDir()
	// The receiving workspace has its own footage, and no access to the sender's paths.
	video, err := os.ReadFile(filepath.Join(human, "media/tall.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(agent, "replacement.mp4"), video, 0o644)
	os.WriteFile(filepath.Join(human, "recipe.json"), []byte(`{"recipe":"1","name":"cross-device","maxDuration":3,"sections":[{"role":"clip1","duration":3}],"texts":[{"slot":"title","text":"From another device","start":0,"end":2}],"music":{"src":"media/beep.mp3","from":0.25,"at":0.5,"duration":2,"volume":40}}`), 0o644)
	if r, c := run(t, human, "", "recipe", "pack", "recipe.json", "--write", "bundle.json"); c != 0 {
		t.Fatalf("pack: %+v", r)
	}
	data, _ := os.ReadFile(filepath.Join(human, "bundle.json"))
	os.WriteFile(filepath.Join(agent, "bundle.json"), data, 0o644)
	if r, c := run(t, agent, "", "recipe", "unpack", "bundle.json", "--write", "template"); c != 0 {
		t.Fatalf("unpack: %+v", r)
	}
	req := `{"recipe":"template/recipe.json","clips":[{"src":"replacement.mp4","role":"clip1"}],"texts":{"title":{"text":"Remixed on agent device"}},"write":"project.json"}`
	if r, c := run(t, agent, req, "recipe", "apply", "--input", "-"); c != 0 {
		t.Fatalf("apply: %+v", r)
	}
	project, _ := os.ReadFile(filepath.Join(agent, "project.json"))
	var p struct {
		Audio []struct {
			At       float64
			Duration float64
		}
	}
	json.Unmarshal(project, &p)
	if len(p.Audio) != 1 || p.Audio[0].At != 0.5 || p.Audio[0].Duration != 2 {
		t.Fatalf("music timing lost: %s", project)
	}
	if r, c := run(t, agent, "", "render", "project.json", "--preset", "draft"); c != 0 || !r.HasAudio {
		t.Fatalf("render: %+v", r)
	}
}
