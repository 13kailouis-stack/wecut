package clone

import (
	"math"

	"github.com/13kailouis-stack/wecut/internal/analyze"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// stampBeats records each shot and text slot as a beat index of the
// source's tempo, beside the seconds already stored. recipe apply uses
// the grid when the music is replaced. A quiet or unsteady soundtrack
// leaves the seconds on their own.
func stampBeats(r *recipe.Recipe, p *Plan) {
	if p.Audio == nil {
		return
	}
	g, ok := p.Audio.Grid()
	if !ok {
		return
	}
	r.Tempo = &recipe.Tempo{BPM: g.BPM, Offset: r3(g.Offset)}
	if r.Music != nil && r.Music.BPM == 0 {
		r.Music.BPM = g.BPM
	}
	si := 0
	for i := range r.Sections {
		for j := range r.Sections[i].Shots {
			if si >= len(p.Shots) {
				break
			}
			idx := roundBeat(g.SnapIndex(p.Shots[si].Start, 0.08))
			r.Sections[i].Shots[j].Beat = spec.F(idx)
			si++
		}
	}
	for i := range r.Sections {
		for j := range r.Sections[i].Texts {
			stampSlotBeats(&r.Sections[i].Texts[j], g)
		}
	}
	for i := range r.Texts {
		stampSlotBeats(&r.Texts[i], g)
	}
}

func stampSlotBeats(sl *recipe.Slot, g analyze.Grid) {
	sl.StartBeat = spec.F(roundBeat(g.SnapIndex(sl.Start, 0.08)))
	if sl.End != nil {
		sl.EndBeat = spec.F(roundBeat(g.SnapIndex(*sl.End, 0.08)))
	}
}

func roundBeat(v float64) float64 { return math.Round(v*100) / 100 }
