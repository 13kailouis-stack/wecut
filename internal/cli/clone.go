package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/clone"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

const (
	cloneMax     = 300.0 // seconds of a source that are cloned
	cloneHighFPS = 6.0
	stillShort   = 720 // short side of the full-size frames read for looks
)

// clone turns a finished video into a recipe with the original's exact
// cuts, transitions, framing, text cards, captions and logo.
//
//	wecut clone media/viral.mp4 [--name my-style] [--write recipes/my-style.json]
//	            [--read answers.json] [--ocr auto|off] [--fresh] [--force]
func (a *app) clone(args []string) int {
	var c common
	var name, write, outDir, readFile, ocr string
	var force, debug, fresh bool
	fs := flag.NewFlagSet("clone", flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&name, "name", "", "template name (default: the video's file name)")
	fs.StringVar(&write, "write", "", "recipe file to write (default recipes/<name>.json)")
	fs.StringVar(&outDir, "out", "", "folder for the slot sheet, text crops and comparison (default out/clone/<name>)")
	fs.StringVar(&readFile, "read", "", "answers from a model that looked at the review images, {\"text1\": {\"lines\": [...], \"font\": \"roboto\"}, \"slot2\": {\"about\": \"...\"}}; reuses the analysis of the last run")
	fs.BoolVar(&fresh, "fresh", false, "with --read: analyse the video again instead of reusing the last analysis")
	fs.StringVar(&ocr, "ocr", "auto", "read text crops with Tesseract when it is installed: auto or off")
	fs.BoolVar(&force, "force", false, "overwrite an existing recipe of that name")
	fs.BoolVar(&debug, "debug", false, "also write debug.json with every measurement (for tuning the detectors)")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one video file", Hint: "wecut clone media/viral.mp4 --name my-style"})
	}
	if ocr != "auto" && ocr != "off" {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "--ocr must be auto or off"})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	src := filepath.ToSlash(pos[0])
	abs, err := root.Input(src)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(src, "", err))
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	name = slugName(name)
	if write == "" {
		write = "recipes/" + name + ".json"
	}
	chosenOut := outDir != ""
	if outDir == "" {
		outDir = "out/clone/" + name
	}
	if !strings.EqualFold(filepath.Ext(write), ".json") {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "--write must be a .json file"})
	}
	recipeAbs, err := root.Output(write)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(write, "", err))
	}
	if _, err := os.Stat(recipeAbs); err == nil && !force {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_EXISTS", Message: write + " already exists", Hint: "Pick another --name, or pass --force to replace it."})
	}
	var given map[string]clone.Reading
	if readFile != "" {
		b, err := a.readJSON(root, readFile)
		if err != nil {
			return a.fail(exitInput, nil, pathIssue(readFile, "", err))
		}
		if given, err = parseReadings(b); err != nil {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_SCHEMA", Message: "--read: " + err.Error(), Hint: answerExample})
		}
	}

	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	info, err := t.Probe(a.ctx, abs)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(src, "", mediaErr{src, err}))
	}
	if info.Kind != "video" || info.Width == 0 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_MEDIA", Message: src + " is " + info.Kind + ", not a video"})
	}
	var warnings []spec.Issue
	dur := info.Duration
	if dur > cloneMax {
		warnings = append(warnings, spec.Issue{Code: "W_PARTIAL", Message: fmt.Sprintf("only the first %.0f s of %.0f s were cloned", cloneMax, dur)})
		dur = cloneMax
	}
	job, err := os.MkdirTemp("", "wecut-clone-")
	if err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	defer os.RemoveAll(job)
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(max(180, dur*8)*float64(time.Second)))
	defer cancel()
	started := time.Now()
	fail := func(err error) int {
		return a.fail(exitRender, warnings, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}

	outAbs, err := root.Output(filepath.Join(outDir, "x"))
	if err != nil {
		return a.fail(exitInput, warnings, pathIssue(outDir, "", err))
	}
	outAbs = filepath.Dir(outAbs)
	// The crops and sheets have common names, and old ones are cleared: never
	// in a folder of someone's own files.
	if chosenOut && !fileExists(filepath.Join(outAbs, sessionFile)) {
		if entries, err := os.ReadDir(outAbs); err == nil && len(entries) > 0 {
			return a.fail(exitInput, warnings, spec.Issue{Code: "E_EXISTS", Message: outDir + " already holds files that are not from a clone", Hint: "Pick a new folder for --out, or leave it out to use out/clone/" + name + "."})
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return fail(err)
	}
	// Full-size frames for colours and words.
	sw, sh := info.Width, info.Height
	if short := min(sw, sh); short > stillShort {
		k := float64(stillShort) / float64(short)
		sw, sh = evenInt(float64(sw)*k), evenInt(float64(sh)*k)
	}

	var an *clone.Analyzer
	var plan *clone.Plan
	var crops []clone.Crop
	reads := map[string]clone.Reading{}
	cropFiles := map[string]string{}
	ocrUsed := ""
	tried := false
	// Answers to the last run's review: the frames were already gone through.
	var sess *cloneSession
	if len(given) > 0 && !fresh && !debug {
		sess = loadSession(outAbs, abs)
	}
	if sess != nil {
		an, plan, crops, cropFiles, reads, ocrUsed, tried = clone.Resume(sess.Config, sess.Plan), sess.Plan, sess.Crops, sess.CropFiles, sess.Reads, sess.OCR, sess.Tried
		for i := range crops {
			crops[i].Image = readCrop(filepath.Join(outAbs, crops[i].ID+".png"))
		}
	} else {
		// Timing frames at the source's own rate (up to 60), so every cut
		// lands on the frame where it really is.
		lowFPS := math.Min(60, math.Round(info.FPS))
		if lowFPS < 10 {
			lowFPS = 30
		}
		cfg := clone.Config{W: info.Width, H: info.Height, Duration: dur, LowFPS: lowFPS, HighFPS: cloneHighFPS}
		an = clone.New(cfg)
		if debug {
			an.Debug()
		}
		lw, lh := clone.LowSize(info.Width, info.Height)
		if err := t.Frames(ctx, abs, job, lw, lh, lowFPS, dur, "gray", func(b []byte) error {
			an.AddLow(&clone.Gray{W: lw, H: lh, Pix: b})
			return nil
		}); err != nil {
			return fail(err)
		}
		hw, hh := clone.HighSize(info.Width, info.Height)
		if err := t.Frames(ctx, abs, job, hw, hh, cloneHighFPS, dur, "gray", func(b []byte) error {
			an.AddHigh(&clone.Gray{W: hw, H: hh, Pix: b})
			return nil
		}); err != nil {
			return fail(err)
		}
		if info.HasAudio {
			pcm, err := t.PCM(ctx, abs, job, analyzeRate, dur)
			if err != nil {
				return fail(err)
			}
			an.SetAudio(pcm, analyzeRate)
		}
		plan = an.Analyze()
		if len(plan.Shots) == 0 {
			return a.fail(exitInput, warnings, spec.Issue{Code: "E_MEDIA", Message: src + " is too short to clone: it has almost no frames", Hint: "Clone a video of at least a second."})
		}

		stills := make([]*clone.RGB, len(plan.Stills))
		for i, at := range plan.Stills {
			b, err := t.Still(ctx, abs, job, at, sw, sh)
			if err != nil {
				return fail(err)
			}
			stills[i] = &clone.RGB{W: sw, H: sh, Pix: b}
		}
		crops = an.Measure(stills)

		// Files from an earlier clone of the same name would mix with these.
		for _, pat := range []string{"text*.png", "caption*.png", "logo*.png", "slots.jpg", "transitions.jpg", "timeline.jpg", "compare.jpg", "debug.json", sessionFile} {
			old, _ := filepath.Glob(filepath.Join(outAbs, pat))
			for _, f := range old {
				os.Remove(f)
			}
		}

		// Words: what the caller gave comes on top of OCR, else placeholders.
		tess, _ := exec.LookPath("tesseract")
		tried = ocr != "off" && tess != ""
		for _, cr := range crops {
			f := filepath.Join(outAbs, cr.ID+".png")
			if err := writePNG(f, rgbImage(cr.Image, 1)); err == nil {
				cropFiles[cr.ID] = root.Rel(f)
			}
			if _, ok := given[cr.ID]; ok || !tried || cr.OCR == nil || cr.Kind == "logo" {
				continue
			}
			rd, err := tesseract(ctx, tess, job, cr)
			if err == nil && len(rd.Lines) > 0 {
				reads[cr.ID] = rd
				ocrUsed = "tesseract"
			}
		}
		{
			saveSession(outAbs, abs, &cloneSession{Config: cfg, Plan: plan, Crops: crops, CropFiles: cropFiles, Reads: reads, OCR: ocrUsed, Tried: tried})
		}
	}
	if problems := clone.CheckAnswers(plan, crops, given); len(problems) > 0 {
		return a.fail(exitInput, warnings, spec.Issue{Code: "E_SCHEMA", Message: "--read: " + strings.Join(problems, "; "), Hint: answerExample})
	}
	for id, rd := range given {
		reads[id] = rd
	}

	musicRel := ""
	hookMusic, hookLen := plan.HookSound(given)
	musicEnd := plan.ContentEnd
	if plan.Speech {
		musicEnd = hookLen
	}
	// A silent track is no soundtrack, however many seconds it runs.
	silent := plan.Audio != nil && plan.Audio.RMSdB < -60
	if info.HasAudio && !silent && (!plan.Speech || hookMusic) {
		musicRel = strings.TrimSuffix(write, filepath.Ext(write)) + "/music.m4a"
		mAbs, err := root.Output(musicRel)
		if _, statErr := os.Stat(mAbs); err == nil && sess != nil && statErr == nil {
			// Kept from the run being answered.
		} else if err == nil {
			os.MkdirAll(filepath.Dir(mAbs), 0o755)
			if err = t.ExtractAudio(ctx, abs, job, musicEnd, mAbs); err != nil {
				warnings = append(warnings, spec.Issue{Code: "W_AUDIO", Message: err.Error()})
				musicRel = ""
			}
		} else {
			musicRel = ""
		}
	}

	res := an.Build(clone.Options{Name: name, Source: src, Music: musicRel, Read: reads, Crops: crops, Tool: "wecut " + wecut.Version + " clone", Tried: tried})
	r := res.Recipe
	if issues := recipe.Validate(r); len(issues) > 0 {
		return a.fail(exitRender, warnings, append([]spec.Issue{{Code: "E_RENDER", Message: "the cloned recipe did not validate; this is a bug, please report it with the video"}}, issues...)...)
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(filepath.Dir(recipeAbs), 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(recipeAbs, append(data, '\n'), 0o644); err != nil {
		return fail(err)
	}

	// The slot sheet: one frame of every media slot, numbered.
	sheetRel, transRel, timelineRel := "", "", ""
	// Frames across the whole video: slots.jpg shows only each shot's middle,
	// and captions and effects live inside the shots.
	if f := filepath.Join(outAbs, "timeline.jpg"); sess != nil && fileExists(f) {
		timelineRel = root.Rel(f)
	} else if err := a.timelineSheet(ctx, t, abs, job, plan.ContentEnd, sw, sh, f); err == nil {
		timelineRel = root.Rel(f)
	} else {
		warnings = append(warnings, spec.Issue{Code: "W_SHEET", Message: err.Error()})
	}
	if f := filepath.Join(outAbs, "slots.jpg"); sess != nil && fileExists(f) {
		sheetRel = root.Rel(f)
	} else if len(res.Slots) > 0 {
		var times []float64
		for _, s := range res.Slots {
			times = append(times, s.Ref)
		}
		if err := a.slotSheet(ctx, t, abs, job, times, sw, sh, f); err == nil {
			sheetRel = root.Rel(f)
		} else {
			warnings = append(warnings, spec.Issue{Code: "W_SHEET", Message: err.Error()})
		}
	}
	// Frames through each transition, for eyes that can name its kind.
	if f := filepath.Join(outAbs, "transitions.jpg"); sess != nil && fileExists(f) {
		transRel = root.Rel(f)
	} else if rows := transitionRows(plan); len(rows) > 0 {
		if err := a.transitionSheet(ctx, t, abs, job, rows, sw, sh, f); err == nil {
			transRel = root.Rel(f)
		} else {
			warnings = append(warnings, spec.Issue{Code: "W_SHEET", Message: err.Error()})
		}
	}

	if d := an.DebugInfo(); d != nil {
		b, _ := json.Marshal(map[string]any{"debug": d, "plan": res.Plan})
		os.WriteFile(filepath.Join(outAbs, "debug.json"), b, 0o644)
	}
	compareRel, timing, err := a.compareSheet(ctx, c, root, t, src, res, info.Width, info.Height, outDir)
	if err != nil {
		warnings = append(warnings, spec.Issue{Code: "W_COMPARE", Message: "comparison sheet: " + err.Error()})
	}
	out := map[string]any{
		"file": root.Rel(recipeAbs), "name": name, "source": src, "duration": round2(plan.ContentEnd),
		"shots": len(plan.Shots), "slots": res.Slots, "events": plan.Events,
		"texts": textSummary(res, cropFiles), "hints": res.Hints, "clone_seconds": round2(time.Since(started).Seconds()),
		"next": fmt.Sprintf("wecut recipe show %s, then wecut recipe apply --input request.json (clips per slot, texts, transcript)", name),
	}
	if res.Plan.Captions != nil {
		out["captions"] = res.Plan.Captions
	}
	if len(res.Plan.Logos) > 0 {
		out["logos"] = res.Plan.Logos
	}
	if r.Music != nil {
		out["music"] = r.Music.Src
	}
	if sheetRel != "" {
		out["sheet"] = sheetRel
	}
	if compareRel != "" {
		out["compare"] = compareRel
	}
	if timing != nil {
		out["timing"] = timing
		if timing.Matched < timing.Events || timing.MaxMs > math.Max(200, 2*timing.FrameMs) {
			warnings = append(warnings, spec.Issue{Code: "W_TIMING", Message: fmt.Sprintf("Template timing matched %d/%d events; worst measured difference %.0f ms.", timing.Matched, timing.Events, timing.MaxMs), Hint: "Inspect the transition sheet and comparison. Matched events are approximate, not proof of frame-exact reproduction."})
		}
	}
	if ocrUsed != "" {
		out["ocr"] = ocrUsed
	}
	if sess != nil {
		out["analysis_reused"] = true
	}
	out["review"] = buildReview(res, crops, cropFiles, given, timelineRel, sheetRel, transRel, compareRel, src, name)
	// A measured recipe is not a finished adaptation. Expose artifacts by
	// purpose so agents do not deliver the placeholder comparison as footage.
	out["status"] = "needs_visual_review"
	if compareRel == "" {
		out["status"] = "comparison_failed"
	}
	if preview := filepath.Join(outAbs, "preview.json"); fileExists(preview) {
		out["template_preview_project"] = root.Rel(preview)
	}
	out["reference_video"] = src
	out["next"] = fmt.Sprintf("Review images and correct with --read; then apply %s to replacement clips using wecut recipe apply --input request.json. template_preview_project uses stand-ins, not replacement footage.", root.Rel(recipeAbs))
	if len(plan.Notes) > 0 {
		out["notes"] = plan.Notes
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return a.ok(out)
}

func textSummary(res *clone.Result, files map[string]string) []map[string]any {
	var out []map[string]any
	for _, bl := range res.Plan.Blocks {
		if bl.Look == nil || bl.Slot == "" {
			continue
		}
		m := map[string]any{"id": bl.ID, "slot": bl.Slot, "start": bl.Start, "end": bl.End, "lines": len(bl.Lines), "look": bl.Look}
		if bl.Words != "" {
			m["words"] = bl.Words
		}
		if f := files[bl.ID]; f != "" {
			m["crop"] = f
		}
		out = append(out, m)
	}
	return out
}

func parseReadings(b []byte) (map[string]clone.Reading, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &raw); err != nil {
		return nil, err
	}
	out := map[string]clone.Reading{}
	for k, v := range raw {
		var lines []string
		var s string
		var rd clone.Reading
		switch {
		case json.Unmarshal(v, &lines) == nil:
			rd.Lines = lines
		case json.Unmarshal(v, &s) == nil:
			rd.Lines = strings.Split(s, "\n")
		default:
			dec := json.NewDecoder(bytes.NewReader(v))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&rd); err != nil {
				return nil, fmt.Errorf("%s: give a list of lines, a string, or an object: %v", k, err)
			}
		}
		rd.Given = true
		out[k] = rd
	}
	return out, nil
}

// tesseract reads a crop: the black-on-white version, enlarged so letters
// are about 40 px tall, one block of text (or one line for captions).
func tesseract(ctx context.Context, bin, dir string, cr clone.Crop) (clone.Reading, error) {
	scale := 1
	if h := cr.Rect.H; h > 0 && h < 120 {
		scale = int(math.Ceil(120 / float64(h)))
	}
	scale = min(scale, 4)
	in := filepath.Join(dir, "ocr-"+cr.ID+".png")
	if err := writePNG(in, grayImage(cr.OCR, scale)); err != nil {
		return clone.Reading{}, err
	}
	psm := "6"
	if cr.Kind == "caption" {
		psm = "7"
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, in, "stdout", "--psm", psm, "-l", "eng", "tsv")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return clone.Reading{}, err
	}
	var rd clone.Reading
	var confSum float64
	lineOf := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 12 || f[0] != "5" {
			continue
		}
		conf, _ := strconv.ParseFloat(f[10], 64)
		// Box edges read as bars and brackets at the ends of a line.
		word := strings.Trim(strings.TrimSpace(f[11]), "|[]{}")
		if word == "" || conf < 30 || (len([]rune(word)) == 1 && conf < 70) {
			continue
		}
		key := f[2] + "/" + f[3] + "/" + f[4]
		n, ok := lineOf[key]
		if !ok {
			n = len(rd.Lines)
			lineOf[key] = n
			rd.Lines = append(rd.Lines, "")
		}
		if rd.Lines[n] != "" {
			rd.Lines[n] += " "
		}
		rd.Lines[n] += word
		x, _ := strconv.Atoi(f[6])
		y, _ := strconv.Atoi(f[7])
		w, _ := strconv.Atoi(f[8])
		h, _ := strconv.Atoi(f[9])
		rd.Words = append(rd.Words, clone.Word{Text: word, X: x / scale, Y: y / scale, W: w / scale, H: h / scale})
		confSum += conf
	}
	if len(rd.Words) > 0 {
		rd.Conf = round2(confSum / float64(len(rd.Words)))
	}
	return rd, nil
}

