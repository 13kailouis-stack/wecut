// Package ff finds and runs FFmpeg and FFprobe. Commands are always started
// with an argument list, never through a shell.
package ff

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

type Tools struct {
	FFmpeg, FFprobe string
	Version         string
	Major, Minor    int
}

// ErrMissing means FFmpeg or FFprobe could not be found.
var ErrMissing = errors.New("ffmpeg not found")

// InstallHint tells an agent how to get FFmpeg on this OS.
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install FFmpeg with libass: `brew install ffmpeg-full` (WeCut finds it on its own; Homebrew's slim `ffmpeg` formula has no libass, so text and captions would not work). Or set WECUT_FFMPEG and WECUT_FFPROBE to the binaries."
	case "windows":
		return "Install FFmpeg 6 or newer: `winget install Gyan.FFmpeg`, then open a new terminal. Or set WECUT_FFMPEG and WECUT_FFPROBE to the binaries."
	default:
		return "Install FFmpeg 6 or newer: `sudo apt-get install -y ffmpeg` (Debian/Ubuntu), `sudo dnf install ffmpeg` (Fedora) or `apk add ffmpeg` (Alpine). Or set WECUT_FFMPEG and WECUT_FFPROBE to the binaries."
	}
}

// Find locates the binaries: explicit paths, then WECUT_FFMPEG/WECUT_FFPROBE,
// then PATH. FFprobe defaults to the one next to FFmpeg.
func Find(ctx context.Context, ffmpegPath, ffprobePath string) (*Tools, error) {
	t := &Tools{}
	var err error
	if ffmpegPath == "" && os.Getenv("WECUT_FFMPEG") == "" {
		ffmpegPath = kegFFmpeg()
	}
	if t.FFmpeg, err = look(ffmpegPath, "WECUT_FFMPEG", "ffmpeg"); err != nil {
		return nil, err
	}
	if ffprobePath == "" && os.Getenv("WECUT_FFPROBE") == "" {
		sib := filepath.Join(filepath.Dir(t.FFmpeg), "ffprobe"+filepath.Ext(t.FFmpeg))
		if st, e := os.Stat(sib); e == nil && !st.IsDir() {
			ffprobePath = sib
		}
	}
	if t.FFprobe, err = look(ffprobePath, "WECUT_FFPROBE", "ffprobe"); err != nil {
		return nil, err
	}
	out, err := output(ctx, t.FFmpeg, "-hide_banner", "-version")
	if err != nil {
		return nil, fmt.Errorf("%w: %s did not run: %v", ErrMissing, t.FFmpeg, err)
	}
	first := strings.SplitN(string(out), "\n", 2)[0]
	if m := versionRe.FindStringSubmatch(first); m != nil {
		t.Version = m[1]
		t.Major, _ = strconv.Atoi(m[2])
		t.Minor, _ = strconv.Atoi(m[3])
	} else {
		// Git builds say "ffmpeg version N-1234-g..."; treat them as current.
		t.Version = strings.TrimPrefix(strings.Fields(first + " ? ? ?")[2], "n")
		t.Major = 99
	}
	return t, nil
}

var versionRe = regexp.MustCompile(`version n?((\d+)\.(\d+)(?:\.\d+)?)`)

// kegFFmpeg finds Homebrew's ffmpeg-full, which is keg-only (not on PATH)
// and is the Homebrew build that includes libass.
func kegFFmpeg() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	for _, p := range []string{"/opt/homebrew/opt/ffmpeg-full/bin/ffmpeg", "/usr/local/opt/ffmpeg-full/bin/ffmpeg"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func look(explicit, env, name string) (string, error) {
	for _, p := range []string{explicit, os.Getenv(env)} {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return "", fmt.Errorf("%w: %s (from %s) is not a file", ErrMissing, p, env)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not on PATH", ErrMissing, name)
	}
	return p, nil
}

func output(ctx context.Context, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		err = errors.New(lastLines(stderr.String(), 1))
	}
	return out, err
}

// Caps lists what this FFmpeg build can do. Filters shows the ones WeCut
// cares about; every filter the build has is kept for MissingFor.
type Caps struct {
	Encoders map[string]bool `json:"encoders"`
	Filters  map[string]bool `json:"filters"`
	all      map[string]bool
}

var (
	wantEncoders = []string{"libx264", "h264_nvenc", "h264_qsv", "h264_amf", "h264_videotoolbox", "h264_mf", "libopenh264", "aac"}
	// Core filters every render may use. ass (text, captions) and whisper
	// (local transcription) are features: a build without them still works
	// for projects that do not need them.
	coreFilters  = []string{"xfade", "acrossfade", "zoompan", "sidechaincompress", "amix", "alimiter", "drawbox", "lutyuv", "palettegen", "paletteuse", "boxblur", "tpad", "apad", "atempo", "colorchannelmixer", "rotate"}
	wantFilters  = append(append([]string{}, coreFilters...), "ass", "whisper")
	filterNeeded = map[string]string{"ass": "text and captions", "whisper": "local transcription", "zoompan": "photo motion", "xfade": "transitions", "sidechaincompress": "ducking"}
)

