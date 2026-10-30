package recipe

import (
	"fmt"
	"math"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// Fill modes for sections with exact shots.
var Fills = []string{"auto", "each", "sequence"}

// fillShots fills a section's exact shot list, the way a CapCut template
// fills its slots:
//
//   - each: one clip per slot, in order. A slot takes the start of its clip
//     (or the next seconds of it when the clips run out and wrap around).
//   - sequence: the clips play one after another, each shot taking the next
//     seconds of the current clip, so one long take can feed every slot.
//   - auto (default): each when there are at least as many clips as slots,
//     otherwise sequence.
//
// A shot marked continue always takes the seconds right after the shot
// before it, from the same clip. Footage that runs out wraps around to the
// start, so every slot gets a picture.
func fillShots(s Section, si int, q []*piece, mode string, warn func(path, msg string)) ([]built, float64) {
	path := fmt.Sprintf("/sections/%d", si)
	if len(q) == 0 {
		return nil, 0
	}
	slots := 0
	for _, sh := range s.Shots {
		if !sh.Continue {
			slots++
		}
	}
	if mode == "" || mode == "auto" {
		mode = "sequence"
		if len(q) >= slots && slots > 1 {
			mode = "each"
		}
	}
	if mode == "each" && len(q) > slots {
		warn(path, fmt.Sprintf("section %s has %d slots; %d of the clips were not used", s.Role, slots, len(q)-slots))
	}
	starts := make([]float64, len(q))
	for i, pc := range q {
		starts[i] = pc.from
	}
	left := func(pc *piece) float64 {
		if pc.kind == "image" {
			if pc.to < 0 {
				return 0
			}
			return math.Inf(1)
		}
		return pc.to - pc.from
	}
	rewind := func(i int) {
		q[i].from = starts[i]
		if q[i].kind == "image" {
			q[i].to = 0
		}
	}
	wrapped, short := false, 0
	var got []built
	var prev *piece
	cur, slot := 0, 0
	filled := 0.0
	for k, sh := range s.Shots {
		need := sh.Dur
		var pc *piece
		switch {
		case sh.Continue && prev != nil && prev.kind == "video" && left(prev) >= math.Min(need, 0.3):
			pc = prev
		case mode == "each":
			i := slot % len(q)
			if slot >= len(q) {
				wrapped = true
			}
			slot++
			pc = q[i]
			if left(pc) < math.Min(need, 0.3) || (pc.kind == "image" && slot > len(q)) {
				rewind(i)
			}
		default:
			// The next clip with footage left; a clip with only a sliver
			// left (under 60% of the shot) is passed over.
			for tries := 0; ; tries++ {
				if cur >= len(q) {
					cur = 0
					wrapped = true
					for i := range q {
						rewind(i)
					}
				}
				if l := left(q[cur]); l >= 0.3 && (l >= need*0.6 || tries >= 2*len(q)) {
					break
				}
				cur++
			}
			pc = q[cur]
		}
		b := built{section: si, join: sh.Join}
		c := &b.clip
		c.Src, c.Focus = pc.src, pc.focus
		if pc.kind == "image" {
			c.Duration = spec.F(round3(need))
			b.dur = need
			pc.to = -1
			if mode != "each" && !sh.Continue {
				cur++
			}
		} else {
			take := math.Min(need, left(pc))
			if take < need-0.05 {
				short++
			}
			c.From, c.To = round3(pc.from), spec.F(round3(pc.from+take))
			b.dur = take
			pc.from += take
		}
		c.Fit = s.Fit
		c.Inset = sh.Inset
		if sh.Fit != "" {
			c.Fit = sh.Fit
		}
		if c.Fit != "contain" {
			if sh.Zoom > 1 {
				c.Zoom = sh.Zoom
			}
			if sh.ZoomTo >= 1 && sh.ZoomTo != math.Max(1, sh.Zoom) {
				c.ZoomTo = sh.ZoomTo
			}
		}
		if sh.Focus != nil {
			c.Focus = sh.Focus
		}
		if s.Mute || s.Audio == "next" {
			c.Mute = true
		}
		if pc.kind == "image" && c.ZoomTo == 0 && s.Motion != "" && c.Fit != "contain" {
			c.Motion = s.Motion
			if s.Motion == "alternate" {
				c.Motion = []string{"in", "left", "out", "right"}[k%4]
			}
		}
		got = append(got, b)
		filled += b.dur
		prev = pc
	}
	if wrapped {
		warn(path, fmt.Sprintf("section %s has more slots than footage, so some clips play twice; pass more clips to fill all %d slots", s.Role, slots))
	}
	if short > 0 {
		warn(path, fmt.Sprintf("%d shots of section %s got less footage than their slot; the video is a little shorter than the template", short, s.Role))
	}
	return got, filled
}

// leadAudio is the sound that plays under a section whose audio is "next":
// the opening seconds of the next section's first clip, which then starts
// that much later so the talk runs on without a repeat.
type leadAudio struct {
	section int
	src     string
	from    float64
	dur     float64
}

// sectionLength is how long built clips last on screen once the joins
// inside them overlap, before any transition out.
func sectionLength(got []built) float64 {
	d := 0.0
	for k, b := range got {
		d += b.dur
		if k+1 < len(got) && b.join != nil {
			d -= math.Min(joinDur(b.join), math.Min(b.dur, got[k+1].dur)-0.1)
		}
	}
	return d
}

func joinDur(t *spec.Transition) float64 {
	if t.Duration == 0 {
		return 0.5
	}
	return t.Duration
}

// dealClips shares clips without roles among the sections of an exact-shot
// template, in order: each section takes as many as it has media slots, and
// keeps enough back that every later section gets one. A hook and a talk
// passed as two clips land one in each, with no roles to write.
func dealClips(r *Recipe, in Input, queues map[string][]*piece, def string) {
	n := len(r.Sections)
	q := queues[def]
	if in.Fill == "sequence" || n < 2 || len(q) < n {
		return
	}
	for _, c := range in.Clips {
		if c.Role != "" {
			return
		}
	}
	for _, s := range r.Sections {
		if len(s.Shots) == 0 {
			return
		}
	}
	delete(queues, def)
	for si, s := range r.Sections {
		slots := 0
		for _, sh := range s.Shots {
			if !sh.Continue {
				slots++
			}
		}
		take := min(slots, len(q)-(n-si-1))
		if si == n-1 {
			take = len(q)
		}
		queues[s.Role] = append(queues[s.Role], q[:take]...)
		q = q[take:]
	}
}
