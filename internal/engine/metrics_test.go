package engine

import (
	"math"
	"strings"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

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
		if m.capH < 0.5*m.upm || m.capH > 0.9*m.upm {
			t.Errorf("%s: cap height %.0f of %.0f", f.Key, m.capH, m.upm)
		}
		if w := m.width("Hello", 100, 0); w < 150 || w > 400 {
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
