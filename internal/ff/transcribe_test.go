package ff

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// A copy of this test binary, named whisper-cli, stands in for
	// whisper.cpp. Windows LookPath only resolves PATHEXT names and
	// cannot execute a shell script.
	if whisperStubInvoked() {
		os.Exit(whisperStub())
	}
	os.Exit(m.Run())
}

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
	bin := plantWhisperCLI(t, dir)
	t.Setenv("PATH", dir)
	if got := FindWhisper(); !samePath(got, bin) {
		t.Fatalf("found %q, want %q", got, bin)
	}
}

func TestRunWhisperFile(t *testing.T) {
	dir := t.TempDir()
	bin := plantWhisperCLI(t, dir)
	w, err := runWhisperFile(context.Background(), bin, filepath.Join(dir, "speech.wav"), filepath.Join(dir, "model.bin"), "auto", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 1 || w[0].Text != "hi" || w[0].End != 0.5 {
		t.Fatalf("%+v", w)
	}
}

// plantWhisperCLI puts a runnable whisper-cli on dir. On Windows the
// file is whisper-cli.exe so exec.LookPath can resolve it.
func plantWhisperCLI(t *testing.T, dir string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	name := "whisper-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	if err := cloneExecutable(dst, exe); err != nil {
		t.Fatal(err)
	}
	return dst
}

func cloneExecutable(dst, src string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func whisperStubInvoked() bool {
	base := filepath.Base(os.Args[0])
	if strings.EqualFold(filepath.Ext(base), ".exe") {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	switch strings.ToLower(base) {
	case "whisper-cli", "whisper-cpp", "whisper":
		return true
	default:
		return false
	}
}

// whisperStub writes the JSON file whisper.cpp would write for -of.
func whisperStub() int {
	out := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-of" && i+1 < len(args) {
			out = args[i+1]
			i++
		}
	}
	if out == "" {
		os.Stderr.WriteString("missing -of\n")
		return 2
	}
	body := "{\"transcription\":[{\"text\":\" hi\",\"offsets\":{\"from\":0,\"to\":500},\"tokens\":[{\"text\":\" hi\",\"offsets\":{\"from\":0,\"to\":500}}]}]}\n"
	if err := os.WriteFile(out+".json", []byte(body), 0o644); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	return 0
}
