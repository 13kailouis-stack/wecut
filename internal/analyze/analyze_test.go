package analyze

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestShotsAndPace(t *testing.T) {
	shots := Shots([]float64{2.4, 1.2, 1.21, 3.6, 9.6}, 10.8)
	want := []Shot{{0, 1.2, 1.2}, {1.2, 2.4, 1.2}, {2.4, 3.6, 1.2}, {3.6, 9.6, 6}, {9.6, 10.8, 1.2}}
	if !reflect.DeepEqual(shots, want) {
		t.Fatalf("shots %v", shots)
	}
	p := Measure(shots, 10.8)
	if p.Shots != 5 || p.HookCuts != 2 || p.MedianShot != 1.2 || p.Longest != 6 || p.Feel != "fast cutting" {
		t.Fatalf("pace %+v", p)
	}
	if one := Shots(nil, 5); len(one) != 1 || one[0].Dur != 5 {
		t.Fatalf("no cuts: %v", one)
	}
}

// synth makes a kick drum at bpm, with an optional quiet gap.
func synth(rate int, seconds, bpm float64, gapFrom, gapTo float64) []int16 {
	n := int(seconds * float64(rate))
	out := make([]int16, n)
	period := 60 / bpm
	rng := rand.New(rand.NewSource(1))
	for i := range out {
		t := float64(i) / float64(rate)
		if t >= gapFrom && t < gapTo {
			continue
		}
		ph := math.Mod(t, period)
		// kick + a quiet sustained pad, so only the gap is silent
		v := 0.8*math.Sin(2*math.Pi*55*t)*math.Exp(-25*ph) + 0.03*math.Sin(2*math.Pi*220*t) + 0.01*(rng.Float64()-0.5)
		out[i] = int16(v * 32767)
	}
	return out
}

func TestTempo(t *testing.T) {
	for _, bpm := range []float64{90, 120, 150} {
		a := Sound(synth(11025, 12, bpm, -1, -1), 11025)
		if math.Abs(a.BPM-bpm) > 2 {
			t.Errorf("%v BPM detected as %v (conf %v)", bpm, a.BPM, a.BPMConf)
		}
		if len(a.Beats) < 10 || math.Abs(a.Beats[1]-a.Beats[0]-60/bpm) > 0.03 {
			t.Errorf("%v BPM beats %v", bpm, a.Beats[:3])
		}
	}
}

// Strong kicks every half second with soft ticks between them: the beat is
// the kick (120), not the tick pulse.
func TestTempoFollowsAccents(t *testing.T) {
	rate := 11025
	pcm := make([]int16, 12*rate)
	for i := range pcm {
		tm := float64(i) / float64(rate)
		v := 0.9*math.Sin(2*math.Pi*55*tm)*math.Exp(-25*math.Mod(tm, 0.5)) +
			0.15*math.Sin(2*math.Pi*440*tm)*math.Exp(-8*math.Mod(tm+0.25, 0.5))
		pcm[i] = int16(v * 32000)
	}
	if a := Sound(pcm, rate); math.Abs(a.BPM-120) > 2 {
		t.Fatalf("detected %v BPM (conf %v), want 120", a.BPM, a.BPMConf)
	}
}

func TestSilences(t *testing.T) {
	a := Sound(synth(11025, 6, 120, 2, 3.5), 11025)
	if len(a.Silences) != 1 || math.Abs(a.Silences[0].Start-2) > 0.06 || math.Abs(a.Silences[0].End-3.5) > 0.06 {
		t.Fatalf("silences %v", a.Silences)
	}
}

func TestBursts(t *testing.T) {
	// a flash at 4.9-5.17 (three detections), real cuts at 2 and 8.3
	clean, bursts := Bursts([]float64{8.3, 2, 4.9, 5.0, 5.167}, 0.3)
	if !reflect.DeepEqual(clean, []float64{2, 5.167, 8.3}) || !reflect.DeepEqual(bursts, []Span{{4.9, 5.17}}) {
		t.Fatalf("clean %v bursts %v", clean, bursts)
	}
}

func TestGridIndex(t *testing.T) {
	a := Sound(synth(11025, 8, 120, -1, -1), 11025)
	g, ok := a.Grid()
	if !ok || math.Abs(g.BPM-120) > 2 {
		t.Fatalf("grid %+v ok %v", g, ok)
	}
	// A time on a later beat is a whole number of beats from beat 0.
	at := g.Offset + 4*g.Period()
	if math.Abs(g.SnapIndex(at, 0.08)-4) > 0.01 {
		t.Fatalf("index %v at %v (offset %v)", g.Index(at), at, g.Offset)
	}
	if _, ok := (Audio{}).Grid(); ok {
		t.Fatal("silence has no grid")
	}
}

func TestBeatSync(t *testing.T) {
	beats := []float64{0, 0.5, 1, 1.5, 2, 2.5}
	if got := BeatSync([]float64{0.52, 1.2, 2.04}, beats, 0.08); got != 0.67 {
		t.Fatalf("got %v", got)
	}
}

func TestSheetTimes(t *testing.T) {
	shots := Shots([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9}, 10)
	got := SheetTimes(shots, 4)
	if len(got) != 4 || got[0] != 0.1 || got[1] != 0.5 || got[3] != 9.5 {
		t.Fatalf("times %v", got)
	}
	if got := SheetTimes(shots[:2], 12); len(got) != 3 {
		t.Fatalf("few shots: %v", got)
	}
}

func TestRatio(t *testing.T) {
	cases := map[[2]int]string{{1080, 1920}: "9:16", {720, 1280}: "9:16", {1920, 1080}: "16:9", {1080, 1350}: "4:5", {1000, 3000}: "other"}
	for wh, want := range cases {
		if got := Ratio(wh[0], wh[1]); got != want {
			t.Errorf("%v: %s", wh, got)
		}
	}
}
