package ff

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/engine"
)

// whisperBins are the whisper.cpp command names, newest first. "main" is
// too generic to search for.
var whisperBins = []string{"whisper-cli", "whisper-cpp", "whisper"}

// FindWhisper returns a whisper.cpp binary on PATH, or "" when there is none.
func FindWhisper() string {
	for _, name := range whisperBins {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// Transcribe turns speech in a media file into word timings. It uses
// FFmpeg's whisper filter when this build has one, and otherwise a
// whisper.cpp binary on PATH. Both need a ggml model file. The model path
// is never written into the filter graph: the filter reads a name this
// function chooses inside dir.
func (t *Tools) Transcribe(ctx context.Context, mediaPath, modelPath, lang, dir string) (words []engine.Word, via string, err error) {
	lang, ok := cleanLang(lang)
	if !ok {
		return nil, "", fmt.Errorf("language must be a short code such as en, or auto")
	}
	caps, err := t.Capabilities(ctx)
	if err != nil {
		return nil, "", err
	}
	if caps.Filters["whisper"] {
		words, err = t.transcribeFilter(ctx, mediaPath, modelPath, lang, dir)
		return words, "ffmpeg", err
	}
	bin := FindWhisper()
	if bin == "" {
		return nil, "", fmt.Errorf("this FFmpeg has no whisper filter, and no whisper.cpp binary (whisper-cli, whisper-cpp or whisper) is on PATH")
	}
	words, err = t.transcribeCLI(ctx, bin, mediaPath, modelPath, lang, dir)
	return words, "whisper.cpp", err
}

func cleanLang(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "auto" {
		return "auto", true
	}
	if len(s) < 2 || len(s) > 8 {
		return "", false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return "", false
		}
	}
	return s, true
}

// whisperFilter is the audio filter for a local transcription. Every
// token in it is chosen here: a model path from the user would be able
// to break out of the filter graph.
func whisperFilter(lang string) string {
	return "whisper=model=model.bin:destination=words.srt:format=srt:language=" + lang + ":use_gpu=false:queue=20"
}

func (t *Tools) transcribeFilter(ctx context.Context, media, model, lang, dir string) ([]engine.Word, error) {
	if err := linkModel(dir, model); err != nil {
		return nil, err
	}
	args := append(input(media), "-t", "600", "-map", "0:a:0", "-vn", "-af", whisperFilter(lang), "-f", "null", "-")
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("whisper filter failed: %s", lastLines(stderr.String(), 3))
	}
	b, err := os.ReadFile(filepath.Join(dir, "words.srt"))
	if err != nil {
		return nil, fmt.Errorf("whisper filter wrote no transcript")
	}
	return engine.ParseTranscript(b, ".srt")
}

func (t *Tools) transcribeCLI(ctx context.Context, bin, media, model, lang, dir string) ([]engine.Word, error) {
	wav := filepath.Join(dir, "speech.wav")
	args := append(input(media), "-t", "600", "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-y", wav)
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("could not read the audio: %s", lastLines(stderr.String(), 2))
	}
	return runWhisperFile(ctx, bin, wav, model, lang, dir)
}

// runWhisperFile runs whisper.cpp on a wav and reads the JSON it writes.
// Token timings (-ojf) are used when that flag works; otherwise segments.
func runWhisperFile(ctx context.Context, bin, wav, model, lang, dir string) ([]engine.Word, error) {
	outBase := filepath.Join(dir, "transcript")
	run := func(extra ...string) error {
		args := append([]string{"-m", model, "-f", wav, "-of", outBase, "-l", lang, "-np"}, extra...)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = dir
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("whisper.cpp failed: %s", lastLines(buf.String(), 4))
		}
		return nil
	}
	errFull := run("-ojf", "-sow")
	b, rerr := os.ReadFile(outBase + ".json")
	if errFull != nil || rerr != nil {
		_ = os.Remove(outBase + ".json")
		if err := run("-oj"); err != nil {
			if errFull != nil {
				return nil, errFull
			}
			return nil, err
		}
		var err error
		b, err = os.ReadFile(outBase + ".json")
		if err != nil {
			return nil, fmt.Errorf("whisper.cpp wrote no JSON")
		}
	}
	return ParseWhisperWords(b)
}

// linkModel exposes the model under a fixed name so the filter string
// stays free of the user's path.
func linkModel(dir, model string) error {
	dest := filepath.Join(dir, "model.bin")
	if filepath.Clean(model) == filepath.Clean(dest) {
		return nil
	}
	_ = os.Remove(dest)
	if err := os.Symlink(model, dest); err == nil {
		return nil
	}
	if err := os.Link(model, dest); err == nil {
		return nil
	}
	in, err := os.Open(model)
	if err != nil {
		return fmt.Errorf("cannot read the whisper model: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return fmt.Errorf("cannot copy the whisper model: %w", err)
	}
	return nil
}

// ParseWhisperWords reads whisper.cpp JSON (segments or token timings),
// the FFmpeg whisper filter's JSON lines (times in milliseconds), or SRT.
func ParseWhisperWords(data []byte) ([]engine.Word, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("transcription was empty")
	}
	if bytes.Contains(data, []byte("-->")) && !bytes.HasPrefix(data, []byte("{")) && !bytes.HasPrefix(data, []byte("[")) {
		return engine.ParseTranscript(data, ".srt")
	}
	if isNDJSON(data) {
		return parseWhisperNDJSON(data)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		if w, nerr := parseWhisperNDJSON(data); nerr == nil && len(w) > 0 {
			return w, nil
		}
		return nil, fmt.Errorf("transcription is not JSON: %v", err)
	}
	if w := wordsFromTokens(raw); len(w) > 0 {
		return w, nil
	}
	return engine.ParseTranscript(data, ".json")
}

func isNDJSON(data []byte) bool {
	return bytes.HasPrefix(data, []byte("{")) && bytes.Contains(data, []byte("\n{"))
}

func parseWhisperNDJSON(data []byte) ([]engine.Word, error) {
	var segs []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var row struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("whisper JSON line: %w", err)
		}
		if strings.TrimSpace(row.Text) == "" {
			continue
		}
		// The FFmpeg filter writes start and end in milliseconds.
		segs = append(segs, map[string]any{"text": row.Text, "start": row.Start / 1000, "end": row.End / 1000})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("transcription has no words")
	}
	body, _ := json.Marshal(map[string]any{"segments": segs})
	return engine.ParseTranscript(body, ".json")
}

func wordsFromTokens(raw any) []engine.Word {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	arr, _ := m["transcription"].([]any)
	var out []engine.Word
	for _, seg := range arr {
		sm, _ := seg.(map[string]any)
		tokens, _ := sm["tokens"].([]any)
		for _, tok := range tokens {
			tm, _ := tok.(map[string]any)
			text := strings.TrimSpace(fmt.Sprint(tm["text"]))
			if text == "" || text == "<nil>" || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "<|") {
				continue
			}
			off, _ := tm["offsets"].(map[string]any)
			from, ok1 := numMS(off["from"])
			to, ok2 := numMS(off["to"])
			if !ok1 || !ok2 {
				continue
			}
			if to < from {
				to = from
			}
			out = append(out, engine.Word{Text: text, Start: from / 1000, End: to / 1000})
		}
	}
	return out
}

func numMS(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
