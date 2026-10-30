// Package assets carries the files WeCut needs at render time inside the
// binary, so a render works offline with nothing else installed but FFmpeg.
package assets

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// The engine measures text with the embedded fonts.
func init() { engine.FontData = Font }

//go:embed fonts/*.ttf
var fonts embed.FS

// WriteFonts copies the named fonts into dir for libass to load.
func WriteFonts(dir string, keys []string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, k := range keys {
		f, ok := spec.FontByKey(k)
		if !ok {
			return fmt.Errorf("unknown font %q", k)
		}
		for _, name := range []string{f.File, f.Italic} {
			if name == "" {
				continue
			}
			data, err := fonts.ReadFile("fonts/" + name)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// Font returns the bytes of an embedded font, for measuring text.
func Font(key string) ([]byte, error) {
	f, ok := spec.FontByKey(key)
	if !ok {
		return nil, fmt.Errorf("unknown font %q", key)
	}
	return fonts.ReadFile("fonts/" + f.File)
}
