package recipe

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// Clip is a piece of footage handed to a recipe. Role says which section it
// feeds; clips without a role feed the "body" section (or the last one).
type Clip struct {
	Src   string      `json:"src"`
	From  float64     `json:"from,omitempty"`
	To    *float64    `json:"to,omitempty"`
	Role  string      `json:"role,omitempty"`
	Focus *spec.Point `json:"focus,omitempty"`
}

type SlotText struct {
	Text     string   `json:"text"`
	Emphasis []string `json:"emphasis,omitempty"`
}

// Input is everything that is new in this video: footage, words, music.
// MusicBPM, when set with a replacement track, retimes a beat-synced recipe.
type Input struct {
	Clips      []Clip              `json:"clips"`
	Texts      map[string]SlotText `json:"texts,omitempty"`
	Transcript string              `json:"transcript,omitempty"` // word timings, in seconds of CaptionSrc
	// CaptionAccent names the key words of the transcript, for a recipe whose
	// captions set them apart (default: the longest word of each phrase).
	CaptionAccent []string `json:"captionAccent,omitempty"`
	CaptionSrc    string   `json:"captionSrc,omitempty"` // default: the source used longest
	Music         string   `json:"music,omitempty"`      // replaces the recipe's track; "none" = no music
	// MusicBPM and MusicOffset are the replacement track's beat grid.
	// MusicOffset is seconds of beat 0 in that file. Zero keeps the
	// template's own seconds.
	MusicBPM    float64 `json:"musicBpm,omitempty"`
	MusicOffset float64 `json:"musicOffset,omitempty"`
	Output      string  `json:"output,omitempty"` // default out/<recipe name>.mp4
	// Overlays gives a picture per overlay slot (e.g. {"logo":
	// "media/logo.png"}); "none" leaves a slot out.
	Overlays map[string]string `json:"overlays,omitempty"`
	// Fill says how clips go into exact shots: each (one clip per slot),
	// sequence (one after another) or auto (default).
	Fill string `json:"fill,omitempty"`
}

type piece struct {
	src   string
	kind  string
	from  float64
	to    float64 // video only
	focus *spec.Point
}

type built struct {
	clip    spec.Clip
	dur     float64
	section int
	join    *spec.Transition // exact shots: into the next shot
}

