package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// Demuxers FFmpeg may use on inputs. Anything else (HLS playlists, concat
// lists, network formats) is refused before it is parsed.
const FormatWhitelist = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,mp3,wav,aac,ogg,flac,image2,png_pipe,jpeg_pipe,webp_pipe"

// Options control how a plan is rendered.
type Options struct {
	Preset      string    // draft, final, high ("" = project quality)
	Encoder     string    // video encoder name
	ResOverride int       // short side, used by previews
	Frames      []float64 // preview: grab these output times instead of encoding a video
	FramePath   string    // preview: output pattern, e.g. /abs/out/preview/clip-%02d.jpg
	OutPath     string    // absolute output path (the runner picks a temp name)
	FFmpegMajor int       // 7+ reads the graph with -/filter_complex
}

// Plan is everything the runner needs. Args assume FFmpeg runs inside a job
// directory that holds graph.txt, wecut.ass and fonts/.
type Plan struct {
	Args     []string
	Graph    string
	ASS      string
	Fonts    []string
	W, H     int
	FPS      int
	Duration float64
	Format   string
	Encoder  string
	Preset   string
	Audio    bool
}

type graph struct {
	lines []string
	n     int
}

func (g *graph) add(format string, a ...any) { g.lines = append(g.lines, fmt.Sprintf(format, a...)) }

