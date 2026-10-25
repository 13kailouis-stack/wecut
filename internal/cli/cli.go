// Package cli is the `wecut` command. Every command prints one JSON object
// on stdout, so agents never have to scrape text. Exit codes:
// 0 ok, 2 invalid input, 3 missing dependency, 4 render failed, 5 over a limit.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/assets"
	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/ff"
	"github.com/13kailouis-stack/wecut/internal/media"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

const (
	exitOK      = 0
	exitInput   = 2
	exitDep     = 3
	exitRender  = 4
	exitLimit   = 5
	maxProjSize = 10 << 20
)

type app struct {
	ctx    context.Context
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// Run executes one command and returns the exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{ctx, stdin, stdout, stderr}
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "version", "--version":
		return a.ok(map[string]any{"wecut": wecut.Version, "format": spec.Version, "os": runtime.GOOS, "arch": runtime.GOARCH})
	case "doctor":
		return a.doctor(rest)
	case "probe":
		return a.probe(rest)
	case "analyze":
		return a.analyze(rest)
	case "recipe":
		return a.recipe(rest)
	case "validate":
		return a.validate(rest)
	case "render":
		return a.render(rest, false)
	case "preview":
		return a.render(rest, true)
	case "schema":
		_, err := stdout.Write(wecut.Schema)
		if err != nil {
			return exitInput
		}
		return exitOK
	case "guide":
		// Markdown, not JSON: it is read by a model, like `schema` is.
		if _, err := stdout.Write(wecut.Guide()); err != nil {
			return exitInput
		}
		return exitOK
	case "list":
		return a.list(rest)
	case "example":
		return a.example(rest)
	}
	return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: fmt.Sprintf("unknown command %q", cmd), Hint: "Commands: guide, doctor, probe, analyze, recipe, validate, preview, render, schema, list, example, mcp, version. Run `wecut help`."})
}

const usage = `wecut: video editing engine for AI agents. Renders on this machine with FFmpeg.

Usage:
  wecut guide                       how to use WeCut, for agents: workflow, format, templates (start here)
  wecut doctor                      check FFmpeg, encoders and filters
  wecut probe <file>...             duration, size, fps and audio of media files
  wecut analyze <video>             cuts, pace, hook, beat, pauses + a frame sheet (--transcript words.json)
  wecut recipe list|show|apply|from-project   templates: the shape of an edit, applied to new clips
  wecut recipe pack <recipe> --write template.wecut.json   include default music for another device
  wecut recipe unpack <bundle> --write templates/name      import into a new directory
  wecut validate <project.json>     check a project without rendering
  wecut preview <project.json>      render a few still frames to look at (--at 1,5,9)
  wecut render <project.json>       render the video (--preset draft|final|high)
  wecut schema                      JSON Schema of the project format
  wecut list [what]                 styles, caption-styles, fonts, transitions, filters, effects, motions, ratios
  wecut example [name]              print an example project
  wecut mcp                         run as an MCP server on stdin/stdout (--root fixes the workspace)
  wecut version

Common flags:
  --root DIR       workspace; every path in a project is relative to it (default: current folder)
  --ffmpeg PATH    FFmpeg binary (or WECUT_FFMPEG); --ffprobe PATH (or WECUT_FFPROBE)

Render flags:
  --preset draft|final|high   draft = 540p fast check, final = default, high = slower and sharper
  --out FILE        override output.file
  --encoder NAME    libx264 (default when present), h264_nvenc, h264_qsv, h264_videotoolbox...
  --progress        progress lines as JSON on stderr
  --timeout SEC     stop after this many seconds (default: scales with video length)
  --dry-run         print the FFmpeg plan without running it

Use "-" as the project path to read the project from stdin. Output is one JSON object on stdout
(guide and schema print a document instead).
Exit codes: 0 ok, 2 invalid input, 3 missing dependency, 4 render failed, 5 over a limit.
`

