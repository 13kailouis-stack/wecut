package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/13kailouis-stack/wecut/internal/assets"
	"github.com/13kailouis-stack/wecut/internal/clone"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/ff"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

// compareSheet renders the cloned template filled with numbered stand-in
// pictures (one per media slot, the logo as a white box, sample caption
// words) and sets its frames beside the original's at the same moments:
// original on the left, template on the right. The template keeps the
// original's timeline, so the same second shows the same slot.
func (a *app) compareSheet(ctx context.Context, c common, root *workspace.Root, t *ff.Tools, src string, res *clone.Result, srcW, srcH int, outRel string) (string, error) {
	r := res.Recipe
	plan := res.Plan
	phRel := filepath.ToSlash(filepath.Join(outRel, "placeholders"))
	phAbs, err := root.Output(phRel + "/x")
	if err != nil {
		return "", err
	}
	phAbs = filepath.Dir(phAbs)
	if err := os.MkdirAll(phAbs, 0o755); err != nil {
		return "", err
	}

	// One stand-in per shot, shaped like the footage the slot shows.
	var in recipe.Input
	in.Fill, in.Music = "each", "none"
	k := 0
	for _, sec := range r.Sections {
		for range sec.Shots {
			w, h := srcW, srcH
			if k < len(plan.Shots) && plan.Shots[k].Fit == "contain" {
				cb := plan.Shots[k].Content
				w, h = int(cb.W*float64(srcW)), int(cb.H*float64(srcH))
			}
			// Small: the comparison renders at 540p, and a dozen looping
			// pictures at full size use a lot of memory.
			if short := min(w, h); short > 320 {
				w, h = w*320/short, h*320/short
			}
			name := fmt.Sprintf("%s/slot-%d.png", phRel, k+1)
			if err := writePNG(filepath.Join(phAbs, fmt.Sprintf("slot-%d.png", k+1)), slate(w, h, k+1)); err != nil {
				return "", err
			}
			in.Clips = append(in.Clips, recipe.Clip{Src: name, Role: sec.Role})
			k++
		}
	}
	for _, o := range r.Overlays {
		lw, lh := 200, 60
		for _, lg := range plan.Logos {
			if lg.ID == o.Slot {
				lw, lh = max(8, int(lg.Box.W*float64(srcW)*1.3)), max(4, int(lg.Box.H*float64(srcH)))
			}
		}
		name := fmt.Sprintf("%s/%s.png", phRel, o.Slot)
		if err := writePNG(filepath.Join(phAbs, o.Slot+".png"), logoBox(lw, lh)); err != nil {
			return "", err
		}
		if in.Overlays == nil {
			in.Overlays = map[string]string{}
		}
		in.Overlays[o.Slot] = name
	}

	var uses []use
	for i, cl := range in.Clips {
		uses = append(uses, use{cl.Src, fmt.Sprintf("/clips/%d/src", i)})
	}
	for _, s := range recipe.OverlaySources(r, in) {
		uses = append(uses, use{s, "/overlays"})
	}
	set, errs := a.probeAll(root, t, uses, map[string]bool{})
	if len(errs) > 0 {
		return "", fmt.Errorf("%s", errs[0].Message)
	}
	p, _, errs := recipe.Apply(r, in, set)
	if len(errs) > 0 {
		return "", fmt.Errorf("%s: %s", errs[0].Path, errs[0].Message)
	}
	// Sample words where the captions ran, so their place and size show.
	if cs := r.Captions; cs != nil && plan.Captions != nil {
		var words []map[string]any
		sample := []string{"CAPTION", "WORDS", "HERE", "LIKE", "THIS"}
		step := 0.45
		if cs.Mode == "chunk" {
			step = 0.3
		}
		for i, at := 0, plan.Captions.Start; at < plan.Captions.End-0.1; i, at = i+1, at+step {
			words = append(words, map[string]any{"word": sample[i%len(sample)], "start": round2(at), "end": round2(at + step - 0.02)})
		}
		raw, _ := json.Marshal(words)
		p.Captions = &spec.Captions{Words: raw, Mode: cs.Mode, Highlight: cs.Highlight, MaxWords: cs.MaxWords, MaxDuration: cs.MaxDuration, X: cs.X, Y: cs.Y, Look: cs.Look}
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	// Kept next to the sheet: `wecut render` it to watch the template.
	if pa, err := root.Output(filepath.Join(outRel, "preview.json")); err == nil {
		os.WriteFile(pa, append(data, '\n'), 0o644)
	}
	l, issues, _ := a.prepare(c, root, data, "compare")
	if len(issues) > 0 {
		return "", fmt.Errorf("%s: %s", issues[0].Path, issues[0].Message)
	}

	// The moments: the middle of each slot, at most nine of them.
	var times []float64
	n := len(res.Slots)
	for i := 0; i < min(n, 9); i++ {
		s := res.Slots[int(math.Round(float64(i)*float64(n-1)/math.Max(1, float64(min(n, 9)-1))))]
		mid := s.Start + s.Dur/2
		if len(times) == 0 || mid-times[len(times)-1] > 0.2 {
			times = append(times, mid)
		}
	}
	frames, got, job, err := a.previewFrames(ctx, l, times)
	if job != "" {
		defer os.RemoveAll(job)
	}
	if err != nil {
		return "", err
	}
	abs, _ := root.Input(src)

	const tw = 216
	th := int(math.Round(float64(tw) * float64(srcH) / float64(srcW)))
	pad, gapPair := 6, 3
	cols := min(3, len(got))
	rows := (len(got) + cols - 1) / cols
	pw := 2*tw + gapPair
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(pw+3*pad)+pad, rows*(th+pad)+pad))
	for i := range sheet.Pix {
		sheet.Pix[i] = 0x16
		if i%4 == 3 {
			sheet.Pix[i] = 255
		}
	}
	for i, at := range got {
		x0, y0 := pad+(i%cols)*(pw+3*pad), pad+(i/cols)*(th+pad)
		b, err := t.Still(ctx, abs, os.TempDir(), at, tw, th)
		if err != nil {
			return "", err
		}
		for y := 0; y < th; y++ {
			for x := 0; x < tw; x++ {
				j := 3 * (y*tw + x)
				sheet.Set(x0+x, y0+y, color.RGBA{b[j], b[j+1], b[j+2], 255})
			}
		}
		f, err := os.Open(frames[i])
		if err != nil {
			return "", err
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			return "", err
		}
		bd := img.Bounds()
		for y := 0; y < th; y++ {
			for x := 0; x < tw; x++ {
				sheet.Set(x0+tw+gapPair+x, y0+y, img.At(bd.Min.X+x*bd.Dx()/tw, bd.Min.Y+y*bd.Dy()/th))
			}
		}
	}
	outAbs, err := root.Output(filepath.Join(outRel, "compare.jpg"))
	if err != nil {
		return "", err
	}
	f, err := os.Create(outAbs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := jpeg.Encode(f, sheet, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	return root.Rel(outAbs), nil
}

// previewFrames renders still frames of a loaded edit at the given output
// times into a temporary folder and returns their paths and actual times.
func (a *app) previewFrames(ctx context.Context, l *loaded, times []float64) (files []string, got []float64, job string, err error) {
	caps, err := l.tools.Capabilities(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	if job, err = os.MkdirTemp("", "wecut-compare-"); err != nil {
		return nil, nil, "", err
	}
	opt := engine.Options{FFmpegMajor: l.tools.Major, Frames: times, ResOverride: 540, FramePath: filepath.Join(job, "f-%02d.jpg")}
	plan, err := engine.Compile(l.edit, opt)
	if err != nil {
		return nil, nil, job, err
	}
	if m := caps.MissingFor(plan.Graph); len(m) > 0 {
		return nil, nil, job, fmt.Errorf("this FFmpeg build lacks %s", ff.Explain(m))
	}
	if err := os.WriteFile(filepath.Join(job, "graph.txt"), []byte(plan.Graph), 0o644); err != nil {
		return nil, nil, job, err
	}
	if plan.ASS != "" {
		if err := os.WriteFile(filepath.Join(job, "wecut.ass"), []byte(plan.ASS), 0o644); err != nil {
			return nil, nil, job, err
		}
		if err := assets.WriteFonts(filepath.Join(job, "fonts"), plan.Fonts); err != nil {
			return nil, nil, job, err
		}
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(max(120, 10*l.edit.Total)*float64(time.Second)))
	defer cancel()
	if err := l.tools.Run(cctx, ff.Job{Dir: job, Args: plan.Args, Duration: plan.Duration}); err != nil {
		return nil, nil, job, err
	}
	got = engine.FrameTimes(times, l.edit.Total, l.edit.FPS)
	for i := range got {
		files = append(files, filepath.Join(job, fmt.Sprintf("f-%02d.jpg", i+1)))
	}
	return files, got, job, nil
}

// slate is a stand-in picture for slot n: a muted colour with its number.
func slate(w, h, n int) image.Image {
	w, h = max(16, w), max(16, h)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	hues := [][3]uint8{{58, 76, 110}, {96, 64, 104}, {54, 98, 86}, {112, 84, 52}, {70, 70, 70}, {104, 56, 64}}
	c := hues[(n-1)%len(hues)]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := c
			if (x/(max(8, w/12))+y/(max(8, w/12)))%2 == 0 {
				v = [3]uint8{c[0] + 10, c[1] + 10, c[2] + 10}
			}
			img.Set(x, y, color.RGBA{v[0], v[1], v[2], 255})
		}
	}
	scale := max(4, min(w, h)/14)
	big := image.NewRGBA(image.Rect(0, 0, w, h))
	drawNumberScaled(big, w/2, h/2, n, scale)
	for i := 0; i < len(big.Pix); i += 4 {
		if big.Pix[i+3] > 0 {
			copy(img.Pix[i:i+4], big.Pix[i:i+4])
		}
	}
	return img
}

