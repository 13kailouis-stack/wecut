package cli

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

// transcribe writes word timings for a video or audio file. FFmpeg's
// whisper filter is used when this build has it; otherwise a whisper.cpp
// binary on PATH (whisper-cli, whisper-cpp or whisper). Neither is
// required to render. A missing model or a missing binary is a dependency
// error, not a crash.
func (a *app) transcribe(args []string) int {
	var c common
	var write, model, lang string
	fs := flag.NewFlagSet("transcribe", flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&write, "write", "", "word-timings JSON to write (default: next to the file, as <name>.words.json)")
	fs.StringVar(&model, "model", "", "whisper.cpp ggml model (default: WECUT_WHISPER_MODEL, or models/ggml-base.bin in the workspace)")
	fs.StringVar(&lang, "language", "auto", "spoken language, or auto")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one video or audio file", Hint: "wecut transcribe media/talk.mp4 --write media/talk.words.json"})
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
	modelPath, iss := resolveWhisperModel(root, model)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	if write == "" {
		stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		write = filepath.ToSlash(filepath.Join(filepath.Dir(src), stem+".words.json"))
	}
	if strings.ToLower(filepath.Ext(write)) != ".json" {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "--write must be a .json file"})
	}
	outAbs, err := root.Output(write)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(write, "/write", err))
	}
	if outAbs == abs {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: "the transcript would overwrite the media file", Hint: "Pass --write with a different name."})
	}

	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	info, err := t.Probe(a.ctx, abs)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_MEDIA", Path: src, Message: err.Error()})
	}
	if info.Kind == "image" || (info.Kind != "audio" && !info.HasAudio) {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_MEDIA", Path: src, Message: src + " has no audio to transcribe"})
	}

	job, err := os.MkdirTemp("", "wecut-transcribe-")
	if err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	defer os.RemoveAll(job)
	words, via, err := t.Transcribe(a.ctx, abs, modelPath, lang, job)
	if err != nil {
		hint := "Install FFmpeg built with the whisper filter, or put whisper-cli on PATH. Both need a ggml model: --model, WECUT_WHISPER_MODEL, or models/ggml-base.bin. WeCut does not download one."
		return a.fail(exitDep, nil, spec.Issue{Code: "E_DEPENDENCY", Message: err.Error(), Hint: hint})
	}
	if len(words) == 0 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_MEDIA", Message: "no speech found in " + src})
	}
	if err := writeWordJSON(outAbs, words); err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	note := ""
	if info.Duration > 600 {
		note = "only the first 600 s were transcribed"
	}
	res := map[string]any{
		"file": src, "write": root.Rel(outAbs), "engine": via, "words": len(words), "language": lang,
		"next": "pass this file as transcript when applying a recipe, or as captions.words",
	}
	if note != "" {
		res["note"] = note
	}
	return a.ok(res)
}

// resolveWhisperModel finds a ggml model. A relative path stays in the
// workspace. An absolute --model or WECUT_WHISPER_MODEL may point outside
// it: the model is a tool, like the FFmpeg binary, and it is only read.
func resolveWhisperModel(root *workspace.Root, flag string) (string, *spec.Issue) {
	if flag == "" {
		flag = os.Getenv("WECUT_WHISPER_MODEL")
	}
	if flag != "" {
		if strings.Contains(flag, "://") {
			return "", &spec.Issue{Code: "E_PATH", Message: "the model must be a file, not a URL", Hint: "Download the ggml file first."}
		}
		if filepath.IsAbs(flag) {
			st, err := os.Stat(flag)
			if err != nil || !st.Mode().IsRegular() {
				return "", &spec.Issue{Code: "E_DEPENDENCY", Message: "whisper model not found: " + flag, Hint: "Pass --model with a ggml file, or set WECUT_WHISPER_MODEL."}
			}
			return flag, nil
		}
		abs, err := root.Input(flag)
		if err != nil {
			iss := pathIssue(flag, "", err)
			return "", &iss
		}
		return abs, nil
	}
	for _, name := range []string{
		"models/ggml-base.bin", "models/ggml-base.en.bin",
		"models/ggml-small.bin", "models/ggml-small.en.bin",
		"models/ggml-tiny.bin", "models/ggml-tiny.en.bin",
	} {
		if abs, err := root.Input(name); err == nil {
			return abs, nil
		}
	}
	return "", &spec.Issue{
		Code:    "E_DEPENDENCY",
		Message: "no whisper model found",
		Hint:    "Put a ggml model at models/ggml-base.bin, pass --model, or set WECUT_WHISPER_MODEL. WeCut does not download one.",
	}
}

func writeWordJSON(path string, words []engine.Word) error {
	type row struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	}
	out := make([]row, len(words))
	for i, w := range words {
		out[i] = row{Word: w.Text, Start: math.Round(w.Start*1000) / 1000, End: math.Round(w.End*1000) / 1000}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
