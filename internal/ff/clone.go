package ff

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
)

// Frames decodes the first maxSec seconds of a video as raw frames of w x h
// pixels at a constant fps and hands them to fn one at a time, in order.
// pixfmt is gray or rgb24. Only the frame being handled is in memory.
func (t *Tools) Frames(ctx context.Context, abs, dir string, w, h int, fps float64, maxSec float64, pixfmt string, fn func([]byte) error) error {
	bpp := 1
	if pixfmt == "rgb24" {
		bpp = 3
	}
	// Decoder threads each hold frames: on a 1080p source the default takes
	// hundreds of MB, and a laptop short of memory fails with -12.
	args := append([]string{"-threads", "2", "-filter_threads", "1"}, input(abs)...)
	args = append(args, "-t", strconv.FormatFloat(maxSec, 'f', 3, 64), "-map", "0:v:0", "-an", "-sn",
		"-vf", fmt.Sprintf("fps=%s,scale=%d:%d:flags=area,format=%s", strconv.FormatFloat(fps, 'f', 3, 64), w, h, pixfmt),
		"-f", "rawvideo", "-pix_fmt", pixfmt, "pipe:1")
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	rd := bufio.NewReaderSize(out, 1<<20)
	size := w * h * bpp
	var ferr error
	for {
		buf := make([]byte, size)
		if _, err := io.ReadFull(rd, buf); err != nil {
			break
		}
		if ferr = fn(buf); ferr != nil {
			break
		}
	}
	io.Copy(io.Discard, rd)
	werr := cmd.Wait()
	switch {
	case ferr != nil:
		return ferr
	case ctx.Err() != nil:
		return ctx.Err()
	case werr != nil:
		return fmt.Errorf("decoding frames failed: %s", lastLines(stderr.String(), 2))
	}
	return nil
}

// Still decodes the frame shown at time at, scaled to w x h, as RGB.
func (t *Tools) Still(ctx context.Context, abs, dir string, at float64, w, h int) ([]byte, error) {
	b, err := t.still(ctx, abs, dir, at, w, h)
	if err == nil && len(b) == 0 && at > 0 {
		// Past the last frame of a low frame rate video: the frame a second
		// earlier is the picture still showing.
		b, err = t.still(ctx, abs, dir, math.Max(0, at-1), w, h)
	}
	if err == nil && len(b) == 0 {
		return nil, fmt.Errorf("no frame at %.2f s", at)
	}
	return b, err
}

func (t *Tools) still(ctx context.Context, abs, dir string, at float64, w, h int) ([]byte, error) {
	// One frame needs one thread, see Frames.
	args := append([]string{"-threads", "1", "-filter_threads", "1", "-ss", strconv.FormatFloat(at, 'f', 3, 64)}, input(abs)...)
	args = append(args, "-map", "0:v:0", "-frames:v", "1", "-an", "-sn",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic,format=rgb24", w, h),
		"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	cmd := t.command(ctx, dir, args...)
	var stderr tail
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("reading the frame at %.2f s failed: %s", at, lastLines(stderr.String(), 2))
	}
	if len(b) < w*h*3 {
		return nil, nil
	}
	return b[:w*h*3], nil
}

// ExtractAudio copies the soundtrack of the first maxSec seconds into an
// .m4a file (re-encoding to AAC when the source codec cannot go in one).
func (t *Tools) ExtractAudio(ctx context.Context, abs, dir string, maxSec float64, out string) error {
	run := func(codec ...string) error {
		args := append(input(abs), "-t", strconv.FormatFloat(maxSec, 'f', 3, 64), "-map", "0:a:0", "-vn", "-sn")
		args = append(append(args, codec...), "-movflags", "+faststart", "-f", "mp4", "-y", out)
		cmd := t.command(ctx, dir, args...)
		var stderr tail
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%s", lastLines(stderr.String(), 2))
		}
		return nil
	}
	if err := run("-c:a", "copy"); err == nil {
		return nil
	}
	os.Remove(out)
	if err := run("-c:a", "aac", "-b:a", "192k"); err != nil {
		return fmt.Errorf("extracting the sound failed: %v", err)
	}
	return nil
}
