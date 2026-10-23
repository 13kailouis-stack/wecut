package ff

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/13kailouis-stack/wecut/internal/engine"
)

// command builds an FFmpeg call that reads only whitelisted local inputs and
// is killed with its whole process tree when ctx ends.
func (t *Tools) command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, t.FFmpeg, append([]string{"-hide_banner", "-nostdin", "-loglevel", "error"}, args...)...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			// Chocolatey and similar package managers start FFmpeg through a
			// shim; killing only the shim leaves FFmpeg running and holding
			// the files. taskkill /T ends the whole tree.
			_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		}
		return cmd.Process.Kill()
	}
	return cmd
}

func input(abs string) []string {
	return []string{"-protocol_whitelist", "file", "-format_whitelist", engine.FormatWhitelist, "-i", "file:" + abs}
}

// SceneCuts returns the times of hard cuts in the first maxSec seconds,
// found by FFmpeg's scdet on a 192 px wide copy (fast, and size does not
// change what counts as a cut). threshold is scdet's 0..100 score.
func (t *Tools) SceneCuts(ctx context.Context, abs, dir string, threshold, maxSec float64) ([]float64, error) {
	args := append(input(abs), "-t", strconv.FormatFloat(maxSec, 'f', 3, 64), "-map", "0:v:0", "-an",
		"-vf", fmt.Sprintf("scale=192:-2,scdet=threshold=%g,metadata=mode=print:key=lavfi.scd.time:file=scd.txt", threshold),
		"-f", "null", "-")
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("scene detection failed: %s", lastLines(stderr.String(), 2))
	}
	f, err := os.Open(filepath.Join(dir, "scd.txt"))
	if os.IsNotExist(err) {
		return nil, nil // no cuts at all
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cuts []float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "lavfi.scd.time="); ok {
			if c, err := strconv.ParseFloat(v, 64); err == nil {
				cuts = append(cuts, c)
			}
		}
	}
	return cuts, sc.Err()
}

// PCM decodes the first maxSec seconds of the first audio stream to mono
// 16-bit samples at rate Hz.
func (t *Tools) PCM(ctx context.Context, abs, dir string, rate int, maxSec float64) ([]int16, error) {
	args := append(input(abs), "-t", strconv.FormatFloat(maxSec, 'f', 3, 64), "-map", "0:a:0", "-vn",
		"-ac", "1", "-ar", strconv.Itoa(rate), "-f", "s16le", "pipe:1")
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	raw, rerr := io.ReadAll(io.LimitReader(out, int64(maxSec*float64(rate)*2)+4096))
	if werr := cmd.Wait(); werr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("audio decode failed: %s", lastLines(stderr.String(), 2))
	}
	if rerr != nil {
		return nil, rerr
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return pcm, nil
}

// Sheet writes one JPEG with a frame at each time, tiled cols wide, each
// stamped with its time. dir must hold fonts/roboto-700.ttf.
func (t *Tools) Sheet(ctx context.Context, abs, dir string, times []float64, cols int, out string) error {
	if len(times) == 0 {
		return fmt.Errorf("no frame times")
	}
	sel := make([]string, len(times))
	for i, s := range times {
		v := strconv.FormatFloat(s, 'f', 3, 64)
		sel[i] = fmt.Sprintf("lt(prev_pts*TB,%s)*gte(pts*TB,%s)", v, v)
	}
	cols = max(1, min(cols, len(times)))
	rows := int(math.Ceil(float64(len(times)) / float64(cols)))
	graph := fmt.Sprintf("[0:v]select='%s',scale=270:-2,"+
		`drawtext=fontfile=fonts/roboto-700.ttf:text='%%{pts\:hms}':x=8:y=8:fontsize=20:fontcolor=white:box=1:boxcolor=black@0.65:boxborderw=6,`+
		"tile=%dx%d:padding=6:margin=6:color=0x111111[out]\n", strings.Join(sel, "+"), cols, rows)
	if err := os.WriteFile(filepath.Join(dir, "sheet.txt"), []byte(graph), 0o644); err != nil {
		return err
	}
	flag := "-/filter_complex"
	if t.Major < 7 {
		flag = "-filter_complex_script"
	}
	args := append(input(abs), flag, "sheet.txt", "-map", "[out]", "-fps_mode", "passthrough", "-frames:v", "1", "-q:v", "3", "-update", "1", "-y", out)
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("frame sheet failed: %s", lastLines(stderr.String(), 2))
	}
	return nil
}
