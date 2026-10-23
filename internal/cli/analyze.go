package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/13kailouis-stack/wecut/internal/analyze"
	"github.com/13kailouis-stack/wecut/internal/assets"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

const (
	analyzeMax  = 1800.0 // seconds looked at
	analyzeRate = 11025  // Hz for tempo and silence
)

// analyze measures a video: cuts, pace, the hook, loudness, silences, tempo,
// and (with a transcript) how fast and what is said in the first seconds.
// It also writes a sheet of frames, one per shot, for the agent to look at.
func (a *app) analyze(args []string) int {
	var c common
	var transcript, outDir string
	var threshold float64
	var frames int
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&transcript, "transcript", "", "word timings for this video (.json, .srt, .vtt)")
	fs.StringVar(&outDir, "out", "out/analyze", "folder for the frame sheet")
	fs.Float64Var(&threshold, "threshold", 10, "scene cut sensitivity, 1-100 (lower finds more cuts)")
	fs.IntVar(&frames, "frames", 12, "frames on the sheet, 1-24")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one video file", Hint: "wecut analyze media/viral.mp4"})
	}
	if threshold < 1 || threshold > 100 || frames < 1 || frames > 24 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "--threshold must be 1-100 and --frames 1-24"})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	src := pos[0]
	abs, err := root.Input(src)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(src, "", err))
	}
	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	info, err := t.Probe(a.ctx, abs)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(src, "", mediaErr{src, err}))
	}
	if info.Kind != "video" {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_MEDIA", Message: src + " is " + info.Kind + ", not a video"})
	}
	var words []engine.Word
	if transcript != "" {
		tabs, err := root.Input(transcript)
		if err != nil {
			return a.fail(exitInput, nil, pathIssue(transcript, "", err))
		}
		b, err := os.ReadFile(tabs)
		if err == nil {
			words, err = engine.ParseTranscript(b, filepath.Ext(transcript))
		}
		if err != nil {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_CAPTIONS", Message: err.Error()})
		}
	}

	var warnings []spec.Issue
	dur := info.Duration
	if dur > analyzeMax {
		warnings = append(warnings, spec.Issue{Code: "W_PARTIAL", Message: fmt.Sprintf("only the first %.0f s of %.0f s were analysed", analyzeMax, dur)})
		dur = analyzeMax
	}
	job, err := os.MkdirTemp("", "wecut-analyze-")
	if err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	defer os.RemoveAll(job)
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(max(120, dur*4)*float64(time.Second)))
	defer cancel()
	started := time.Now()

	raw, err := t.SceneCuts(ctx, abs, job, threshold, dur)
	if err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	cuts, bursts := analyze.Bursts(raw, 0.3)
	shots := analyze.Shots(cuts, dur)
	pace := analyze.Measure(shots, dur)

	res := map[string]any{
		"file": src, "duration": info.Duration, "width": info.Width, "height": info.Height,
		"fps": info.FPS, "ratio": analyze.Ratio(info.Width, info.Height),
		"shots": shots, "pace": pace,
	}
	var hints []string
	hints = append(hints, fmt.Sprintf("%s: %d shots, median %.1f s", pace.Feel, pace.Shots, pace.MedianShot))
	if len(bursts) > 0 {
		res["flashes"] = bursts
		at := make([]string, 0, 3)
		for _, b := range bursts[:min(3, len(bursts))] {
			at = append(at, fmt.Sprintf("%.1f s", b.Start))
		}
		hints = append(hints, fmt.Sprintf("%d flash/whip/strobe moments (at %s); check the sheet to tell a flash transition from stage lights", len(bursts), strings.Join(at, ", ")))
	}
	if pace.HookCuts >= 2 {
		hints = append(hints, fmt.Sprintf("busy hook: %d cuts in the first 3 s", pace.HookCuts))
	} else if pace.Shots > 1 && shots[0].Dur >= 3 {
		hints = append(hints, fmt.Sprintf("the opening shot holds %.1f s before the first cut", shots[0].Dur))
	}

	if info.HasAudio {
		pcm, err := t.PCM(ctx, abs, job, analyzeRate, dur)
		if err != nil {
			return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
		}
		snd := analyze.Sound(pcm, analyzeRate)
		res["audio"] = snd
		if snd.BPM > 0 && snd.BPMConf >= 0.2 {
			hints = append(hints, fmt.Sprintf("steady beat around %.0f BPM (confidence %.2f)", snd.BPM, snd.BPMConf))
			if len(cuts) >= 3 {
				sync := analyze.BeatSync(cuts, snd.Beats, 0.08)
				res["cuts_on_beat"] = sync
				if sync >= 0.5 {
					hints = append(hints, fmt.Sprintf("cuts follow the music: %.0f%% land on a beat", sync*100))
				}
			}
		}
		pauses := snd.Silences
		if n := len(pauses); n > 0 && pauses[n-1].End >= dur-0.2 && pauses[n-1].End-pauses[n-1].Start >= 1.5 {
			s := pauses[n-1]
			hints = append(hints, fmt.Sprintf("the last %.1f s are silent (from %.1f s): often the platform's download outro, not part of the edit; leave it out of a recipe", s.End-s.Start, s.Start))
			res["content_end"] = s.Start
			pauses = pauses[:n-1]
		}
		if n := len(pauses); n > 0 {
			hints = append(hints, fmt.Sprintf("%d pauses of 0.35 s or more", n))
		}
	} else {
		hints = append(hints, "no audio track")
	}

	if len(words) > 0 {
		sp := speech(words)
		res["speech"] = sp
		if wpm, _ := sp["wpm"].(float64); wpm >= 170 {
			hints = append(hints, fmt.Sprintf("fast talker: %.0f words per minute", wpm))
		}
	}

	// The frame sheet: the agent's eyes on text, framing and style.
	times := analyze.SheetTimes(shots, frames)
	sheetRel := filepath.ToSlash(filepath.Join(outDir, strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))+"-sheet.jpg"))
	sheetAbs, err := root.Output(sheetRel)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(sheetRel, "", err))
	}
	if err := assets.WriteFonts(filepath.Join(job, "fonts"), []string{"roboto"}); err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	if err := t.Sheet(ctx, abs, job, times, 4, sheetAbs); err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	res["sheet"] = root.Rel(sheetAbs)
	res["sheet_times"] = times
	res["hints"] = hints
	res["analyze_seconds"] = round2(time.Since(started).Seconds())
	if len(warnings) > 0 {
		res["warnings"] = warnings
	}
	return a.ok(res)
}

// speech summarises a transcript: how much is said, how fast, and the words
// of the hook (the first 3 seconds).
func speech(words []engine.Word) map[string]any {
	first, last := words[0].Start, words[len(words)-1].End
	var hook []string
	for _, w := range words {
		if w.Start < 3 {
			hook = append(hook, w.Text)
		}
	}
	out := map[string]any{"words": len(words), "hook_text": strings.Join(hook, " ")}
	if span := last - first; span > 1 {
		out["wpm"] = math.Round(float64(len(words)) / span * 60)
	}
	out["first_word_at"] = round2(first)
	return out
}
