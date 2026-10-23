// Package workspace keeps every file WeCut reads or writes inside one root
// folder. Agents may be steered by text they read elsewhere, so a project
// must not be able to read ~/.ssh or overwrite files outside the job.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Root struct{ dir string }

var ErrOutside = errors.New("path is outside the workspace")

// Open resolves the workspace root (default: the current directory).
func Open(dir string) (*Root, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", dir, err)
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("workspace %s is not a folder", dir)
	}
	return &Root{dir: real}, nil
}

func (r *Root) Dir() string { return r.dir }

func (r *Root) inside(p string) bool {
	rel, err := filepath.Rel(r.dir, p)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (r *Root) join(p string) string {
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return filepath.Clean(p)
	}
	return filepath.Join(r.dir, filepath.FromSlash(p))
}

// Rel shows a path the way the agent wrote it: relative, with forward slashes.
func (r *Root) Rel(abs string) string {
	rel, err := filepath.Rel(r.dir, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// Input returns the real path of an existing regular file inside the root.
// Symlinks are followed and the target must also be inside.
func (r *Root) Input(p string) (string, error) {
	if strings.Contains(p, "://") {
		return "", fmt.Errorf("%w: URLs are not accepted", ErrOutside)
	}
	abs := r.join(p)
	if !r.inside(abs) {
		return "", fmt.Errorf("%w: %s", ErrOutside, p)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", p)
		}
		return "", err
	}
	if !r.inside(real) {
		return "", fmt.Errorf("%w: %s links to a file outside", ErrOutside, p)
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", p)
	}
	return real, nil
}

// Output returns where to write p, creating missing folders. Folders that
// already exist are resolved through symlinks so a link cannot point the
// output outside the root.
func (r *Root) Output(p string) (string, error) {
	abs := r.join(p)
	if !r.inside(abs) {
		return "", fmt.Errorf("%w: %s", ErrOutside, p)
	}
	dir, base := filepath.Split(abs)
	dir = filepath.Clean(dir)
	existing, rest := dir, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("cannot create %s", p)
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if !r.inside(real) {
		return "", fmt.Errorf("%w: %s", ErrOutside, p)
	}
	target := filepath.Join(real, rest)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(target, base)
	if st, err := os.Lstat(out); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: output %s is a symlink", ErrOutside, p)
		}
		if st.IsDir() {
			return "", fmt.Errorf("output %s is a folder", p)
		}
	}
	return out, nil
}
