package ff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseWhisperWords(t *testing.T) {
	tokens := []byte(`{"transcription":[{"offsets":{"from":0,"to":800},"text":" hi there","tokens":[{"text":" hi","offsets":{"from":0,"to":300}},{"text":" there","offsets":{"from":300,"to":800}},{"text":"[_BEG_]","offsets":{"from":0,"to":0}}]}]}`)
	w, err := ParseWhisperWords(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 2 || w[0].Text != "hi" || w[0].Start != 0 || w[0].End != 0.3 || w[1].Text != "there" {
		t.Fatalf("%+v", w)
	}
	// No tokens: phrase timings are spread into words.
	segs := []byte(`{"transcription":[{"offsets":{"from":0,"to":1000},"text":" one two"}]}`)
	w, err = ParseWhisperWords(segs)
	if err != nil || len(w) != 2 || w[0].Text != "one" {
		t.Fatalf("%+v %v", w, err)
	}
	nd := []byte("{\"start\":0,\"end\":1000,\"text\":\"one two\"}\n{\"start\":1000,\"end\":1600,\"text\":\"three\"}\n")
	w, err = ParseWhisperWords(nd)
	if err != nil || len(w) != 3 || w[2].Text != "three" || w[2].Start != 1 {
		t.Fatalf("ndjson %+v %v", w, err)
	}
	if _, err := ParseWhisperWords(nil); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestWhisperFilterHasNoUserPath(t *testing.T) {
	f := whisperFilter("auto")
	if f != "whisper=model=model.bin:destination=words.srt:format=srt:language=auto:use_gpu=false:queue=20" {
		t.Fatal(f)
	}
	if _, ok := cleanLang("en;vol=0"); ok {
		t.Fatal("language accepted a filter breakout")
	}
	if lang, ok := cleanLang("EN"); !ok || lang != "en" {
		t.Fatalf("%q %v", lang, ok)
	}
}

func TestFindWhisperOnPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := FindWhisper(); got != bin {
		t.Fatalf("found %q", got)
	}
}

func TestRunWhisperFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-cli")
	script := "#!/bin/sh\nout=\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"-of\" ]; then out=$2; shift 2; continue; fi\n  shift\ndone\nprintf '%s\\n' '{\"transcription\":[{\"text\":\" hi\",\"offsets\":{\"from\":0,\"to\":500},\"tokens\":[{\"text\":\" hi\",\"offsets\":{\"from\":0,\"to\":500}}]}]}' > \"$out.json\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := runWhisperFile(context.Background(), bin, filepath.Join(dir, "speech.wav"), filepath.Join(dir, "model.bin"), "auto", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 1 || w[0].Text != "hi" || w[0].End != 0.5 {
		t.Fatalf("%+v", w)
	}
}
