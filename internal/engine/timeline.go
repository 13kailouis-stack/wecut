// Package engine turns a validated project plus probed media into an FFmpeg
// render plan: argument list, filter graph and an .ass subtitle file. It is
// pure: no disk, no processes, so every decision can be unit tested.
package engine

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// ClipT is a clip placed on the output timeline.
type ClipT struct {
	Index    int
	Src      string // as written in the project
	Info     media.Info
	Kind     string  // video or image
	From, To float64 // source range (video)
	Speed    float64
	Dur      float64 // seconds on the output timeline
	Offset   float64 // where it starts on the output timeline
	Trans    string  // xfade name into the next clip, "" = hard cut
	TransDur float64
	Clip     *spec.Clip
}

// Edit is a project with defaults filled in and every clip placed in time.
type Edit struct {
	P        *spec.Project
	Media    media.Set
	Words    []Word // caption words on the output timeline
	Clips    []ClipT
	Total    float64
	W, H     int
	FPS      int
	Fit      string
	Bg       string // #RRGGBB or blur
	Blur     int
	Quality  string
	Format   string // mp4 or gif
	OutFile  string
	Warnings []spec.Issue
}

const defaultOut = "out/wecut.mp4"

// Resolve checks the project against the probed media and lays out the
// timeline. words are the caption words as parsed (source or output times).
func Resolve(p *spec.Project, m media.Set, words []Word) (*Edit, []spec.Issue) {
	e := &Edit{P: p, Media: m}
	var errs []spec.Issue
	bad := func(code, path, msg, hint string) {
		errs = append(errs, spec.Issue{Code: code, Path: path, Message: msg, Hint: hint})
	}
	warn := func(path, msg string) {
		e.Warnings = append(e.Warnings, spec.Issue{Code: "W_IGNORED", Path: path, Message: msg})
	}

	o := p.Output
	e.FPS = orInt(o.FPS, 30)
	e.Fit = orStr(o.Fit, "cover")
	e.Bg = orStr(o.Background, "#000000")
	e.Blur = orInt(o.Blur, 2)
	e.Quality = orStr(o.Quality, "final")
	e.OutFile = orStr(o.File, defaultOut)
	e.Format = "mp4"
	if strings.EqualFold(filepath.Ext(e.OutFile), ".gif") {
		e.Format = "gif"
	}

	for i := range p.Clips {
		c := &p.Clips[i]
		path := fmt.Sprintf("/clips/%d", i)
		info, ok := m[c.Src]
		if !ok {
			bad("E_MEDIA", path+"/src", "file was not probed: "+c.Src, "")
			continue
		}
		t := ClipT{Index: i, Src: c.Src, Info: info, Kind: info.Kind, Speed: 1, Clip: c}
		switch info.Kind {
		case "video":
			if c.Speed != 0 {
				t.Speed = c.Speed
			}
			t.From = c.From
			t.To = info.Duration
			if c.To != nil {
				t.To = *c.To
			}
			if t.From >= info.Duration {
				bad("E_RANGE", path+"/from", fmt.Sprintf("from (%s) is past the end of %s (%s s long)", num(t.From), c.Src, num(info.Duration)), "Run `wecut probe` to see each file's duration.")
				continue
			}
			if t.To > info.Duration+0.05 {
				bad("E_RANGE", path+"/to", fmt.Sprintf("to (%s) is past the end of %s (%s s long)", num(t.To), c.Src, num(info.Duration)), fmt.Sprintf("Use a value up to %s, or leave \"to\" out to play to the end.", num(info.Duration)))
				continue
			}
			t.To = math.Min(t.To, info.Duration)
			t.Dur = (t.To - t.From) / t.Speed
			if c.Duration != nil {
				warn(path+"/duration", "duration applies to images only; use from/to for video")
			}
			if c.Motion != "" {
				warn(path+"/motion", "motion applies to images only")
			}
		case "image":
			t.Dur = 3
			if c.Duration != nil {
				t.Dur = *c.Duration
			}
			if c.Speed != 0 && c.Speed != 1 {
				warn(path+"/speed", "speed has no effect on an image")
			}
			if c.From != 0 || c.To != nil {
				warn(path+"/from", "from/to have no effect on an image; use duration")
			}
		default:
			bad("E_MEDIA", path+"/src", fmt.Sprintf("%s is %s, a clip needs video or an image", c.Src, info.Kind), "Put music and voice files in \"audio\" instead.")
			continue
		}
		if t.Dur < 0.1 {
			bad("E_VALUE", path, fmt.Sprintf("clip is too short (%s s after speed)", num(t.Dur)), "Clips must last at least 0.1 s.")
			continue
		}
		if c.Zoom > 1 && clipFit(c, e.Fit) == "contain" {
			warn(path+"/zoom", "zoom only applies with fit \"cover\"")
		}
		if fi, fo := c.FadeIn, c.FadeOut; fi+fo > t.Dur {
			bad("E_VALUE", path+"/fadeIn", fmt.Sprintf("fadeIn + fadeOut (%s s) is longer than the clip (%s s)", num(fi+fo), num(t.Dur)), "")
		}
		if c.Transition != nil {
			t.Trans = spec.Transitions[c.Transition.Type]
			t.TransDur = c.Transition.Duration
			if t.TransDur == 0 {
				t.TransDur = 0.5
			}
		}
		e.Clips = append(e.Clips, t)
	}
	if len(errs) > 0 {
		return nil, errs
	}

	// Place clips. A transition overlaps two clips, so it must fit in both.
	off := 0.0
	for i := range e.Clips {
		c := &e.Clips[i]
		c.Offset = off
		off += c.Dur
		if c.Trans != "" && i+1 < len(e.Clips) {
			lim := math.Min(c.Dur, e.Clips[i+1].Dur) - 0.05
			if c.TransDur > lim {
				bad("E_VALUE", fmt.Sprintf("/clips/%d/transition/duration", c.Index), fmt.Sprintf("transition (%s s) is longer than a clip it joins allows (%s s)", num(c.TransDur), num(math.Max(lim, 0))), "Shorten the transition or lengthen the clips.")
				continue
			}
			off -= c.TransDur
		}
	}
	e.Total = round3(off)
	if e.Total > spec.MaxDuration {
		bad("E_LIMIT", "/clips", fmt.Sprintf("video would be %s s long; the limit is %s s", num(e.Total), num(spec.MaxDuration)), "Split it into several projects.")
	}

	// Canvas.
	ratio := orStr(o.Ratio, "9:16")
	var r float64
	if ratio == "original" {
		first := e.Clips[0].Info
		if first.Width == 0 || first.Height == 0 {
			bad("E_MEDIA", "/output/ratio", "cannot read the first clip's size for ratio \"original\"", "Set an explicit ratio such as 9:16.")
			return nil, errs
		}
		w, h := float64(first.Width), float64(first.Height)
		if rot := e.Clips[0].Clip.Rotate; rot == 90 || rot == 270 || rot == -90 {
			w, h = h, w
		}
		r = w / h
	} else {
		var a, b float64
		fmt.Sscanf(ratio, "%f:%f", &a, &b)
		r = a / b
	}
	res := orInt(o.Res, 1080)
	e.W, e.H = canvas(r, res)

	// Timed things must start before the end.
	for i, t := range p.Texts {
		if t.Start >= e.Total {
			warn(fmt.Sprintf("/texts/%d/start", i), "starts after the video ends; it will not show")
		} else if t.End != nil && *t.End > e.Total+0.05 {
			warn(fmt.Sprintf("/texts/%d/end", i), fmt.Sprintf("ends after the video (%s s); cut at the end", num(e.Total)))
		}
	}
	for i, ov := range p.Overlays {
		path := fmt.Sprintf("/overlays/%d", i)
		if info, ok := m[ov.Src]; !ok {
			bad("E_MEDIA", path+"/src", "file was not probed: "+ov.Src, "")
		} else if info.Kind != "image" {
			bad("E_MEDIA", path+"/src", ov.Src+" is not an image", "Overlays take png, jpg or webp.")
		}
		if ov.Start >= e.Total {
			warn(path+"/start", "starts after the video ends; it will not show")
		}
	}
	for i, a := range p.Audio {
		path := fmt.Sprintf("/audio/%d", i)
		info, ok := m[a.Src]
		switch {
		case !ok:
			bad("E_MEDIA", path+"/src", "file was not probed: "+a.Src, "")
		case !info.HasAudio:
			bad("E_MEDIA", path+"/src", a.Src+" has no audio stream", "")
		case a.From >= info.Duration && !a.Loop:
			bad("E_RANGE", path+"/from", fmt.Sprintf("from (%s) is past the end of %s (%s s long)", num(a.From), a.Src, num(info.Duration)), "")
		}
		if a.At >= e.Total {
			warn(path+"/at", "starts after the video ends; it will not play")
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	if cp := p.Captions; cp != nil {
		if cp.Src != "" {
			used := false
			for _, c := range e.Clips {
				used = used || (c.Src == cp.Src && c.Kind == "video")
			}
			if !used {
				warn("/captions/src", "no clip uses "+cp.Src+", so no captions will show")
			}
			e.Words = MapWords(words, e.Clips, cp.Src)
		} else {
			e.Words = words
		}
		if len(words) > spec.MaxWords {
			bad("E_LIMIT", "/captions/words", fmt.Sprintf("too many words (%d, the limit is %d)", len(words), spec.MaxWords), "")
			return nil, errs
		}
		if len(e.Words) == 0 && cp.Src != "" && len(words) > 0 {
			warn("/captions", "none of the transcript falls inside the clips' from/to ranges; check that the transcript times are seconds of captions.src")
		}
	}
	return e, nil
}

// canvas returns even output dimensions with the given ratio, where res is
// the short side, capping the long side at 3840.
func canvas(r float64, res int) (int, int) {
	var w, h float64
	if r <= 1 {
		w, h = float64(res), float64(res)/r
	} else {
		w, h = float64(res)*r, float64(res)
	}
	if long := math.Max(w, h); long > 3840 {
		w, h = w*3840/long, h*3840/long
	}
	return even(w), even(h)
}

func even(v float64) int {
	n := int(math.Round(v/2)) * 2
	if n < 2 {
		return 2
	}
	return n
}

func clipFit(c *spec.Clip, def string) string {
	if c.Fit != "" {
		return c.Fit
	}
	return def
}

func orStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func orInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func num(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", f), "0"), ".")
}