// Apply builds a project from a recipe and new material. m must hold every
// clip src. It returns the project, warnings (things the recipe wanted but
// the input did not give), and errors.
func Apply(r *Recipe, in Input, m media.Set) (*spec.Project, []spec.Issue, []spec.Issue) {
	var warns, errs []spec.Issue
	warn := func(path, msg string) { warns = append(warns, spec.Issue{Code: "W_RECIPE", Path: path, Message: msg}) }
	fail := func(path, msg, hint string) {
		errs = append(errs, spec.Issue{Code: "E_VALUE", Path: path, Message: msg, Hint: hint})
	}
	if len(in.Clips) == 0 {
		fail("/clips", "give at least one clip", `e.g. {"src": "media/talk.mp4", "from": 12, "to": 70}`)
		return nil, nil, errs
	}
	if in.Fill != "" && !contains(Fills, in.Fill) {
		fail("/fill", "fill must be auto, each or sequence", "")
		return nil, nil, errs
	}

	roles := map[string]bool{}
	for _, s := range r.Sections {
		roles[s.Role] = true
	}
	defaultRole := r.Sections[len(r.Sections)-1].Role
	if roles["body"] {
		defaultRole = "body"
	}
	queues := map[string][]*piece{}
	for i, c := range in.Clips {
		p := fmt.Sprintf("/clips/%d", i)
		info, ok := m[c.Src]
		if !ok {
			fail(p+"/src", "file was not probed: "+c.Src, "")
			continue
		}
		role := c.Role
		if role == "" {
			role = defaultRole
		}
		if !roles[role] {
			fail(p+"/role", fmt.Sprintf("the recipe has no section with role %q", role), "Roles here: "+strings.Join(sectionRoles(r), ", "))
			continue
		}
		pc := &piece{src: c.Src, kind: info.Kind, from: c.From, focus: c.Focus}
		switch info.Kind {
		case "video":
			pc.to = info.Duration
			if c.To != nil {
				pc.to = math.Min(*c.To, info.Duration)
			}
			if pc.to-pc.from < 0.3 {
				fail(p, fmt.Sprintf("clip is shorter than 0.3 s (%s is %.2f s long)", c.Src, info.Duration), "")
				continue
			}
		case "image":
		default:
			fail(p+"/src", c.Src+" is "+info.Kind+", clips need video or an image", "Pass music as \"music\".")
			continue
		}
		queues[role] = append(queues[role], pc)
	}
	if len(errs) > 0 {
		return nil, nil, errs
	}
	beatSnapped := false
	if cp, note, changed := snapBeats(r, in); note != "" {
		warn("/music", note)
		if changed {
			r = cp
			beatSnapped = true
		}
	}
	dealClips(r, in, queues, defaultRole)

	maxTotal := r.MaxDuration
	if maxTotal == 0 {
		maxTotal = 60
	}
	var clips []built
	var leads []leadAudio
	borrowed := map[int]bool{}
	lastGot := map[int][]built{}
	total := 0.0
	for si, s := range r.Sections {
		q := queues[s.Role]
		if len(q) == 0 { // borrow from the next section that has footage
			borrowed[si] = true
			for sj := si + 1; sj < len(r.Sections) && len(q) == 0; sj++ {
				q = queues[r.Sections[sj].Role]
			}
		}
		// The section before plays this one's sound (a J-cut): its first
		// clip lends that many opening seconds and starts later.
		if pg := lastGot[si-1]; si > 0 && r.Sections[si-1].Audio == "next" && len(pg) > 0 && len(q) > 0 {
			prev := r.Sections[si-1]
			lead := sectionLength(pg)
			if t := prev.TransitionOut; t != nil {
				lead -= joinDur(t)
			}
			pc := q[0]
			switch {
			case borrowed[si-1]:
				// It showed this footage itself: keep its own sound.
				for k := range clips {
					if clips[k].section == si-1 {
						clips[k].clip.Mute = false
					}
				}
			case pc.kind != "video" || !m[pc.src].HasAudio:
				warn(fmt.Sprintf("/sections/%d/audio", si-1), fmt.Sprintf("section %s wants the sound of section %s, but its first clip has no sound", prev.Role, s.Role))
			case pc.to-pc.from < lead+0.3:
				warn(fmt.Sprintf("/sections/%d/audio", si-1), fmt.Sprintf("the first %s clip is too short to also play under %s", s.Role, prev.Role))
			case lead > 0.1:
				leads = append(leads, leadAudio{section: si - 1, src: pc.src, from: round3(pc.from), dur: round3(lead)})
				pc.from += lead
			}
		}
		if len(s.Shots) > 0 {
			got, filled := fillShots(s, si, q, in.Fill, warn)
			if len(got) == 0 {
				warn(fmt.Sprintf("/sections/%d", si), "section "+s.Role+" got no footage and was left out")
				continue
			}
			if s.FadeIn > 0 {
				got[0].clip.FadeIn = math.Min(s.FadeIn, got[0].dur/2)
			}
			lastGot[si] = got
			clips = append(clips, got...)
			total += filled
			continue
		}
		want := maxTotal - total
		if s.Duration > 0 {
			want = math.Min(s.Duration, want)
		}
		if want < 0.5 {
			if total >= maxTotal {
				warn(fmt.Sprintf("/sections/%d", si), fmt.Sprintf("section %s left out: the video already reached maxDuration (%.0f s)", s.Role, maxTotal))
			}
			continue
		}
		var got []built
		filled := 0.0
		for _, pc := range q {
			if filled >= want-0.05 {
				break
			}
			room := want - filled
			if pc.kind == "image" {
				if pc.to < 0 { // already used
					continue
				}
				d := s.Shot
				if d == 0 {
					d = 3
				}
				d = math.Min(d, room)
				if d < 0.3 {
					break
				}
				got = append(got, built{clip: spec.Clip{Src: pc.src, Duration: spec.F(round3(d))}, dur: d, section: si})
				pc.to = -1
				filled += d
				continue
			}
			left := pc.to - pc.from
			if left < 0.3 {
				continue
			}
			take := math.Min(left, room)
			for _, part := range split(take, s.Shot) {
				got = append(got, built{clip: spec.Clip{Src: pc.src, From: round3(pc.from), To: spec.F(round3(pc.from + part)), Focus: pc.focus}, dur: part, section: si})
				pc.from += part
			}
			filled += take
		}
		if len(got) == 0 {
			warn(fmt.Sprintf("/sections/%d", si), "section "+s.Role+" got no footage and was left out")
			continue
		}
		for k := range got {
			c := &got[k].clip
			if s.Fit != "" {
				c.Fit = s.Fit
			}
			if s.Zoom > 1 && k%2 == 1 && s.Fit != "contain" {
				c.Zoom = s.Zoom
			}
			if s.Mute || s.Audio == "next" {
				c.Mute = true
			}
			if c.Duration != nil && s.Motion != "" { // image
				c.Motion = s.Motion
				if s.Motion == "alternate" {
					c.Motion = []string{"in", "left", "out", "right"}[k%4]
				}
			}
		}
		if s.FadeIn > 0 {
			got[0].clip.FadeIn = math.Min(s.FadeIn, got[0].dur/2)
		}
		lastGot[si] = got
		clips = append(clips, got...)
		total += filled
	}
	if len(clips) == 0 {
		fail("/clips", "no footage made it into the video", "Check the clips' from/to and roles.")
		return nil, nil, errs
	}

	// Transitions: join between shots of one section, transitionOut into the
	// next section. They shorten the video, so offsets come after.
	for k := 0; k+1 < len(clips); k++ {
		sec := r.Sections[clips[k].section]
		t := sec.TransitionOut
		if clips[k].section == clips[k+1].section {
			t = sec.Join
			if len(sec.Shots) > 0 {
				t = clips[k].join
			}
		}
		if t != nil {
			d := t.Duration
			if d == 0 {
				d = 0.5
			}
			if lim := math.Min(clips[k].dur, clips[k+1].dur) - 0.1; d > lim {
				d = math.Max(0.1, lim)
			}
			clips[k].clip.Transition = &spec.Transition{Type: t.Type, Duration: round3(d)}
		}
	}
	offsets := make([]float64, len(clips))
	secStart := map[int]float64{}
	secEnd := map[int]float64{}
	off := 0.0
	for k, c := range clips {
		offsets[k] = off
		if _, ok := secStart[c.section]; !ok {
			secStart[c.section] = off
		}
		off += c.dur
		secEnd[c.section] = off
		if c.clip.Transition != nil {
			off -= c.clip.Transition.Duration
		}
	}
	total = round3(off)

	p := &spec.Project{Wecut: spec.Version}
	p.Output = spec.Output{File: in.Output}
	if p.Output.File == "" {
		p.Output.File = "out/" + slug(r.Name) + ".mp4"
	}
	if o := r.Output; o != nil {
		p.Output.Ratio, p.Output.Res, p.Output.FPS = o.Ratio, o.Res, o.FPS
		p.Output.Background, p.Output.Blur = o.Background, o.Blur
	}
	for _, c := range clips {
		p.Clips = append(p.Clips, c.clip)
	}

	// Texts.
	place := func(sl Slot, from, to float64, path string) {
		t, ok := in.Texts[sl.Slot]
		words := t.Text
		if !ok || strings.TrimSpace(words) == "" {
			words, t.Emphasis = sl.Text, sl.Emphasis
		}
		if strings.TrimSpace(words) == "" {
			warn(path, fmt.Sprintf("slot %q has no text and was left out; pass texts.%s", sl.Slot, sl.Slot))
			return
		}
		start, end := from+sl.Start, to
		if sl.End != nil {
			end = math.Min(from+*sl.End, to)
		}
		if sl.FromEnd != nil {
			start = math.Max(from, to-*sl.FromEnd)
			end = to
		}
		if end-start < 0.2 {
			warn(path, fmt.Sprintf("slot %q does not fit in its section and was left out", sl.Slot))
			return
		}
		p.Texts = append(p.Texts, spec.Text{
			Text: words, Start: round3(start), End: spec.F(round3(end)), X: sl.X, Y: sl.Y, Rotate: sl.Rotate,
			MaxWidth: sl.MaxWidth, LineHeight: sl.LineHeight, Padding: sl.Padding,
			In: sl.In, Out: sl.Out, Emphasis: t.Emphasis, EmphasisColor: sl.EmphasisColor, Look: sl.Look,
		})
	}
	for si, s := range r.Sections {
		st, ok := secStart[si]
		if !ok {
			continue
		}
		for j, sl := range s.Texts {
			place(sl, st, secEnd[si], fmt.Sprintf("/sections/%d/texts/%d", si, j))
		}
	}
	for j, sl := range r.Texts {
		place(sl, 0, total, fmt.Sprintf("/texts/%d", j))
	}
	for slot := range in.Texts {
		if !hasSlot(r, slot) {
			warn("/texts/"+slot, fmt.Sprintf("the recipe has no slot %q; that text was not used", slot))
		}
	}

	// Captions follow the footage the transcript belongs to.
	if c := r.Captions; c != nil {
		if in.Transcript == "" {
			warn("/captions", "the recipe has captions: pass a transcript (word timings) to get them")
		} else {
			src := in.CaptionSrc
			if src == "" {
				src = longestSource(clips)
			}
			words, _ := json.Marshal(in.Transcript)
			p.Captions = &spec.Captions{Words: words, Src: src, Mode: c.Mode, Highlight: c.Highlight, MaxWords: c.MaxWords, MaxDuration: c.MaxDuration, X: c.X, Y: c.Y, Accent: c.Accent, Look: c.Look}
			if len(in.CaptionAccent) > 0 {
				accent := spec.Accent{}
				if c.Accent != nil {
					accent = *c.Accent
				}
				accent.Words = in.CaptionAccent
				p.Captions.Accent = &accent
			}
		}
	}

	// Sound borrowed from the next section, under the section before it.
	for _, l := range leads {
		st, ok := secStart[l.section]
		if !ok {
			continue
		}
		p.Audio = append(p.Audio, spec.Track{Src: l.src, From: l.from, At: round3(st), Duration: spec.F(l.dur)})
	}

	if src, from := MusicSource(r, in); src != "" {
		tr := spec.Track{Src: src, From: from}
		if mu := r.Music; mu != nil {
			tr.At, tr.Duration = mu.At, mu.Duration
			tr.Volume, tr.Duck, tr.Loop, tr.FadeIn, tr.FadeOut = mu.Volume, mu.Duck, mu.Loop, mu.FadeIn, mu.FadeOut
			// A snapped grid stores the new track's phase on the copy.
			// The recipe's own From is a point in the original song, so
			// it is only used when that song is the one playing.
			if beatSnapped {
				tr.From, tr.At = mu.From, mu.At
			}
		}
		p.Audio = append(p.Audio, tr)
		if in.Music == "" {
			warn("/music", fmt.Sprintf("using the recipe's music %s; pass music to change it, or \"none\" to leave it out", src))
		}
	} else if mu := r.Music; mu != nil && in.Music != "none" {
		note := "the recipe has music settings"
		if mu.BPM > 0 {
			note += fmt.Sprintf(" (about %.0f BPM)", mu.BPM)
		}
		if mu.Note != "" {
			note += "; " + mu.Note
		}
		warn("/music", note+": pass music to mix it in")
	}

	for _, e := range r.Effects {
		var at []float64
		switch e.On {
		case "cuts":
			for k := 1; k < len(clips); k++ {
				at = append(at, offsets[k])
			}
		case "sections":
			for k := 1; k < len(clips); k++ {
				if clips[k].section != clips[k-1].section {
					at = append(at, offsets[k])
				}
			}
		case "time":
			t := e.At
			if t < 0 {
				t += total
			}
			at = append(at, t)
		}
		every := max(1, e.Every)
		for i, t := range at {
			if i%every == every-1 && t >= 0 && t < total-0.1 {
				p.Effects = append(p.Effects, spec.Effect{Name: e.Name, Start: round3(t)})
			}
		}
	}
	p.Filters = append(p.Filters, r.Filters...)

	// Picture slots such as a logo.
	for j, o := range r.Overlays {
		path := fmt.Sprintf("/overlays/%d", j)
		src, given := in.Overlays[o.Slot]
		if !given {
			src = o.Src
		}
		switch {
		case src == "none":
			continue
		case src == "":
			warn(path, fmt.Sprintf("overlay slot %q (%s) has no picture and was left out; pass overlays.%s", o.Slot, orDefault(o.About, "a picture"), o.Slot))
			continue
		}
		if info, ok := m[src]; !ok || info.Kind != "image" {
			fail(path, fmt.Sprintf("overlay %q must be a png, jpg or webp image in the workspace: %s", o.Slot, src), "")
			continue
		}
		start, end := o.Start, total
		if o.End != nil {
			end = math.Min(*o.End, total)
		}
		if o.FromEnd != nil {
			start, end = math.Max(0, total-*o.FromEnd), total
		}
		if end-start < 0.2 {
			warn(path, fmt.Sprintf("overlay slot %q does not fit in the video and was left out", o.Slot))
			continue
		}
		ov := spec.Overlay{Src: src, Start: round3(start), X: o.X, Y: o.Y, Width: o.Width, Opacity: o.Opacity, FadeIn: o.FadeIn, FadeOut: o.FadeOut}
		if end < total-0.01 {
			ov.End = spec.F(round3(end))
		}
		p.Overlays = append(p.Overlays, ov)
	}
	p.Gradients = r.Gradients
	for slot := range in.Overlays {
		if !hasOverlay(r, slot) {
			warn("/overlays/"+slot, fmt.Sprintf("the recipe has no overlay slot %q; that picture was not used", slot))
		}
	}
	if len(errs) > 0 {
		return nil, warns, errs
	}

	if issues := spec.Validate(p); len(issues) > 0 {
		return nil, warns, issues
	}
	return p, warns, nil
}