// slotSheet tiles one frame per slot, numbered, four across.
func (a *app) slotSheet(ctx context.Context, t interface {
	Still(context.Context, string, string, float64, int, int) ([]byte, error)
}, abs, dir string, times []float64, w, h int, out string) error {
	tw := 220
	th := int(math.Round(float64(h) * float64(tw) / float64(w)))
	cols := min(4, len(times))
	rows := (len(times) + cols - 1) / cols
	pad := 6
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(tw+pad)+pad, rows*(th+pad)+pad))
	for i := range sheet.Pix {
		sheet.Pix[i] = 0x16
		if i%4 == 3 {
			sheet.Pix[i] = 255
		}
	}
	for i, at := range times {
		b, err := t.Still(ctx, abs, dir, at, tw, th)
		if err != nil {
			return err
		}
		x0, y0 := pad+(i%cols)*(tw+pad), pad+(i/cols)*(th+pad)
		for y := 0; y < th; y++ {
			for x := 0; x < tw; x++ {
				j := 3 * (y*tw + x)
				sheet.Set(x0+x, y0+y, color.RGBA{b[j], b[j+1], b[j+2], 255})
			}
		}
		drawNumber(sheet, x0+6, y0+6, i+1)
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, sheet, &jpeg.Options{Quality: 85})
}

