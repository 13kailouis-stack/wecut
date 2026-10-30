package clone

import (
	"testing"

	_ "github.com/13kailouis-stack/wecut/internal/assets"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

func TestChooseFont(t *testing.T) {
	// Thin condensed capitals with no words: the weight alone decides.
	if got := chooseFont(&Look{Weight: 0.204}, nil, 0, 1080, true); got != "bebas" {
		t.Errorf("condensed caps: got %q, want bebas", got)
	}
	// A weight no font comes near (glow, outline) keeps the default.
	if got := chooseFont(&Look{Weight: 0.06}, nil, 0, 1080, true); got != "" {
		t.Errorf("unreliable weight: got %q, want none", got)
	}
	// Without words or capitals, nor does text of unknown case.
	if got := chooseFont(&Look{Weight: 0.200}, nil, 0, 1080, false); got == "bebas" {
		t.Errorf("unknown case picked caps-only bebas")
	}
	// Lowercase words never get a font without lowercase.
	if got := chooseFont(&Look{Weight: 0.200}, []string{"tolak ngantor"}, 0, 1080, false); got == "bebas" {
		t.Errorf("lowercase picked caps-only bebas")
	}
	// Lines set in Poppins SemiBold: their widths pick it out of the family.
	lines := []string{"Tolak Ngantor di Jakarta,", "Ambisi Bos Ini Bangun"}
	f, _ := spec.FontByKey("poppins-semibold")
	capPx, W := 40.0, 1080
	l := &Look{Weight: 0.199}
	for _, ln := range lines {
		w := engine.TextWidth(f.Key, ln, capPx/f.Cap)
		l.Left = append(l.Left, 0.1)
		l.Right = append(l.Right, 0.1+w/float64(W))
	}
	if got := chooseFont(l, lines, capPx, W, false); got != "poppins-semibold" {
		t.Errorf("widths: got %q, want poppins-semibold", got)
	}
	// A slanted news card measured a little light, as compressed video is,
	// with Roboto's widths: the widths win over a real italic face.
	r, _ := spec.FontByKey("roboto")
	l = &Look{Weight: 0.188, Italic: true}
	for _, ln := range lines {
		w := engine.TextWidth(r.Key, ln, capPx/r.Cap)
		l.Left = append(l.Left, 0.1)
		l.Right = append(l.Right, 0.1+w/float64(W))
	}
	if got := chooseFont(l, lines, capPx, W, false); got != "roboto" {
		t.Errorf("italic widths: got %q, want roboto", got)
	}
}
