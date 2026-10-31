package clone

import (
	"math"
	"math/rand"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/analyze"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

func TestStampBeats(t *testing.T) {
	rate := 11025
	pcm := make([]int16, 8*rate)
	rng := rand.New(rand.NewSource(1))
	period := 60 / 120.0
	for i := range pcm {
		tm := float64(i) / float64(rate)
		ph := math.Mod(tm, period)
		v := 0.8*math.Sin(2*math.Pi*55*tm)*math.Exp(-25*ph) + 0.02*(rng.Float64()-0.5)
		pcm[i] = int16(v * 32767)
	}
	snd := analyze.Sound(pcm, rate)
	p := &Plan{
		ContentEnd: 4,
		Audio:      &snd,
		Shots: []ShotInfo{
			{Start: 0, End: 1},
			{Start: 1, End: 2},
			{Start: 2, End: 4},
		},
	}
	r := &recipe.Recipe{
		Music: &recipe.Music{},
		Sections: []recipe.Section{{Shots: []recipe.Shot{
			{Dur: 1}, {Dur: 1}, {Dur: 2},
		}}},
		Texts: []recipe.Slot{{Slot: "title", Start: 0, End: spec.F(1)}},
	}
	stampBeats(r, p)
	if r.Tempo == nil || math.Abs(r.Tempo.BPM-120) > 2 {
		t.Fatalf("tempo %+v (audio %+v)", r.Tempo, snd.BPM)
	}
	if r.Music.BPM == 0 || math.Abs(r.Music.BPM-r.Tempo.BPM) > 0.1 {
		t.Fatalf("music bpm %v", r.Music.BPM)
	}
	b1 := r.Sections[0].Shots[1].Beat
	if b1 == nil {
		t.Fatal("shot 1 has no beat")
	}
	// One second later is two beats at 120 BPM, measured from beat 0.
	g, _ := snd.Grid()
	want := roundBeat(g.SnapIndex(1, 0.08))
	if math.Abs(*b1-want) > 0.02 {
		t.Fatalf("shot beat %v, want %v (offset %v)", *b1, want, g.Offset)
	}
	if r.Texts[0].StartBeat == nil || r.Texts[0].EndBeat == nil {
		t.Fatalf("text beats %+v", r.Texts[0])
	}
	// No steady tempo: seconds stay, and no grid is written.
	quiet := &Plan{Audio: &analyze.Audio{}, Shots: p.Shots}
	bare := &recipe.Recipe{Sections: r.Sections}
	stampBeats(bare, quiet)
	if bare.Tempo != nil {
		t.Fatalf("quiet got %+v", bare.Tempo)
	}
}