// drawNumber writes n in a small blocky font on a dark badge, so the sheet
// needs no font files.
func drawNumber(img *image.RGBA, x, y, n int) {
	digits := strconv.Itoa(n)
	glyphs := map[rune][5]string{
		'0': {"111", "101", "101", "101", "111"}, '1': {"010", "110", "010", "010", "111"},
		'2': {"111", "001", "111", "100", "111"}, '3': {"111", "001", "111", "001", "111"},
		'4': {"101", "101", "111", "001", "001"}, '5': {"111", "100", "111", "001", "111"},
		'6': {"111", "100", "111", "101", "111"}, '7': {"111", "001", "010", "010", "010"},
		'8': {"111", "101", "111", "101", "111"}, '9': {"111", "101", "111", "001", "111"},
	}
	s := 4
	w := len(digits)*4*s + 2*s
	for yy := 0; yy < 7*s; yy++ {
		for xx := 0; xx < w; xx++ {
			img.Set(x+xx, y+yy, color.RGBA{0, 0, 0, 200})
		}
	}
	for k, r := range digits {
		g := glyphs[r]
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if g[row][col] == '1' {
					for dy := 0; dy < s; dy++ {
						for dx := 0; dx < s; dx++ {
							img.Set(x+s+k*4*s+col*s+dx, y+s+row*s+dy, color.RGBA{255, 255, 255, 255})
						}
					}
				}
			}
		}
	}
}

func rgbImage(p *clone.RGB, scale int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, p.W*scale, p.H*scale))
	for y := 0; y < p.H*scale; y++ {
		for x := 0; x < p.W*scale; x++ {
			i := 3 * ((y/scale)*p.W + x/scale)
			img.Set(x, y, color.RGBA{p.Pix[i], p.Pix[i+1], p.Pix[i+2], 255})
		}
	}
	return img
}

func grayImage(g *clone.Gray, scale int) image.Image {
	img := image.NewGray(image.Rect(0, 0, g.W*scale+20, g.H*scale+20))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 0; y < g.H*scale; y++ {
		for x := 0; x < g.W*scale; x++ {
			img.SetGray(x+10, y+10, color.Gray{g.Pix[(y/scale)*g.W+x/scale]})
		}
	}
	return img
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func evenInt(v float64) int { return max(2, int(math.Round(v/2))*2) }
