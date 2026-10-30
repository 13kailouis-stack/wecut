package engine

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// The assets package supplies font bytes in the binary; tests read the
// same files from disk (assets imports engine, so it cannot be used here).
func init() {
	FontData = func(key string) ([]byte, error) {
		f, ok := spec.FontByKey(key)
		if !ok {
			return nil, os.ErrNotExist
		}
		return os.ReadFile(filepath.Join("..", "assets", "fonts", f.File))
	}
}

func TestFontMetrics(t *testing.T) {
	for _, f := range spec.Fonts {
		m, err := metricsFor(f.Key)
		if err != nil {
			t.Fatalf("%s: %v", f.Key, err)
		}
		// The Em factor in the catalog is (winAscent+winDescent)/unitsPerEm.
		if em := (m.winAsc + m.winDesc) / m.upm; math.Abs(em-f.Em) > 0.002 {
			t.Errorf("%s: Em %.4f, catalog %.4f", f.Key, em, f.Em)
		}
		if m.capH < 0.4*m.upm || m.capH > 0.9*m.upm {
			t.Errorf("%s: cap height %.0f of %.0f", f.Key, m.capH, m.upm)
		}
		if math.Abs(m.capH/m.upm-f.Cap) > 0.002 {
			t.Errorf("%s: cap %.4f, catalog %.4f", f.Key, m.capH/m.upm, f.Cap)
		}
		if f.Italic != "" {
			if _, err := os.Stat(filepath.Join("..", "assets", "fonts", f.Italic)); err != nil {
				t.Errorf("%s: italic file: %v", f.Key, err)
			}
		}
		if w := m.width("Hello", 100, 0); w < 150 || w > 500 { // wide display faces reach 450
			t.Errorf("%s: Hello is %.0f px at 100", f.Key, w)
		}
	}
	m, _ := metricsFor("poppins")
	text := "Tolak Ngantor di Jakarta, Ambisi Bos Ini Bangun"
	lines := m.wrap(text, 50, 0, 600)
	if len(lines) < 2 || strings.Join(lines, " ") != text {
		t.Errorf("wrap: %q", lines)
	}
	for _, l := range lines {
		if m.width(l, 50, 0) > 600 {
			t.Errorf("line %q is wider than 600 px", l)
		}
	}
	if got := m.wrap("one\ntwo", 50, 0, 600); len(got) != 2 {
		t.Errorf("explicit break: %q", got)
	}
}