type common struct {
	root, ffmpeg, ffprobe string
}

func (c *common) bind(fs *flag.FlagSet) {
	fs.StringVar(&c.root, "root", "", "workspace folder")
	fs.StringVar(&c.ffmpeg, "ffmpeg", "", "ffmpeg binary")
	fs.StringVar(&c.ffprobe, "ffprobe", "", "ffprobe binary")
}

// parse accepts flags before and after positional arguments, which is how
// agents tend to write commands.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func (a *app) out(v any) {
	b, _ := json.Marshal(v)
	a.stdout.Write(append(b, '\n'))
}

func (a *app) ok(v map[string]any) int {
	v["ok"] = true
	a.out(v)
	return exitOK
}

func (a *app) fail(code int, warnings []spec.Issue, errs ...spec.Issue) int {
	res := map[string]any{"ok": false, "errors": errs}
	if len(warnings) > 0 {
		res["warnings"] = warnings
	}
	a.out(res)
	return code
}

func usageErr(err error) spec.Issue {
	return spec.Issue{Code: "E_USAGE", Message: err.Error(), Hint: "Run `wecut help`."}
}

// exitFor maps the first issue code to an exit code.
func exitFor(issues []spec.Issue) int {
	if len(issues) == 0 {
		return exitInput
	}
	switch issues[0].Code {
	case "E_LIMIT":
		return exitLimit
	case "E_DEPENDENCY":
		return exitDep
	case "E_RENDER", "E_VERIFY", "E_TIMEOUT":
		return exitRender
	}
	return exitInput
}

func (a *app) tools(c common) (*ff.Tools, *spec.Issue) {
	t, err := ff.Find(a.ctx, c.ffmpeg, c.ffprobe)
	if err != nil {
		return nil, &spec.Issue{Code: "E_DEPENDENCY", Message: err.Error(), Hint: ff.InstallHint()}
	}
	if t.Major < 6 {
		return nil, &spec.Issue{Code: "E_DEPENDENCY", Message: fmt.Sprintf("FFmpeg %s is too old; WeCut needs 6.0 or newer", t.Version), Hint: ff.InstallHint()}
	}
	return t, nil
}

// ── doctor ────────────────────────────────────────────────────────────────

func (a *app) doctor(args []string) int {
	var c common
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	c.bind(fs)
	if _, err := parse(fs, args); err != nil {
		return a.fail(exitInput, nil, usageErr(err))
	}
	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	caps, err := t.Capabilities(a.ctx)
	if err != nil {
		return a.fail(exitDep, nil, spec.Issue{Code: "E_DEPENDENCY", Message: "cannot list FFmpeg features: " + err.Error()})
	}
	res := map[string]any{
		"wecut":               wecut.Version,
		"ffmpeg":              map[string]any{"path": t.FFmpeg, "version": t.Version},
		"ffprobe":             map[string]any{"path": t.FFprobe},
		"encoders":            caps.Encoders,
		"filters":             caps.Filters,
		"default_encoder":     caps.DefaultEncoder(),
		"cpus":                runtime.NumCPU(),
		"os":                  runtime.GOOS + "/" + runtime.GOARCH,
		"local_transcription": caps.Filters["whisper"],
	}
	var problems []spec.Issue
	if m := caps.Missing(); len(m) > 0 {
		problems = append(problems, spec.Issue{Code: "E_DEPENDENCY", Message: "this FFmpeg build lacks filters WeCut needs: " + ff.Explain(m), Hint: "Use a full FFmpeg build. " + ff.InstallHint()})
	}
	if caps.DefaultEncoder() == "" {
		problems = append(problems, spec.Issue{Code: "E_DEPENDENCY", Message: "no H.264 encoder found", Hint: ff.InstallHint()})
	}
	if len(problems) > 0 {
		res["ok"] = false
		res["errors"] = problems
		a.out(res)
		return exitDep
	}
	if !caps.Filters["ass"] {
		res["warnings"] = []spec.Issue{{Code: "W_FEATURE", Message: "this FFmpeg has no libass: projects with texts or captions will not render", Hint: ff.InstallHint()}}
	}
	return a.ok(res)
}

