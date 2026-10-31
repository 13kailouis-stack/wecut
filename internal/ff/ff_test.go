package ff

import (
	"reflect"
	"strings"
	"testing"
)

func TestFailureLinesPreservesFilterCause(t *testing.T) {
	log := "[Parsed_xfade_100] The inputs needs to be a constant frame rate; current rate of 1/0 is invalid\n[Parsed_xfade_100] Failed to configure output pad\n" + strings.Repeat("[aost] Terminating thread\n[vost] Could not open encoder before EOF\n", 8)
	got := failureLines(log)
	if !strings.Contains(got, "current rate of 1/0") || !strings.Contains(got, "Could not open encoder") {
		t.Fatal(got)
	}
	if got := failureLines("signal: killed"); got != "signal: killed" {
		t.Fatal(got)
	}
}

func TestGraphFilters(t *testing.T) {
	graph := "[0:v]setpts=PTS-STARTPTS,scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920:(iw-1080)*0.5:(ih-1920)*0.5[v0];\n" +
		"anullsrc=r=48000:cl=stereo,atrim=duration=3[a0];\n" +
		"[v0]lutyuv=y='val*0.9+1':enable='between(t,0,2)',ass=f=wecut.ass:fontsdir=fonts,format=yuv420p[vout];\n" +
		"[a0][t0]amix=inputs=2,alimiter=limit=0.97:level=0[aout]\n"
	want := []string{"setpts", "scale", "crop", "anullsrc", "atrim", "lutyuv", "ass", "format", "amix", "alimiter"}
	if got := GraphFilters(graph); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestMissingFor(t *testing.T) {
	c := &Caps{all: map[string]bool{"scale": true, "format": true}}
	if got := c.MissingFor("[0:v]scale=2:2,ass=f=a.ass,format=yuv420p,ass=f=b.ass[o]"); !reflect.DeepEqual(got, []string{"ass"}) {
		t.Fatalf("got %v", got)
	}
	if got := Explain([]string{"ass", "foo"}); got != "ass (text and captions), foo" {
		t.Fatalf("explain: %q", got)
	}
}