func (g *graph) label(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// Compile builds the FFmpeg plan for a resolved edit.
func Compile(e *Edit, opt Options) (*Plan, error) {
	W, H := e.W, e.H
	if opt.ResOverride > 0 {
		short := float64(min(W, H))
		k := float64(opt.ResOverride) / short
		if k < 1 {
			W, H = even(float64(W)*k), even(float64(H)*k)
		}
	}
	preset := opt.Preset
	if preset == "" {
		preset = e.Quality
	}
	if preset == "draft" && opt.Frames == nil {
		if short := float64(min(W, H)); short > 540 {
			k := 540 / short
			W, H = even(float64(W)*k), even(float64(H)*k)
		}
	}
	scaled := *e
	scaled.W, scaled.H = W, H

	preview := len(opt.Frames) > 0
	withAudio := !preview && e.Format == "mp4"
	p := &Plan{W: W, H: H, FPS: e.FPS, Duration: e.Total, Format: e.Format, Preset: preset, Audio: withAudio}
	if preview {
		p.Format = "jpg"
	}

	var args []string
	g := &graph{}
	input := 0
	addInput := func(pre []string, path string) int {
		args = append(args, pre...)
		args = append(args, "-protocol_whitelist", "file", "-format_whitelist", FormatWhitelist, "-i", "file:"+path)
		input++
		return input - 1
	}

	// Clips.
	vlabels := make([]string, len(e.Clips))
	alabels := make([]string, len(e.Clips))
	for i, c := range e.Clips {
		var idx int
		motion := (c.Kind == "image" && c.Clip.Motion != "" || c.Clip.ZoomTo > 0) && clipFit(c.Clip, e.Fit) == "cover"
		switch {
		case c.Kind == "video":
			idx = addInput([]string{"-ss", f3(c.From), "-t", f3(c.To - c.From)}, c.Info.Path)
		case motion:
			idx = addInput(nil, c.Info.Path)
		default:
			idx = addInput([]string{"-loop", "1", "-framerate", fmt.Sprint(e.FPS), "-t", f3(c.Dur)}, c.Info.Path)
		}
		vlabels[i] = clipVideo(g, &scaled, c, idx, motion)
		if withAudio {
			alabels[i] = clipAudio(g, c, idx)
		}
	}

	// Join clips: one concat when there are no transitions, otherwise pairwise.
	var vcur, acur string
	hasTrans := false
	for _, c := range e.Clips[:len(e.Clips)-1] {
		hasTrans = hasTrans || c.Trans != ""
	}
	switch {
	case len(e.Clips) == 1:
		vcur, acur = vlabels[0], alabels[0]
	case !hasTrans:
		var ins strings.Builder
		for i := range e.Clips {
			ins.WriteString("[" + vlabels[i] + "]")
			if withAudio {
				ins.WriteString("[" + alabels[i] + "]")
			}
		}
		vcur = g.label("vj")
		if withAudio {
			acur = g.label("aj")
			g.add("%sconcat=n=%d:v=1:a=1[%s][%s]", ins.String(), len(e.Clips), vcur, acur)
		} else {
			g.add("%sconcat=n=%d:v=1:a=0[%s]", ins.String(), len(e.Clips), vcur)
		}
	default:
		vcur, acur = vlabels[0], alabels[0]
		length := e.Clips[0].Dur
		for i := 1; i < len(e.Clips); i++ {
			prev := e.Clips[i-1]
			nv := g.label("vj")
			if prev.Trans != "" {
				g.add("[%s][%s]xfade=transition=%s:duration=%s:offset=%s[%s]", vcur, vlabels[i], prev.Trans, f3(prev.TransDur), f3(length-prev.TransDur), nv)
				length += e.Clips[i].Dur - prev.TransDur
			} else {
				g.add("[%s][%s]concat=n=2:v=1:a=0[%s]", vcur, vlabels[i], nv)
				length += e.Clips[i].Dur
			}
			vcur = nv
			if withAudio {
				na := g.label("aj")
				if prev.Trans != "" {
					g.add("[%s][%s]acrossfade=d=%s:c1=tri:c2=tri[%s]", acur, alabels[i], f3(prev.TransDur), na)
				} else {
					g.add("[%s][%s]concat=n=2:v=0:a=1[%s]", acur, alabels[i], na)
				}
				acur = na
			}
		}
	}

	// Filters and effects are translucent colour washes switched on by time.
	// A full-frame drawbox costs ~10 ms per 1080p frame; a lookup table on
	// each YUV plane gives the same blend for almost nothing.
	var post []string
	for _, f := range e.P.Filters {
		t, _ := spec.TintByKey(f.Name)
		amount := 100.0
		if f.Amount != nil {
			amount = *f.Amount
		}
		post = append(post, tint(t.Color, t.A*amount/100, between(f.Start, endOr(f.End, e.Total))))
	}
	for _, ef := range e.P.Effects {
		def, _ := spec.EffectByKey(ef.Name)
		end := ef.Start + def.Dur
		if ef.End != nil {
			end = *ef.End
		}
		en := between(ef.Start, math.Min(end, e.Total))
		switch ef.Name {
		case "flash":
			post = append(post, tint("#FFFFFF", 0.78, en))
		case "dim":
			post = append(post, tint("#000000", 0.45, en))
		case "night":
			post = append(post, tint("#0B1E4A", 0.4, en))
		case "sepia":
			post = append(post, tint("#704214", 0.28, en))
		case "cinema":
			bar := even(float64(H) * 0.12)
			post = append(post,
				fmt.Sprintf("drawbox=x=0:y=0:w=iw:h=%d:color=0x000000:t=fill:enable='%s'", bar, en),
				fmt.Sprintf("drawbox=x=0:y=ih-%d:w=iw:h=%d:color=0x000000:t=fill:enable='%s'", bar, bar, en))
		case "frame":
			post = append(post, fmt.Sprintf("drawbox=x=0:y=0:w=iw:h=ih:color=0xFFFFFF:t=%d:enable='%s'", max(2, int(math.Round(float64(W)*0.035))), en))
		}
	}
	if len(post) > 0 {
		nv := g.label("vf")
		g.add("[%s]%s[%s]", vcur, strings.Join(post, ","), nv)
		vcur = nv
	}

	// Image overlays.
	for i, ov := range e.P.Overlays {
		if ov.Start >= e.Total {
			continue
		}
		info := e.Media[ov.Src]
		end := endOr(ov.End, e.Total)
		idx := addInput([]string{"-loop", "1", "-framerate", fmt.Sprint(e.FPS), "-t", f3(e.Total)}, info.Path)
		width := ov.Width
		if width == 0 {
			width = 0.3
		}
		chain := []string{"format=rgba", fmt.Sprintf("scale=%d:-2", even(width*float64(W)))}
		if ov.Opacity != nil && *ov.Opacity < 100 {
			chain = append(chain, "colorchannelmixer=aa="+f3(*ov.Opacity/100))
		}
		if ov.Rotate != 0 {
			rad := f3(ov.Rotate * math.Pi / 180)
			chain = append(chain, fmt.Sprintf("rotate=%s:c=none:ow=rotw(%s):oh=roth(%s)", rad, rad, rad))
		}
		if ov.FadeIn > 0 {
			chain = append(chain, fmt.Sprintf("fade=t=in:st=%s:d=%s:alpha=1", f3(ov.Start), f3(ov.FadeIn)))
		}
		if ov.FadeOut > 0 {
			chain = append(chain, fmt.Sprintf("fade=t=out:st=%s:d=%s:alpha=1", f3(math.Max(ov.Start, end-ov.FadeOut)), f3(ov.FadeOut)))
		}
		ol := fmt.Sprintf("ov%d", i)
		g.add("[%d:v]%s[%s]", idx, strings.Join(chain, ","), ol)
		x, y := 0.5, 0.5
		if ov.X != nil {
			x = *ov.X
		}
		if ov.Y != nil {
			y = *ov.Y
		}
		nv := g.label("vo")
		g.add("[%s][%s]overlay=x=%s-w/2:y=%s-h/2:enable='%s':eof_action=pass[%s]", vcur, ol, f3(x*float64(W)), f3(y*float64(H)), between(ov.Start, end), nv)
		vcur = nv
	}

	// Text and captions.
	ass, fonts := buildASS(&scaled)
	p.ASS, p.Fonts = ass, fonts
	tail := []string{}
	if ass != "" {
		tail = append(tail, "ass=f=wecut.ass:fontsdir=fonts")
	}
	vout := "vout"
	switch {
	case preview:
		sel := make([]string, 0, len(opt.Frames))
		for _, n := range frameNumbers(opt.Frames, e.Total, e.FPS) {
			sel = append(sel, fmt.Sprintf("eq(n,%d)", n))
		}
		// A JPEG is full range: without this, white shows as 235 and the
		// frames a model checks look dull.
		tail = append(tail, "select='"+strings.Join(sel, "+")+"'", "scale=out_range=pc", "format=yuvj420p")
		g.add("[%s]%s[%s]", vcur, strings.Join(tail, ","), vout)
	case e.Format == "gif":
		gw, gh := W, H
		if short := float64(min(W, H)); short > 480 {
			gw, gh = even(float64(W)*480/short), even(float64(H)*480/short)
		}
		tail = append(tail, fmt.Sprintf("fps=%d", min(15, e.FPS)), fmt.Sprintf("scale=%d:%d:flags=lanczos", gw, gh), "split[g1][g2]")
		g.add("[%s]%s", vcur, strings.Join(tail, ","))
		g.add("[g1]palettegen=stats_mode=diff[gp]")
		g.add("[g2][gp]paletteuse=dither=bayer:bayer_scale=5[%s]", vout)
		p.W, p.H, p.FPS = gw, gh, min(15, e.FPS)
	default:
		tail = append(tail, "format=yuv420p")
		g.add("[%s]%s[%s]", vcur, strings.Join(tail, ","), vout)
	}

	// Extra audio tracks, mixed over the clips' own sound.
	aout := ""
	if withAudio {
		aout = mixAudio(g, e, acur, addInput)
	}

	p.Graph = strings.Join(g.lines, ";\n") + "\n"
	if opt.FFmpegMajor >= 7 {
		args = append(args, "-/filter_complex", "graph.txt")
	} else {
		args = append(args, "-filter_complex_script", "graph.txt")
	}
	args = append(args, "-map", "["+vout+"]")

	switch {
	case preview:
		n := len(frameNumbers(opt.Frames, e.Total, e.FPS))
		args = append(args, "-fps_mode", "passthrough", "-frames:v", fmt.Sprint(n), "-q:v", "3", "-f", "image2", opt.FramePath)
	case e.Format == "gif":
		args = append(args, "-loop", "0", "-t", f3(e.Total), "-f", "gif", opt.OutPath)
	default:
		enc, vargs := encoderArgs(opt.Encoder, preset, e.FPS)
		p.Encoder = enc
		args = append(args, vargs...)
		args = append(args, "-r", fmt.Sprint(e.FPS), "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709")
		args = append(args, "-map", "["+aout+"]", "-c:a", "aac", "-b:a", map[string]string{"draft": "96k", "final": "160k", "high": "192k"}[preset], "-ar", "48000", "-ac", "2")
		args = append(args, "-t", f3(e.Total), "-movflags", "+faststart", "-f", "mp4", opt.OutPath)
	}
	p.Args = args
	return p, nil
}

// clipVideo writes the filter chain that turns one clip into canvas-sized
// frames of exactly its timeline duration, and returns the output label.
func clipVideo(g *graph, e *Edit, c ClipT, idx int, motion bool) string {
	W, H := e.W, e.H
	cl := c.Clip
	in := fmt.Sprintf("[%d:v]", idx)
	var pre []string
	if c.Kind == "video" {
		if c.Speed != 1 {
			pre = append(pre, fmt.Sprintf("setpts=(PTS-STARTPTS)/%s", f3(c.Speed)))
		} else {
			pre = append(pre, "setpts=PTS-STARTPTS")
		}
	}
	switch (cl.Rotate + 360) % 360 {
	case 90:
		pre = append(pre, "transpose=1")
	case 180:
		pre = append(pre, "hflip", "vflip")
	case 270:
		pre = append(pre, "transpose=2")
	}
	if cl.Flip {
		pre = append(pre, "hflip")
	}

	fit := clipFit(cl, e.Fit)
	out := fmt.Sprintf("v%d", c.Index)
	tail := []string{fmt.Sprintf("fps=%d", e.FPS), "format=yuv420p", "setsar=1"}
	if !motion || c.Kind == "video" {
		tail = append(tail, "tpad=stop_mode=clone:stop_duration=1", fmt.Sprintf("trim=duration=%s", f3(c.Dur)), "setpts=PTS-STARTPTS")
	}
	// One time base for every clip: concat outputs microseconds, and an
	// xfade after a concat refuses inputs whose time bases differ.
	tail = append(tail, "settb=AVTB")
	if cl.FadeIn > 0 {
		tail = append(tail, fmt.Sprintf("fade=t=in:st=0:d=%s", f3(cl.FadeIn)))
	}
	if cl.FadeOut > 0 {
		tail = append(tail, fmt.Sprintf("fade=t=out:st=%s:d=%s", f3(c.Dur-cl.FadeOut), f3(cl.FadeOut)))
	}

	switch {
	case motion:
		// zoompan on a 2x copy: whole-pixel steps at double size are half a
		// pixel on the canvas, so slow zooms do not wobble. A photo is one
		// frame held for every output frame (d=frames); video is one output
		// frame per input frame (d=1), already at the output rate.
		frames := max(2, int(math.Round(c.Dur*float64(e.FPS))))
		d := frames
		p := fmt.Sprintf("on/%d", frames-1)
		fx, fy := 0.5, 0.5
		if cl.Focus != nil {
			fx, fy = cl.Focus.X, cl.Focus.Y
		}
		z, x, y := "1.3", "(iw-iw/zoom)/2", "(ih-ih/zoom)/2"
		crop := fmt.Sprintf("crop=%d:%d", 2*W, 2*H)
		if fx != 0.5 || fy != 0.5 {
			x, y = "(iw-iw/zoom)*"+f3(fx), "(ih-ih/zoom)*"+f3(fy)
			crop += fmt.Sprintf(":(iw-%d)*%s:(ih-%d)*%s", 2*W, f3(fx), 2*H, f3(fy))
		}
		switch cl.Motion {
		case "in":
			z = "1+0.3*" + p
		case "out":
			z = "1.3-0.3*" + p
		case "left":
			x = "(iw-iw/zoom)*" + p
		case "right":
			x = "(iw-iw/zoom)*(1-" + p + ")"
		case "up":
			y = "(ih-ih/zoom)*" + p
		case "down":
			y = "(ih-ih/zoom)*(1-" + p + ")"
		}
		if cl.ZoomTo > 0 {
			z0 := math.Max(1, cl.Zoom)
			z = f3(z0) + "+" + f3(cl.ZoomTo-z0) + "*" + p
		}
		var head []string
		if c.Kind == "video" {
			d = 1
			head = append(head, fmt.Sprintf("fps=%d", e.FPS))
		}
		chain := append(append(pre, head...),
			fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=bicubic", 2*W, 2*H),
			crop,
			fmt.Sprintf("zoompan=z='%s':x='%s':y='%s':d=%d:s=%dx%d:fps=%d", z, x, y, d, W, H, e.FPS))
		g.add("%s%s[%s]", in, strings.Join(append(chain, tail...), ","), out)
	case fit == "cover":
		zoom := math.Max(1, cl.Zoom)
		fx, fy := 0.5, 0.5
		if cl.Focus != nil {
			fx, fy = cl.Focus.X, cl.Focus.Y
		}
		chain := append(pre,
			fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=bicubic", even(float64(W)*zoom), even(float64(H)*zoom)),
			fmt.Sprintf("crop=%d:%d:(iw-%d)*%s:(ih-%d)*%s", W, H, W, f3(fx), H, f3(fy)))
		g.add("%s%s[%s]", in, strings.Join(append(chain, tail...), ","), out)
	case e.Bg == "blur":
		// Blur at quarter size: same look, a fraction of the work.
		qw, qh := even(float64(W)/4), even(float64(H)/4)
		r := []int{0, 2, 4, 7, 10}[e.Blur]
		a, b, bg, fg := g.label("cb"), g.label("cf"), g.label("cbg"), g.label("cfg")
		head := ""
		if len(pre) > 0 {
			head = strings.Join(pre, ",") + ","
		}
		g.add("%s%ssplit=2[%s][%s]", in, head, a, b)
		g.add("[%s]scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,boxblur=%d:2,scale=%d:%d[%s]", a, qw, qh, qw, qh, r, W, H, bg)
		g.add("[%s]scale=%d:%d:force_original_aspect_ratio=decrease:flags=bicubic[%s]", b, W, H, fg)
		g.add("[%s][%s]overlay=(W-w)/2:(H-h)/2,%s[%s]", bg, fg, strings.Join(tail, ","), out)
	default:
		chain := append(pre,
			fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=bicubic", W, H),
			fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s", W, H, hex0x(e.Bg)))
		g.add("%s%s[%s]", in, strings.Join(append(chain, tail...), ","), out)
	}
	return out
}

// clipAudio returns a stereo 48 kHz stream of exactly the clip's duration:
// its own sound (sped up, faded) or silence.
func clipAudio(g *graph, c ClipT, idx int) string {
	cl := c.Clip
	out := fmt.Sprintf("a%d", c.Index)
	vol := 100.0
	if cl.Volume != nil {
		vol = *cl.Volume
	}
	norm := "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo"
	fit := fmt.Sprintf("apad=whole_dur=%s,atrim=duration=%s", f3(c.Dur), f3(c.Dur))
	if c.Kind != "video" || !c.Info.HasAudio || cl.Mute || vol == 0 {
		g.add("anullsrc=r=48000:cl=stereo,atrim=duration=%s,%s[%s]", f3(c.Dur), norm, out)
		return out
	}
	chain := []string{"asetpts=PTS-STARTPTS"}
	chain = append(chain, atempo(c.Speed)...)
	if vol != 100 {
		chain = append(chain, "volume="+f3(vol/100))
	}
	chain = append(chain, norm, fit)
	if cl.FadeIn > 0 {
		chain = append(chain, fmt.Sprintf("afade=t=in:st=0:d=%s", f3(cl.FadeIn)))
	}
	if cl.FadeOut > 0 {
		chain = append(chain, fmt.Sprintf("afade=t=out:st=%s:d=%s", f3(c.Dur-cl.FadeOut), f3(cl.FadeOut)))
	}
	g.add("[%d:a]%s[%s]", idx, strings.Join(chain, ","), out)
	return out
}

// atempo splits a speed into factors within 0.5..2, which every FFmpeg
// version accepts.
func atempo(speed float64) []string {
	var out []string
	for speed > 2.0001 {
		out = append(out, "atempo=2")
		speed /= 2
	}
	for speed < 0.4999 {
		out = append(out, "atempo=0.5")
		speed /= 0.5
	}
	if math.Abs(speed-1) > 1e-4 {
		out = append(out, "atempo="+f3(speed))
	}
	return out
}

func mixAudio(g *graph, e *Edit, main string, addInput func([]string, string) int) string {
	type track struct {
		label string
		duck  bool
	}
	var tracks []track
	for i, a := range e.P.Audio {
		if a.At >= e.Total {
			continue
		}
		info := e.Media[a.Src]
		room := e.Total - a.At
		length := room
		if !a.Loop {
			srcEnd := info.Duration
			if a.To != nil {
				srcEnd = math.Min(*a.To, info.Duration)
			}
			length = math.Min(length, srcEnd-a.From)
		} else if a.To != nil {
			length = math.Min(length, *a.To-a.From)
		}
		if a.Duration != nil {
			length = math.Min(length, *a.Duration)
		}
		if length < 0.05 {
			continue
		}
		var idx int
		var chain []string
		if a.Loop {
			idx = addInput([]string{"-stream_loop", "-1"}, info.Path)
			if a.From > 0 {
				chain = append(chain, fmt.Sprintf("atrim=start=%s", f3(a.From)))
			}
		} else {
			idx = addInput([]string{"-ss", f3(a.From)}, info.Path)
		}
		chain = append(chain, fmt.Sprintf("atrim=duration=%s", f3(length)), "asetpts=PTS-STARTPTS")
		if a.Volume != nil && *a.Volume != 100 {
			chain = append(chain, "volume="+f3(*a.Volume/100))
		}
		chain = append(chain, "aresample=48000", "aformat=sample_fmts=fltp:channel_layouts=stereo")
		if a.FadeIn > 0 {
			chain = append(chain, fmt.Sprintf("afade=t=in:st=0:d=%s", f3(math.Min(a.FadeIn, length))))
		}
		if a.FadeOut > 0 {
			fo := math.Min(a.FadeOut, length)
			chain = append(chain, fmt.Sprintf("afade=t=out:st=%s:d=%s", f3(length-fo), f3(fo)))
		}
		if a.At > 0 {
			ms := int(math.Round(a.At * 1000))
			chain = append(chain, fmt.Sprintf("adelay=%d|%d", ms, ms))
		}
		l := fmt.Sprintf("t%d", i)
		g.add("[%d:a]%s[%s]", idx, strings.Join(chain, ","), l)
		tracks = append(tracks, track{l, a.Duck})
	}
	if len(tracks) == 0 {
		g.add("[%s]alimiter=limit=0.97:level=0[aout]", main)
		return "aout"
	}
	ducks := 0
	for _, t := range tracks {
		if t.duck {
			ducks++
		}
	}
	mainOut := main
	if ducks > 0 {
		outs := []string{g.label("am")}
		for i := 0; i < ducks; i++ {
			outs = append(outs, g.label("sc"))
		}
		g.add("[%s]asplit=%d[%s]", main, ducks+1, strings.Join(outs, "]["))
		mainOut = outs[0]
		k := 1
		for i, t := range tracks {
			if !t.duck {
				continue
			}
			nl := g.label("td")
			g.add("[%s][%s]sidechaincompress=threshold=0.02:ratio=8:attack=20:release=350:makeup=1[%s]", t.label, outs[k], nl)
			tracks[i].label = nl
			k++
		}
	}
	ins := "[" + mainOut + "]"
	for _, t := range tracks {
		ins += "[" + t.label + "]"
	}
	g.add("%samix=inputs=%d:duration=first:dropout_transition=0:normalize=0,alimiter=limit=0.97:level=0[aout]", ins, len(tracks)+1)
	return "aout"
}

// encoderArgs picks codec settings. libx264 is the reference; the others are
// fallbacks for FFmpeg builds without it.
func encoderArgs(enc, preset string, fps int) (string, []string) {
	if enc == "" {
		enc = "libx264"
	}
	crf := map[string]int{"draft": 28, "final": 21, "high": 18}[preset]
	gop := fmt.Sprint(fps * 2)
	switch enc {
	case "libx264":
		speed := map[string]string{"draft": "ultrafast", "final": "veryfast", "high": "medium"}[preset]
		return enc, []string{"-c:v", "libx264", "-preset", speed, "-crf", fmt.Sprint(crf), "-pix_fmt", "yuv420p", "-profile:v", "high", "-g", gop}
	case "h264_nvenc":
		return enc, []string{"-c:v", enc, "-preset", "p4", "-rc", "vbr", "-cq", fmt.Sprint(crf + 2), "-b:v", "0", "-pix_fmt", "yuv420p", "-g", gop}
	case "h264_qsv":
		return enc, []string{"-c:v", enc, "-global_quality", fmt.Sprint(crf + 2), "-pix_fmt", "nv12", "-g", gop}
	case "h264_amf":
		return enc, []string{"-c:v", enc, "-rc", "cqp", "-qp_i", fmt.Sprint(crf), "-qp_p", fmt.Sprint(crf + 2), "-pix_fmt", "yuv420p", "-g", gop}
	case "h264_videotoolbox":
		return enc, []string{"-c:v", enc, "-q:v", fmt.Sprint(map[string]int{"draft": 45, "final": 60, "high": 70}[preset]), "-pix_fmt", "yuv420p", "-g", gop}
	default: // h264_mf, libopenh264 and anything else: bitrate mode
		return enc, []string{"-c:v", enc, "-b:v", map[string]string{"draft": "2M", "final": "6M", "high": "10M"}[preset], "-pix_fmt", "yuv420p", "-g", gop}
	}
}

// frameNumbers turns preview times into unique, sorted frame indexes. Times
// past the end land two frames before it: rounding in transitions can leave
// the very last frame out of the stream.
func frameNumbers(times []float64, total float64, fps int) []int {
	last := max(0, int(math.Floor(total*float64(fps)))-2)
	seen := map[int]bool{}
	var out []int
	for _, t := range times {
		n := min(last, max(0, int(math.Round(t*float64(fps)))))
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// FrameTimes reports the time of each preview frame, in output order.
func FrameTimes(times []float64, total float64, fps int) []float64 {
	ns := frameNumbers(times, total, fps)
	out := make([]float64, len(ns))
	for i, n := range ns {
		out[i] = round3(float64(n) / float64(fps))
	}
	return out
}

// tint blends a colour over the frame at opacity a, in limited-range BT.709
// YUV (what every clip is converted to before this point).
func tint(hex string, a float64, enable string) string {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	R, G, B := float64(r)/255, float64(g)/255, float64(b)/255
	y := 0.2126*R + 0.7152*G + 0.0722*B
	Y := 16 + 219*y
	U := 128 + 224*(B-y)/1.8556
	V := 128 + 224*(R-y)/1.5748
	k := f3(1 - a)
	return fmt.Sprintf("lutyuv=y='val*%s+%s':u='val*%s+%s':v='val*%s+%s':enable='%s'", k, f3(Y*a), k, f3(U*a), k, f3(V*a), enable)
}

func between(s, e float64) string { return fmt.Sprintf("between(t,%s,%s)", f3(s), f3(e)) }

func endOr(p *float64, d float64) float64 {
	if p == nil {
		return d
	}
	return math.Min(*p, d)
}

func hex0x(c string) string { return "0x" + strings.TrimPrefix(c, "#") }

func f3(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}