func (t *Tools) Capabilities(ctx context.Context) (*Caps, error) {
	c := &Caps{Encoders: map[string]bool{}, Filters: map[string]bool{}}
	enc, err := output(ctx, t.FFmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return nil, err
	}
	fil, err := output(ctx, t.FFmpeg, "-hide_banner", "-filters")
	if err != nil {
		return nil, err
	}
	encNames := fieldSet(string(enc), 1)
	filNames := fieldSet(string(fil), 1)
	for _, e := range wantEncoders {
		c.Encoders[e] = encNames[e]
	}
	for _, f := range wantFilters {
		c.Filters[f] = filNames[f]
	}
	c.all = filNames
	return c, nil
}

// MissingFor lists filters a filter graph uses that this build lacks.
func (c *Caps) MissingFor(graph string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range GraphFilters(graph) {
		if !c.all[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// Explain says which WeCut features need the given filters.
func Explain(filters []string) string {
	var parts []string
	for _, f := range filters {
		if why, ok := filterNeeded[f]; ok {
			parts = append(parts, f+" ("+why+")")
		} else {
			parts = append(parts, f)
		}
	}
	return strings.Join(parts, ", ")
}

// GraphFilters returns the filter names in a filter graph, in order. It
// splits on , and ; outside quotes and drops [labels].
func GraphFilters(graph string) []string {
	var names []string
	var cur strings.Builder
	quoted, escaped := false, false
	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		for strings.HasPrefix(s, "[") {
			end := strings.IndexByte(s, ']')
			if end < 0 {
				return
			}
			s = strings.TrimSpace(s[end+1:])
		}
		end := strings.IndexAny(s, "=[")
		if end < 0 {
			end = len(s)
		}
		if name := strings.TrimSpace(s[:end]); name != "" {
			names = append(names, name)
		}
	}
	for _, r := range graph {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '\'':
			quoted = !quoted
		case !quoted && (r == ',' || r == ';'):
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return names
}

// fieldSet collects the nth whitespace field of every line.
func fieldSet(s string, n int) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) > n {
			out[f[n]] = true
		}
	}
	return out
}

// Missing lists core filters this build lacks.
func (c *Caps) Missing() []string {
	var out []string
	for _, f := range coreFilters {
		if !c.Filters[f] {
			out = append(out, f)
		}
	}
	return out
}

// DefaultEncoder prefers libx264, then hardware encoders, then whatever H.264
// encoder exists.
func (c *Caps) DefaultEncoder() string {
	for _, e := range []string{"libx264", "h264_videotoolbox", "h264_nvenc", "h264_qsv", "h264_amf", "h264_mf", "libopenh264"} {
		if c.Encoders[e] {
			return e
		}
	}
	return ""
}

type probeOut struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
	} `json:"format"`
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
		RFrameRate  string            `json:"r_frame_rate"`
		AvgFrame    string            `json:"avg_frame_rate"`
		Duration    string            `json:"duration"`
		NbFrames    string            `json:"nb_frames"`
		Tags        map[string]string `json:"tags"`
		Disposition map[string]int    `json:"disposition"`
		SideData    []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
}

var imageFormats = map[string]bool{"image2": true, "png_pipe": true, "jpeg_pipe": true, "webp_pipe": true}

// Probe reads one file's streams. The same demuxer whitelist as rendering
// applies, so a playlist renamed to .mp4 is refused here.
func (t *Tools) Probe(ctx context.Context, abs string) (media.Info, error) {
	return t.probe(ctx, abs, engine.FormatWhitelist)
}

func (t *Tools) probe(ctx context.Context, abs, formats string) (media.Info, error) {
	out, err := output(ctx, t.FFprobe, "-v", "error", "-protocol_whitelist", "file", "-format_whitelist", formats,
		"-print_format", "json", "-show_format", "-show_streams", "file:"+abs)
	if err != nil {
		return media.Info{}, fmt.Errorf("cannot read media: %v", err)
	}
	var p probeOut
	if err := json.Unmarshal(out, &p); err != nil {
		return media.Info{}, fmt.Errorf("cannot read ffprobe output: %v", err)
	}
	info := media.Info{Path: abs}
	info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	info.SizeBytes, _ = strconv.ParseInt(p.Format.Size, 10, 64)
	isImageFmt := imageFormats[p.Format.FormatName]
	hasVideo := false
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition["attached_pic"] == 1 || hasVideo {
				continue // cover art inside an audio file
			}
			hasVideo = true
			info.VideoCodec = s.CodecName
			info.Width, info.Height = s.Width, s.Height
			info.FPS = rate(s.AvgFrame)
			if info.FPS == 0 {
				info.FPS = rate(s.RFrameRate)
			}
			rot := 0.0
			if r, ok := s.Tags["rotate"]; ok {
				rot, _ = strconv.ParseFloat(r, 64)
			}
			for _, sd := range s.SideData {
				if sd.Rotation != 0 {
					rot = sd.Rotation
				}
			}
			info.Rotation = ((int(rot) % 360) + 360) % 360
			if info.Rotation == 90 || info.Rotation == 270 {
				info.Width, info.Height = info.Height, info.Width
			}
		case "audio":
			if !info.HasAudio {
				info.HasAudio = true
				info.AudioCodec = s.CodecName
			}
		}
	}
	switch {
	case hasVideo && (isImageFmt || (info.Duration == 0 && !info.HasAudio)):
		info.Kind = "image"
		info.Duration = 0
		info.FPS = 0
	case hasVideo:
		info.Kind = "video"
	case info.HasAudio:
		info.Kind = "audio"
	default:
		return media.Info{}, fmt.Errorf("no audio or video stream found")
	}
	info.Duration = float64(int(info.Duration*1000)) / 1000
	return info, nil
}

