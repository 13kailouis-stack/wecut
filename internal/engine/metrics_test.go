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

// Under every text, with no words in the graph: thin rectangles rising in
// opacity from the start of the fade, then one solid block to the frame's edge.
func TestGradientsInASS(t *testing.T) {
	end := 2.0
	opacity := 50.0
	e := &Edit{W: 1080, H: 1920, Total: 4, P: &spec.Project{
		Texts:     []spec.Text{{Text: "hello"}},
		Gradients: []spec.Gradient{{From: 0.5, To: 0.75, End: &end, Color: "#102030", Opacity: &opacity}, {From: 0.2, To: 0}},
	}}
	ass, _ := buildASS(e)
	first := strings.Index(ass, `\p1`)
	if first < 0 || first > strings.Index(ass, "hello") {
		t.Fatalf("gradient not under the text:\n%s", ass)
	}
	if !strings.Contains(ass, `\pos(0,1440)`) || !strings.Contains(ass, `m 0 0 l 1080 0 1080 480 0 480`) {
		t.Errorf("no solid block from the end of the fade to the bottom:\n%s", ass)
	}
	if !strings.Contains(ass, `\1c&H302010&`) || !strings.Contains(ass, "0:00:02.00") {
		t.Errorf("colour or end time lost:\n%s", ass)
	}
	if !strings.Contains(ass, `\pos(0,0)`) {
		t.Errorf("no block for the fade at the top:\n%s", ass)
	}
}

// Words light up and stay lit in reveal, and the key word is set apart.
func TestCaptionRevealAndAccent(t *testing.T) {
	e := &Edit{W: 1080, H: 1920, Total: 3, P: &spec.Project{Captions: &spec.Captions{
		Mode: "reveal", Highlight: "#FFFFFF", MaxWords: 3,
		Accent: &spec.Accent{Color: "#FFD60A", Scale: 130, Italic: true, Font: "kanit-black"},
		Look:   spec.Look{Color: "#808080"},
	}}, Words: []Word{{Text: "jual", Start: 0, End: 0.5}, {Text: "tempat", Start: 0.5, End: 1}, {Text: "lu", Start: 1, End: 1.5}}}
	ass, fonts := buildASS(e)
	var lines []string
	for _, l := range strings.Split(ass, "\n") {
		if strings.HasPrefix(l, "Dialogue:") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("%d events:\n%s", len(lines), ass)
	}
	white, yellow := assBGR("#FFFFFF"), assBGR("#FFD60A")
	// First event: only the first word is lit; the key word is big but not yet yellow.
	if !strings.Contains(lines[0], `\1c`+white+`}jual`) || strings.Contains(lines[0], yellow) || !strings.Contains(lines[0], `\fscx130`) {
		t.Errorf("first: %s", lines[0])
	}
	// Second: the key word turns its own colour, the first word stays lit.
	if !strings.Contains(lines[1], `\1c`+white+`}jual`) || !strings.Contains(lines[1], `\1c`+yellow+`}tempat`) || !strings.Contains(lines[1], `\i1`) {
		t.Errorf("second: %s", lines[1])
	}
	if !slicesContains(fonts, "kanit-black") {
		t.Errorf("accent font not bundled: %v", fonts)
	}
	// Named words win over the longest word.
	e.P.Captions.Accent.Words = []string{"LU!"}
	ass, _ = buildASS(e)
	if strings.Contains(ass, yellow+`}tempat`) || !strings.Contains(ass, yellow+`}lu`) {
		t.Errorf("named word:\n%s", ass)
	}
}

func TestInsetCornersInASS(t *testing.T) {
	e := &Edit{W: 1080, H: 1920, Total: 5, P: &spec.Project{}, Clips: []ClipT{
		{Dur: 2, Clip: &spec.Clip{}},
		{Offset: 2, Dur: 3, Clip: &spec.Clip{Inset: &spec.Inset{X: 0.1, Y: 0.25, W: 0.8, H: 0.25, Radius: 24, Background: "#101010"}}},
	}}
	ass, _ := buildASS(e)
	if got := strings.Count(ass, `\p1`); got != 4 {
		t.Fatalf("%d corners:\n%s", got, ass)
	}
	if !strings.Contains(ass, `\pos(108,480)`) || !strings.Contains(ass, `\pos(948,936)`) || !strings.Contains(ass, "0:00:02.00") {
		t.Errorf("corners misplaced:\n%s", ass)
	}
}

func slicesContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