// ── probe ─────────────────────────────────────────────────────────────────

func (a *app) probe(args []string) int {
	var c common
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	c.bind(fs)
	files, err := parse(fs, args)
	if err != nil {
		return a.fail(exitInput, nil, usageErr(err))
	}
	if len(files) == 0 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give at least one file", Hint: "wecut probe media/clip.mp4"})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	var infos []media.Info
	var errs []spec.Issue
	for _, f := range files {
		abs, err := root.Input(f)
		if err != nil {
			errs = append(errs, pathIssue(f, "", err))
			continue
		}
		info, err := t.Probe(a.ctx, abs)
		if err != nil {
			errs = append(errs, spec.Issue{Code: "E_MEDIA", Path: f, Message: err.Error()})
			continue
		}
		info.Rel = f
		infos = append(infos, info)
	}
	if len(errs) > 0 {
		res := map[string]any{"ok": false, "errors": errs, "files": infos}
		a.out(res)
		return exitInput
	}
	return a.ok(map[string]any{"files": infos})
}

// mediaErr is a file that exists but FFprobe cannot read.
type mediaErr struct {
	src string
	err error
}

func (m mediaErr) Error() string { return fmt.Sprintf("%s cannot be read as media (%v)", m.src, m.err) }

func pathIssue(src, ptr string, err error) spec.Issue {
	code := "E_MEDIA"
	hint := "Paths are relative to the workspace (--root)."
	var me mediaErr
	switch {
	case errors.Is(err, workspace.ErrOutside):
		code = "E_PATH"
		hint = "Only files inside the workspace can be used. Copy the file in, or pass --root."
	case errors.As(err, &me):
		hint = "Check that it is a complete video, image or audio file. `wecut probe " + src + "` shows what FFmpeg sees."
	}
	return spec.Issue{Code: code, Path: ptr, Message: err.Error(), Hint: hint}
}

// ── validate / render / preview ──────────────────────────────────────────

type loaded struct {
	root  *workspace.Root
	tools *ff.Tools
	edit  *engine.Edit
	name  string // project file stem, for preview names
	used  map[string]bool
}