func rate(s string) float64 {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	n, _ := strconv.ParseFloat(a, 64)
	d, _ := strconv.ParseFloat(b, 64)
	if d == 0 {
		return 0
	}
	return float64(int(n/d*100)) / 100
}

// Job is one FFmpeg run inside its own directory.
type Job struct {
	Dir      string
	Args     []string
	Duration float64       // expected output seconds, for progress
	Progress func(float64) // 0..1, may be nil
}

// Run executes FFmpeg and returns its error output when it fails.
func (t *Tools) Run(ctx context.Context, j Job) error {
	cmd := t.command(ctx, j.Dir, append([]string{"-nostats", "-progress", "pipe:1", "-y"}, j.Args...)...)
	var stderr tail
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readProgress(stdout, j.Duration, j.Progress)
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", failureLines(msg))
	}
	return nil
}

// Keep the cause as well as the shutdown messages. FFmpeg often emits many
// encoder/EOF errors after a single filter configuration error; tail-only
// reporting hides the actionable diagnostic from the calling agent.
func failureLines(s string) string {
	var causes []string
	for _, line := range strings.Split(s, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "[parsed_") || strings.Contains(lower, "error applying") || strings.Contains(lower, "no such filter") || strings.Contains(lower, "matches no streams") {
			causes = append(causes, strings.TrimSpace(line))
			if len(causes) == 6 {
				break
			}
		}
	}
	if len(causes) == 0 {
		return lastLines(s, 6)
	}
	return strings.Join(causes, " | ") + " | " + lastLines(s, 2)
}

func readProgress(r io.Reader, total float64, fn func(float64)) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || fn == nil || total <= 0 {
			continue
		}
		if k == "out_time_us" || k == "out_time_ms" { // both are microseconds
			if us, err := strconv.ParseFloat(v, 64); err == nil && us > 0 {
				fn(min(1, us/1e6/total))
			}
		}
	}
}

// tail keeps the last 16 KB written to it.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 16<<10 {
		t.b = t.b[len(t.b)-16<<10:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.b) }

// lastLines keeps the last n meaningful lines of FFmpeg output, without the
// "file:C:\...: " prefix that only repeats the path.
func lastLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.Contains(l, "Last message repeated") {
			continue
		}
		if strings.HasPrefix(l, "file:") {
			if i := strings.LastIndex(l, ": "); i > 0 {
				l = l[i+2:]
			}
		}
		lines = append(lines, l)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// Verify checks a finished render against what was asked for. A non-zero
// exit is not the only way a render fails: truncated files are possible too.
func (t *Tools) Verify(ctx context.Context, abs string, p *engine.Plan) (media.Info, []spec.Issue) {
	info, err := t.probe(ctx, abs, engine.FormatWhitelist+",gif")
	if err != nil {
		return info, []spec.Issue{{Code: "E_VERIFY", Message: "the rendered file cannot be read: " + err.Error()}}
	}
	if p.Format == "gif" { // a GIF is one video stream that probes like a clip
		info.Kind = "gif"
	}
	var bad []spec.Issue
	if info.Width != p.W || info.Height != p.H {
		bad = append(bad, spec.Issue{Code: "E_VERIFY", Message: fmt.Sprintf("rendered %dx%d, expected %dx%d", info.Width, info.Height, p.W, p.H)})
	}
	tol := max(0.25, 3/float64(p.FPS))
	if p.Format == "gif" {
		tol = max(tol, 0.2)
	}
	if d := info.Duration - p.Duration; d < -tol || d > tol {
		bad = append(bad, spec.Issue{Code: "E_VERIFY", Message: fmt.Sprintf("rendered %.2f s, expected %.2f s", info.Duration, p.Duration)})
	}
	if p.Audio && !info.HasAudio {
		bad = append(bad, spec.Issue{Code: "E_VERIFY", Message: "rendered file has no audio track"})
	}
	return info, bad
}