// MusicSource is the track a recipe will use: the one passed in, else the
// recipe's own, else none. "none" in the input turns music off.
func MusicSource(r *Recipe, in Input) (string, float64) {
	switch {
	case in.Music == "none":
		return "", 0
	case in.Music != "":
		return in.Music, 0
	case r.Music != nil && r.Music.Src != "":
		return r.Music.Src, r.Music.From
	}
	return "", 0
}

// OverlaySources lists the pictures a recipe will lay over the video, so a
// caller can probe them before Apply.
func OverlaySources(r *Recipe, in Input) []string {
	var out []string
	for _, o := range r.Overlays {
		src, given := in.Overlays[o.Slot]
		if !given {
			src = o.Src
		}
		if src != "" && src != "none" {
			out = append(out, src)
		}
	}
	return out
}

func hasOverlay(r *Recipe, slot string) bool {
	for _, o := range r.Overlays {
		if o.Slot == slot {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// split cuts length into shots of about shot seconds (0 = one piece).
func split(length, shot float64) []float64 {
	if shot <= 0 || length < shot*1.5 {
		return []float64{length}
	}
	n := int(math.Round(length / shot))
	out := make([]float64, n)
	for i := range out {
		out[i] = length / float64(n)
	}
	return out
}

func longestSource(clips []built) string {
	by := map[string]float64{}
	for _, c := range clips {
		by[c.clip.Src] += c.dur
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return by[keys[i]] > by[keys[j]] || (by[keys[i]] == by[keys[j]] && keys[i] < keys[j])
	})
	return keys[0]
}

func hasSlot(r *Recipe, name string) bool {
	for _, s := range r.Texts {
		if s.Slot == name {
			return true
		}
	}
	for _, sec := range r.Sections {
		for _, s := range sec.Texts {
			if s.Slot == name {
				return true
			}
		}
	}
	return false
}

func sectionRoles(r *Recipe) []string {
	out := make([]string, len(r.Sections))
	for i, s := range r.Sections {
		out[i] = s.Role
	}
	return out
}

// Slots lists every text slot a recipe has, for `wecut recipe show`.
func Slots(r *Recipe) []string {
	var out []string
	for _, s := range r.Sections {
		for _, t := range s.Texts {
			out = append(out, t.Slot)
		}
	}
	for _, t := range r.Texts {
		out = append(out, t.Slot)
	}
	return out
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "recipe"
	}
	return out
}