// load runs everything up to a resolved edit: parse, static checks, path
// checks, probing, transcript parsing and timeline layout.
func (a *app) load(c common, projectArg string) (*loaded, int) {
	root, err := workspace.Open(c.root)
	if err != nil {
		return nil, a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	var data []byte
	name := "wecut"
	if projectArg == "-" {
		data, err = io.ReadAll(io.LimitReader(a.stdin, maxProjSize+1))
	} else {
		var abs string
		abs, err = root.Input(projectArg)
		if err == nil {
			var st os.FileInfo
			if st, err = os.Stat(abs); err == nil && st.Size() > maxProjSize {
				err = fmt.Errorf("project file is larger than 10 MB")
			} else if err == nil {
				data, err = os.ReadFile(abs)
			}
		}
		name = strings.TrimSuffix(filepath.Base(projectArg), filepath.Ext(projectArg))
	}
	if err != nil {
		return nil, a.fail(exitInput, nil, pathIssue(projectArg, "", err))
	}
	if len(data) > maxProjSize {
		return nil, a.fail(exitLimit, nil, spec.Issue{Code: "E_LIMIT", Message: "project is larger than 10 MB"})
	}
	l, issues, code := a.prepare(c, root, data, name)
	if len(issues) > 0 {
		return nil, a.fail(code, nil, issues...)
	}
	return l, exitOK
}

// use is one place a project names a file.
type use struct{ src, ptr string }

// probeAll probes each distinct file once and reports problems at every
// place it is used. used collects the absolute paths read.
func (a *app) probeAll(root *workspace.Root, t *ff.Tools, uses []use, used map[string]bool) (media.Set, []spec.Issue) {
	set := media.Set{}
	failed := map[string]error{}
	var errs []spec.Issue
	for _, u := range uses {
		if _, ok := set[u.src]; ok {
			continue
		}
		if err, ok := failed[u.src]; ok {
			errs = append(errs, pathIssue(u.src, u.ptr, err))
			continue
		}
		abs, err := root.Input(u.src)
		if err == nil {
			var info media.Info
			info, err = t.Probe(a.ctx, abs)
			if err == nil {
				info.Rel = u.src
				set[u.src] = info
				used[abs] = true
				continue
			}
			err = mediaErr{u.src, err}
		}
		failed[u.src] = err
		errs = append(errs, pathIssue(u.src, u.ptr, err))
	}
	return set, errs
}

// prepare runs everything after reading a project: parse, checks, probing,
// transcript and timeline. It returns problems instead of printing them, so
// commands that build projects (recipe apply) can check what they made.
func (a *app) prepare(c common, root *workspace.Root, data []byte, name string) (*loaded, []spec.Issue, int) {
	p, issues := spec.Decode(data)
	if len(issues) > 0 {
		return nil, issues, exitInput
	}
	if issues = spec.Validate(p); len(issues) > 0 {
		return nil, issues, exitFor(issues)
	}
	t, iss := a.tools(c)
	if iss != nil {
		return nil, []spec.Issue{*iss}, exitDep
	}
	var uses []use
	for i, cl := range p.Clips {
		uses = append(uses, use{cl.Src, fmt.Sprintf("/clips/%d/src", i)})
	}
	for i, o := range p.Overlays {
		uses = append(uses, use{o.Src, fmt.Sprintf("/overlays/%d/src", i)})
	}
	for i, tr := range p.Audio {
		uses = append(uses, use{tr.Src, fmt.Sprintf("/audio/%d/src", i)})
	}
	used := map[string]bool{}
	set, errs := a.probeAll(root, t, uses, used)

	var words []engine.Word
	if cp := p.Captions; cp != nil {
		raw := strings.TrimSpace(string(cp.Words))
		var tw []engine.Word
		var werr error
		if strings.HasPrefix(raw, "\"") {
			var rel string
			_ = json.Unmarshal(cp.Words, &rel)
			abs, err := root.Input(rel)
			if err != nil {
				errs = append(errs, pathIssue(rel, "/captions/words", err))
			} else {
				b, err := os.ReadFile(abs)
				if err == nil {
					tw, werr = engine.ParseTranscript(b, filepath.Ext(rel))
				} else {
					werr = err
				}
				used[abs] = true
			}
		} else {
			tw, werr = engine.ParseTranscript(cp.Words, ".json")
		}
		if werr != nil {
			errs = append(errs, spec.Issue{Code: "E_CAPTIONS", Path: "/captions/words", Message: werr.Error(), Hint: "Use an array of {\"word\": \"hi\", \"start\": 0.1, \"end\": 0.4} in seconds, Whisper JSON, SRT or VTT."})
		}
		words = tw
	}
	if len(errs) > 0 {
		return nil, errs, exitInput
	}
	e, issues := engine.Resolve(p, set, words)
	if len(issues) > 0 {
		return nil, issues, exitFor(issues)
	}
	return &loaded{root: root, tools: t, edit: e, name: name, used: used}, nil, exitOK
}

func (a *app) validate(args []string) int {
	var c common
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	c.bind(fs)
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one project file", Hint: "wecut validate project.json"})
	}
	l, code := a.load(c, pos[0])
	if l == nil {
		return code
	}
	e := l.edit
	res := map[string]any{"duration": e.Total, "width": e.W, "height": e.H, "fps": e.FPS, "clips": len(e.Clips), "output": e.OutFile}
	if e.P.Captions != nil {
		res["caption_words"] = len(e.Words)
	}
	if len(e.Warnings) > 0 {
		res["warnings"] = e.Warnings
	}
	return a.ok(res)
}