// logoBox stands in for a logo: a white rounded box.
func logoBox(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := float64(min(w, h)) / 4
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := math.Max(0, math.Max(r-float64(x), float64(x)-(float64(w-1)-r)))
			dy := math.Max(0, math.Max(r-float64(y), float64(y)-(float64(h-1)-r)))
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, color.NRGBA{255, 255, 255, 230})
			}
		}
	}
	return img
}

// drawNumberScaled draws n centred on (cx, cy) in the blocky digits of
// drawNumber, s pixels per dot.
func drawNumberScaled(img *image.RGBA, cx, cy, n, s int) {
	tmp := image.NewRGBA(image.Rect(0, 0, 400, 60))
	drawNumber(tmp, 0, 0, n)
	// drawNumber uses 4 px dots; copy its white dots at the new size.
	bw := 0
	for x := 0; x < 400; x++ {
		for y := 0; y < 28; y++ {
			if tmp.RGBAAt(x, y).R == 255 {
				bw = max(bw, x+1)
			}
		}
	}
	k := float64(s) / 4
	ox, oy := cx-int(float64(bw)*k/2), cy-int(28*k/2)
	for y := 0; y < int(28*k); y++ {
		for x := 0; x < int(float64(bw)*k); x++ {
			if tmp.RGBAAt(int(float64(x)/k), int(float64(y)/k)).R == 255 {
				img.Set(ox+x, oy+y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
}
