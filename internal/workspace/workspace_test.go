package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) (*Root, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	must(t, os.MkdirAll(filepath.Join(root, "media"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "media", "a.mp4"), []byte("x"), 0o644))
	must(t, os.WriteFile(filepath.Join(base, "secret.txt"), []byte("x"), 0o644))
	r, err := Open(root)
	must(t, err)
	return r, base
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestInput(t *testing.T) {
	r, base := setup(t)
	if _, err := r.Input("media/a.mp4"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Input(filepath.Join(r.Dir(), "media", "a.mp4")); err != nil {
		t.Fatal("absolute path inside the root:", err)
	}
	for _, p := range []string{"../secret.txt", "media/../../secret.txt", filepath.Join(base, "secret.txt"), "https://x/a.mp4"} {
		if _, err := r.Input(p); !errors.Is(err, ErrOutside) {
			t.Errorf("%s: got %v, want ErrOutside", p, err)
		}
	}
	if _, err := r.Input("media/missing.mp4"); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := r.Input("media"); err == nil {
		t.Error("folder accepted as input")
	}
}

func TestInputSymlinkOut(t *testing.T) {
	r, base := setup(t)
	link := filepath.Join(r.Dir(), "media", "link.mp4")
	if err := os.Symlink(filepath.Join(base, "secret.txt"), link); err != nil {
		t.Skip("cannot create symlinks here:", err)
	}
	if _, err := r.Input("media/link.mp4"); !errors.Is(err, ErrOutside) {
		t.Fatalf("symlink out: got %v", err)
	}
}

func TestOutput(t *testing.T) {
	r, base := setup(t)
	p, err := r.Output("out/deep/clip.mp4")
	must(t, err)
	if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
		t.Fatal("output folder not created")
	}
	if r.Rel(p) != "out/deep/clip.mp4" {
		t.Fatalf("rel %s", r.Rel(p))
	}
	for _, bad := range []string{"../x.mp4", filepath.Join(base, "x.mp4")} {
		if _, err := r.Output(bad); !errors.Is(err, ErrOutside) {
			t.Errorf("%s: got %v", bad, err)
		}
	}
	if _, err := r.Output("media"); err == nil {
		t.Error("folder accepted as output")
	}
	out := filepath.Join(base, "elsewhere")
	must(t, os.MkdirAll(out, 0o755))
	if err := os.Symlink(out, filepath.Join(r.Dir(), "escape")); err == nil {
		if _, err := r.Output("escape/x.mp4"); !errors.Is(err, ErrOutside) {
			t.Errorf("symlinked folder out: got %v", err)
		}
	}
}