func (a *app) render(args []string, preview bool) int {
	var c common
	var preset, outFlag, encoder, at string
	var progress, dry, keep bool
	var timeout float64
	name := "render"
	if preview {
		name = "preview"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&outFlag, "out", "", "output file (render) or folder (preview)")
	fs.Float64Var(&timeout, "timeout", 0, "seconds")
	fs.BoolVar(&keep, "keep-temp", false, "keep the job folder")
	fs.BoolVar(&dry, "dry-run", false, "print the plan only")
	if preview {
		fs.StringVar(&at, "at", "", "comma-separated times in seconds")
	} else {
		fs.StringVar(&preset, "preset", "", "draft, final or high")
		fs.StringVar(&encoder, "encoder", "", "video encoder")
		fs.BoolVar(&progress, "progress", false, "progress on stderr")
	}
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one project file", Hint: "wecut " + name + " project.json"})
	}
	if preset != "" && preset != "draft" && preset != "final" && preset != "high" {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "unknown preset " + strconv.Quote(preset), Hint: "Use draft, final or high."})
	}
	l, code := a.load(c, pos[0])
	if l == nil {
		return code
	}
	e := l.edit

	caps, err := l.tools.Capabilities(a.ctx)
	if err != nil {
		return a.fail(exitDep, nil, spec.Issue{Code: "E_DEPENDENCY", Message: err.Error()})
	}
	if encoder == "" {
		encoder = caps.DefaultEncoder()
	}
	if !preview && e.Format == "mp4" && !caps.Encoders[encoder] {
		return a.fail(exitDep, nil, spec.Issue{Code: "E_DEPENDENCY", Message: fmt.Sprintf("encoder %q is not available", encoder), Hint: "Run `wecut doctor` to see the encoders this FFmpeg has."})
	}

	opt := engine.Options{Preset: preset, Encoder: encoder, FFmpegMajor: l.tools.Major}
	var finalPath, framesDir, stem string
	var times []float64
	if preview {
		times, err = parseTimes(at, e.Total)
		if err != nil {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: err.Error(), Hint: "--at 0.5,3,10 (seconds)"})
		}
		dir := outFlag
		if dir == "" {
			dir = "out/preview"
		}
		probe, err := l.root.Output(filepath.Join(dir, "x.jpg"))
		if err != nil {
			return a.fail(exitInput, nil, pathIssue(dir, "", err))
		}
		framesDir, stem = filepath.Dir(probe), l.name
		opt.Frames, opt.ResOverride = times, 540
		opt.FramePath = filepath.Join(framesDir, stem+"-%02d.jpg")
	} else {
		target := e.OutFile
		if outFlag != "" {
			target = outFlag
			if ext := strings.ToLower(filepath.Ext(outFlag)); ext != ".mp4" && ext != ".gif" {
				return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "--out must end in .mp4 or .gif"})
			}
			e.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(outFlag)), ".")
		}
		finalPath, err = l.root.Output(target)
		if err != nil {
			return a.fail(exitInput, nil, pathIssue(target, "/output/file", err))
		}
		if l.used[finalPath] {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Path: "/output/file", Message: "output would overwrite an input file", Hint: "Write to out/ or another new name."})
		}
		opt.OutPath = filepath.Join(filepath.Dir(finalPath), fmt.Sprintf(".%s.%d.wecut-tmp", filepath.Base(finalPath), os.Getpid()))
	}

	plan, err := engine.Compile(e, opt)
	if err != nil {
		return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	if dry {
		res := map[string]any{"dry_run": true, "ffmpeg": append([]string{l.tools.FFmpeg}, plan.Args...), "graph": plan.Graph, "width": plan.W, "height": plan.H, "duration": plan.Duration}
		if plan.ASS != "" {
			res["ass"] = plan.ASS
		}
		if len(e.Warnings) > 0 {
			res["warnings"] = e.Warnings
		}
		if m := caps.MissingFor(plan.Graph); len(m) > 0 {
			res["missing_filters"] = m
		}
		return a.ok(res)
	}
	// Only what this project uses has to exist: a build without libass can
	// still render a project that has no text.
	if m := caps.MissingFor(plan.Graph); len(m) > 0 {
		return a.fail(exitDep, e.Warnings, spec.Issue{Code: "E_DEPENDENCY", Message: "this FFmpeg build lacks what the project needs: " + ff.Explain(m), Hint: ff.InstallHint()})
	}

	job, err := os.MkdirTemp("", "wecut-job-")
	if err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	if !keep {
		defer os.RemoveAll(job)
	}
	if err := os.WriteFile(filepath.Join(job, "graph.txt"), []byte(plan.Graph), 0o644); err != nil {
		return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
	}
	if plan.ASS != "" {
		if err := os.WriteFile(filepath.Join(job, "wecut.ass"), []byte(plan.ASS), 0o644); err != nil {
			return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
		}
		if err := assets.WriteFonts(filepath.Join(job, "fonts"), plan.Fonts); err != nil {
			return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
		}
	}
	if preview {
		for i := 1; i <= 99; i++ {
			os.Remove(filepath.Join(framesDir, fmt.Sprintf("%s-%02d.jpg", stem, i)))
		}
	}

	limit := time.Duration(timeout * float64(time.Second))
	if limit <= 0 {
		k := 30.0
		if preview {
			k = 10
		}
		limit = time.Duration(max(180, k*e.Total) * float64(time.Second))
	}
	ctx, cancel := context.WithTimeout(a.ctx, limit)
	defer cancel()

	var onProgress func(float64)
	if progress {
		last := -1.0
		onProgress = func(f float64) {
			pct := float64(int(f*1000)) / 10
			if pct-last >= 2 || pct >= 100 {
				last = pct
				b, _ := json.Marshal(map[string]any{"event": "progress", "percent": pct})
				a.stderr.Write(append(b, '\n'))
			}
		}
	}
	started := time.Now()
	runErr := l.tools.Run(ctx, ff.Job{Dir: job, Args: plan.Args, Duration: plan.Duration, Progress: onProgress})
	elapsed := time.Since(started).Seconds()
	if runErr != nil {
		if opt.OutPath != "" {
			os.Remove(opt.OutPath)
		}
		if errors.Is(runErr, context.DeadlineExceeded) {
			return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_TIMEOUT", Message: fmt.Sprintf("stopped after %.0f s", limit.Seconds()), Hint: "Try --preset draft first, or raise --timeout."})
		}
		if errors.Is(runErr, context.Canceled) {
			return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_RENDER", Message: "cancelled"})
		}
		return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_RENDER", Message: "ffmpeg failed: " + runErr.Error(), Hint: "Run the same command with --dry-run to see the filter graph."})
	}

	if preview {
		got := engine.FrameTimes(times, e.Total, e.FPS)
		frames := make([]map[string]any, 0, len(got))
		for i, t := range got {
			f := filepath.Join(framesDir, fmt.Sprintf("%s-%02d.jpg", stem, i+1))
			if _, err := os.Stat(f); err != nil {
				return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_VERIFY", Message: fmt.Sprintf("frame at %.2f s was not written", t)})
			}
			frames = append(frames, map[string]any{"t": t, "file": l.root.Rel(f)})
		}
		res := map[string]any{"frames": frames, "width": plan.W, "height": plan.H, "duration": e.Total, "render_seconds": round2(elapsed)}
		if len(e.Warnings) > 0 {
			res["warnings"] = e.Warnings
		}
		return a.ok(res)
	}

	info, bad := l.tools.Verify(a.ctx, opt.OutPath, plan)
	if len(bad) > 0 {
		os.Remove(opt.OutPath)
		return a.fail(exitRender, e.Warnings, bad...)
	}
	if err := os.Rename(opt.OutPath, finalPath); err != nil {
		os.Remove(opt.OutPath)
		return a.fail(exitRender, e.Warnings, spec.Issue{Code: "E_RENDER", Message: "cannot move the finished file into place: " + err.Error()})
	}
	res := map[string]any{
		"file": l.root.Rel(finalPath), "duration": info.Duration, "width": info.Width, "height": info.Height,
		"fps": plan.FPS, "size_bytes": info.SizeBytes, "has_audio": info.HasAudio, "preset": plan.Preset,
		"render_seconds": round2(elapsed), "realtime_factor": round2(e.Total / max(elapsed, 0.001)),
	}
	if plan.Encoder != "" {
		res["encoder"] = plan.Encoder
	}
	if len(e.Warnings) > 0 {
		res["warnings"] = e.Warnings
	}
	return a.ok(res)
}

