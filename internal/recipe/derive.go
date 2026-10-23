package recipe

import (
	"fmt"
	"math"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// FromEdit turns a finished project into a recipe: the "save as template"
// of an editor. Runs of clips from the same file with the same fit become a
// section; texts become slots (their words kept as defaults); captions,
// music settings, effects and filters keep their style but lose their
// files. The footage, transcript and music themselves stay behind.
func FromEdit(e *engine.Edit, name string) *Recipe {
	p := e.P
	r := &Recipe{Recipe: Version, Name: name, MaxDuration: math.Ceil(e.Total)}
	r.Output = &Output{Ratio: p.Output.Ratio, Res: p.Output.Res, FPS: p.Output.FPS, Background: p.Output.Background, Blur: p.Output.Blur}

	type group struct {
		start, end float64
		clips      []engine.ClipT
	}
	var groups []*group
	for _, c := range e.Clips {
		fit := clipFit(c.Clip, e.Fit)
		if n := len(groups); n > 0 {
			last := groups[n-1].clips[len(groups[n-1].clips)-1]
			if last.Src == c.Src && clipFit(last.Clip, e.Fit) == fit && last.Trans == "" {
				g := groups[n-1]
				g.clips = append(g.clips, c)
				g.end = c.Offset + c.Dur
				continue
			}
		}
		groups = append(groups, &group{start: c.Offset, end: c.Offset + c.Dur, clips: []engine.ClipT{c}})
	}

	for gi, g := range groups {
		// A short opening run is the hook; everything after is body, body2...
		role := "body"
		if gi == 0 && len(groups) > 1 && g.end-g.start <= 10 {
			role = "hook"
		}
		s := Section{Role: uniqueRole(r, role), Fit: clipFit(g.clips[0].Clip, e.Fit)}
		if gi < len(groups)-1 {
			s.Duration = round3(g.end - g.start)
		}
		if len(g.clips) > 1 {
			var sum, zoom float64
			for _, c := range g.clips {
				sum += c.Dur
				zoom = math.Max(zoom, c.Clip.Zoom)
			}
			s.Shot = round3(sum / float64(len(g.clips)))
			if zoom > 1 {
				s.Zoom = zoom
			}
		}
		first, last := g.clips[0], g.clips[len(g.clips)-1]
		if first.Kind == "image" && first.Clip.Motion != "" {
			s.Motion = first.Clip.Motion
			if len(g.clips) > 1 && g.clips[1].Clip.Motion != first.Clip.Motion {
				s.Motion = "alternate"
			}
		}
		s.FadeIn = first.Clip.FadeIn
		muted := true
		for _, c := range g.clips {
			muted = muted && (c.Clip.Mute || (c.Clip.Volume != nil && *c.Clip.Volume == 0))
		}
		s.Mute = muted && first.Kind == "video"
		if last.Trans != "" && gi < len(groups)-1 {
			s.TransitionOut = &spec.Transition{Type: last.Clip.Transition.Type, Duration: last.TransDur}
		}
		r.Sections = append(r.Sections, s)
	}

	n := 0
	taken := map[string]bool{}
	for _, t := range p.Texts {
		end := e.Total
		if t.End != nil {
			end = math.Min(*t.End, e.Total)
		}
		n++
		// Slots are named after their style (label, headline, hook...) when
		// that is free, so the names say what goes in them.
		name := t.Style
		if name == "" || taken[name] {
			name = fmt.Sprintf("text%d", n)
		}
		sl := Slot{Slot: name, Text: t.Text, X: t.X, Y: t.Y, Rotate: t.Rotate, In: t.In, Out: t.Out, EmphasisColor: t.EmphasisColor, Look: t.Look}
		// Texts running to the end are kept relative to the end (a call to
		// action stays last whatever the length).
		if end >= e.Total-0.05 && t.Start > e.Total/2 {
			if !taken["cta"] {
				sl.Slot = "cta"
			}
			taken[sl.Slot] = true
			sl.FromEnd = spec.F(round3(e.Total - t.Start))
			r.Texts = append(r.Texts, sl)
			continue
		}
		gi := 0
		for i, g := range groups {
			if t.Start >= g.start-1e-3 {
				gi = i
			}
		}
		taken[sl.Slot] = true
		g := groups[gi]
		sl.Start = round3(t.Start - g.start)
		if end < g.end-0.05 {
			sl.End = spec.F(round3(end - g.start))
		}
		r.Sections[gi].Texts = append(r.Sections[gi].Texts, sl)
	}

	if c := p.Captions; c != nil {
		r.Captions = &CaptionStyle{Mode: c.Mode, Highlight: c.Highlight, MaxWords: c.MaxWords, MaxDuration: c.MaxDuration, X: c.X, Y: c.Y, Look: c.Look}
	}
	if len(p.Audio) > 0 {
		a := p.Audio[0]
		// The template keeps its song, like a CapCut template does.
		r.Music = &Music{Src: a.Src, From: a.From, Volume: a.Volume, Duck: a.Duck, Loop: a.Loop, FadeIn: a.FadeIn, FadeOut: a.FadeOut}
	}

	// Effects that sit on cuts become "on cuts" rules; others keep their time.
	var cuts []float64
	for _, c := range e.Clips[1:] {
		cuts = append(cuts, c.Offset)
	}
	seen := map[string]bool{}
	for _, ef := range p.Effects {
		onCut := false
		for _, c := range cuts {
			onCut = onCut || math.Abs(c-ef.Start) < 0.1
		}
		switch {
		case onCut && !seen[ef.Name]:
			hits := 0
			for _, other := range p.Effects {
				if other.Name == ef.Name {
					hits++
				}
			}
			every := 1
			if hits > 0 && len(cuts) > hits {
				every = int(math.Round(float64(len(cuts)) / float64(hits)))
			}
			r.Effects = append(r.Effects, EffectRule{Name: ef.Name, On: "cuts", Every: every})
			seen[ef.Name] = true
		case !onCut:
			at := ef.Start
			if ef.Start > e.Total/2 {
				at = round3(ef.Start - e.Total)
			}
			r.Effects = append(r.Effects, EffectRule{Name: ef.Name, On: "time", At: at})
		}
	}
	r.Filters = append(r.Filters, p.Filters...)
	return r
}

func uniqueRole(r *Recipe, role string) string {
	taken := map[string]bool{}
	for _, s := range r.Sections {
		taken[s.Role] = true
	}
	if !taken[role] {
		return role
	}
	for i := 2; ; i++ {
		if c := fmt.Sprintf("%s%d", role, i); !taken[c] {
			return c
		}
	}
}

func clipFit(c *spec.Clip, def string) string {
	if c != nil && c.Fit != "" {
		return c.Fit
	}
	return def
}
