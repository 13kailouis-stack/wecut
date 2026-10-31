package recipe

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// HasBeatGrid reports whether a recipe stored its cuts on a beat clock.
// recipe apply only retimes a recipe that has one, and only when the new
// music brings a tempo of its own.
func HasBeatGrid(r *Recipe) bool {
	if r == nil || r.Tempo == nil || r.Tempo.BPM < 30 {
		return false
	}
	for _, s := range r.Sections {
		for _, sh := range s.Shots {
			if sh.Beat != nil {
				return true
			}
		}
	}
	return false
}

// snapBeats retimes a recipe so its cuts and text slots keep the same beat
// indexes on a replacement track. Seconds are scaled by oldBPM/newBPM, which
// is the same as moving each stored beat index onto the new clock while the
// video still starts at 0. The new track is then shifted so its beat 0 is
// heard where the original's beat 0 now sits.
//
// changed is false when nothing should move: no replacement music, no grid,
// or the two tempos are too far apart to still be the same edit. A note is
// set whenever the caller should say what happened.
func snapBeats(r *Recipe, in Input) (out *Recipe, note string, changed bool) {
	if in.Music == "" || in.Music == "none" || in.MusicBPM < 30 || !HasBeatGrid(r) {
		return r, "", false
	}
	ratio := r.Tempo.BPM / in.MusicBPM
	if ratio < 0.5 || ratio > 2 {
		return r, fmt.Sprintf("beat sync left off: %.0f BPM against the template's %.0f BPM would stretch the edit too far", in.MusicBPM, r.Tempo.BPM), false
	}
	cp := cloneRecipe(r)
	if math.Abs(ratio-1) > 0.01 {
		scaleRecipe(cp, ratio)
		for _, s := range cp.Sections {
			for _, sh := range s.Shots {
				if sh.Dur < 0.15 || sh.Dur > spec.MaxDuration {
					return r, fmt.Sprintf("beat sync left off: at %.0f BPM a shot would be %.2f s", in.MusicBPM, sh.Dur), false
				}
			}
		}
	}
	phase := r.Tempo.Offset*ratio - in.MusicOffset
	if cp.Music == nil {
		cp.Music = &Music{}
	}
	// The original From is a point in the old song. The replacement starts
	// at the phase that lines its beats up with the scaled edit.
	switch {
	case phase > 0.02:
		cp.Music.At = round3(phase)
		cp.Music.From = 0
	case phase < -0.02:
		cp.Music.At = 0
		cp.Music.From = round3(-phase)
	default:
		cp.Music.At = 0
		cp.Music.From = 0
	}
	if cp.Music.Duration != nil && math.Abs(ratio-1) > 0.01 {
		d := round3(*cp.Music.Duration * ratio)
		cp.Music.Duration = &d
	}
	if math.Abs(ratio-1) <= 0.01 && math.Abs(phase) <= 0.02 {
		return r, "", false
	}
	note = fmt.Sprintf("cuts and text slots snapped from %.0f BPM to the new music at %.0f BPM", r.Tempo.BPM, in.MusicBPM)
	if math.Abs(phase) > 0.02 {
		note += fmt.Sprintf("; the track is shifted %.2f s so the beats line up", math.Abs(phase))
	}
	return cp, note, true
}

func scaleRecipe(r *Recipe, ratio float64) {
	if r.MaxDuration > 0 {
		r.MaxDuration = math.Min(spec.MaxDuration, math.Max(3, math.Ceil(r.MaxDuration*ratio)))
	}
	for i := range r.Sections {
		s := &r.Sections[i]
		if s.Duration > 0 {
			s.Duration = round3(s.Duration * ratio)
		}
		if s.Shot > 0 {
			s.Shot = round3(s.Shot * ratio)
		}
		scaleTrans(s.Join, ratio)
		scaleTrans(s.TransitionOut, ratio)
		for j := range s.Shots {
			sh := &s.Shots[j]
			sh.Dur = round3(sh.Dur * ratio)
			scaleTrans(sh.Join, ratio)
		}
		for j := range s.Texts {
			scaleSlot(&s.Texts[j], ratio)
		}
	}
	for i := range r.Texts {
		scaleSlot(&r.Texts[i], ratio)
	}
}

func scaleSlot(s *Slot, ratio float64) {
	s.Start = round3(s.Start * ratio)
	if s.End != nil {
		e := round3(*s.End * ratio)
		s.End = &e
	}
	if s.FromEnd != nil {
		e := round3(*s.FromEnd * ratio)
		s.FromEnd = &e
	}
}

func scaleTrans(t *spec.Transition, ratio float64) {
	if t == nil || t.Duration == 0 {
		return
	}
	d := t.Duration * ratio
	if d < 0.1 {
		d = 0.1
	}
	if d > 3 {
		d = 3
	}
	t.Duration = round3(d)
}

func cloneRecipe(r *Recipe) *Recipe {
	b, err := json.Marshal(r)
	if err != nil {
		return r
	}
	var out Recipe
	if json.Unmarshal(b, &out) != nil {
		return r
	}
	return &out
}