// parseTimes reads --at, defaulting to three frames across the video.
func parseTimes(s string, total float64) ([]float64, error) {
	if strings.TrimSpace(s) == "" {
		return []float64{round2(total * 0.1), round2(total * 0.5), round2(total * 0.9)}, nil
	}
	var out []float64
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("invalid time %q", part)
		}
		out = append(out, v)
	}
	if len(out) > 12 {
		return nil, fmt.Errorf("at most 12 preview frames at once")
	}
	return out, nil
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

// ── list / example ───────────────────────────────────────────────────────

// plainArgs drops the common flags from commands that do not need them, so
// an agent that adds --root to everything still gets an answer.
func plainArgs(name string, args []string) ([]string, error) {
	var c common
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	c.bind(fs)
	return parse(fs, args)
}

func (a *app) list(args []string) int {
	args, err := plainArgs("list", args)
	if err != nil {
		return a.fail(exitInput, nil, usageErr(err))
	}
	type item struct {
		Key   string `json:"key"`
		About string `json:"about,omitempty"`
	}
	all := map[string]any{}
	var styles, caps, fonts, effects, tints []item
	for _, s := range spec.TextStyles {
		styles = append(styles, item{s.Key, s.About})
	}
	for _, s := range spec.CaptionStyles {
		caps = append(caps, item{s.Key, s.About + " (mode " + s.Mode + ")"})
	}
	for _, f := range spec.Fonts {
		fonts = append(fonts, item{f.Key, f.About})
	}
	for _, e := range spec.Effects {
		effects = append(effects, item{e.Key, e.About})
	}
	for _, t := range spec.Tints {
		tints = append(tints, item{t.Key, t.Color + " tint"})
	}
	all["styles"] = styles
	all["caption-styles"] = caps
	all["fonts"] = fonts
	all["transitions"] = spec.TransitionNames()
	all["filters"] = tints
	all["effects"] = effects
	all["motions"] = spec.Motions
	all["ratios"] = spec.Ratios
	all["text-animations"] = map[string]any{"in": spec.AnimIn, "out": spec.AnimOut}
	if len(args) == 0 {
		return a.ok(all)
	}
	v, ok := all[args[0]]
	if !ok {
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "unknown list " + strconv.Quote(args[0]), Hint: "One of: " + strings.Join(keys, ", ")})
	}
	return a.ok(map[string]any{args[0]: v})
}

func (a *app) example(args []string) int {
	args, err := plainArgs("example", args)
	if err != nil {
		return a.fail(exitInput, nil, usageErr(err))
	}
	names := wecut.ExampleNames()
	if len(args) == 0 {
		return a.ok(map[string]any{"examples": names, "hint": "wecut example <name> prints one project"})
	}
	b, ok := wecut.Example(args[0])
	if !ok {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "unknown example " + strconv.Quote(args[0]), Hint: "One of: " + strings.Join(names, ", ")})
	}
	a.stdout.Write(b)
	return exitOK
}
