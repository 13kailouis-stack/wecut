// Package clone turns a finished video into a WeCut template: it finds the
// cuts and transitions, how each shot is framed and moved, the text cards,
// the captions and the logo, then writes them as a recipe whose shots have
// the original's exact lengths. Fill the slots with new footage and words
// and the new video is cut the same way.
//
// The package is pure Go and does no I/O, so the same code runs in the
// `wecut clone` command (FFmpeg decodes the frames) and in a browser
// (WebAssembly, the page decodes the frames). It works in four steps:
//
//  1. New, then AddLow / AddHigh for every decoded frame and SetAudio: two
//     small grey streams, one at full frame rate for timing and one sharper
//     and slower for reading text positions.
//  2. Analyze finds everything that can be found from those, and asks for a
//     few full-size frames (Plan.Stills).
//  3. Measure reads colours, boxes, font sizes and styles from those frames
//     and returns the text crops, for OCR or for a model to read.
//  4. Build writes the recipe and a report, using the OCR'd words if any.
package clone

import (
	"math"

	"github.com/13kailouis-stack/wecut/internal/analyze"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// Config describes the source video and the two streams fed to the
// analyzer. LowFPS is the full timing rate (at most 30); HighFPS is the
// rate of the sharper text stream (6 is plenty).
type Config struct {
	W, H     int     // source picture size, after rotation
	Duration float64 // seconds
	LowFPS   float64
	HighFPS  float64
}

// Sizes of the two streams for a source picture: the low stream has a
// short side of 72 pixels, the high one 288.
func LowSize(w, h int) (int, int)  { return fitShort(w, h, 72) }
func HighSize(w, h int) (int, int) { return fitShort(w, h, 288) }

func fitShort(w, h, short int) (int, int) {
	if w <= 0 || h <= 0 {
		return short, short
	}
	if w < h {
		return short, even(float64(h) * float64(short) / float64(w))
	}
	return even(float64(w) * float64(short) / float64(h)), short
}

func even(v float64) int { return max(2, int(math.Round(v/2))*2) }

// Analyzer collects frames and finds the edit in them.
type Analyzer struct {
	cfg     Config
	low     []lowStat
	lowGray []*Gray
	prevLow *Gray

	highN    int
	highW    int
	highH    int
	prevHigh *Gray
	edges    [][]uint64 // edge map of every high frame, as bits
	tracks   []*track
	active   []*track

	pcm  []int16
	rate int

	debug *DebugInfo

	// Results of Analyze, used by Measure and Build.
	plan *Plan
}

func New(cfg Config) *Analyzer {
	if cfg.LowFPS <= 0 {
		cfg.LowFPS = 30
	}
	if cfg.HighFPS <= 0 {
		cfg.HighFPS = 6
	}
	return &Analyzer{cfg: cfg}
}

// AddLow takes the next frame of the low stream (grey, LowSize).
func (a *Analyzer) AddLow(g *Gray) {
	st := lowStat{hist: histogram(g), sharp: sharpness(g)}
	st.mean, st.std = meanStd(g.Pix)
	if a.prevLow != nil && a.prevLow.W == g.W && a.prevLow.H == g.H {
		st.diff = changeScore(a.prevLow, g)
		st.hdist = histDist(a.low[len(a.low)-1].hist, st.hist)
	}
	a.low = append(a.low, st)
	a.lowGray = append(a.lowGray, g)
	a.prevLow = g
}

// AddHigh takes the next frame of the high stream (grey, HighSize). Text is
// found and followed frame by frame, so the frames are not kept.
func (a *Analyzer) AddHigh(g *Gray) {
	a.highW, a.highH = g.W, g.H
	a.trackText(g, a.highN)
	a.prevHigh = g
	a.highN++
}

// SetAudio gives the soundtrack as mono 16-bit samples.
func (a *Analyzer) SetAudio(pcm []int16, rate int) {
	a.pcm, a.rate = pcm, rate
}

func (a *Analyzer) lowTime(i int) float64  { return float64(i) / a.cfg.LowFPS }
func (a *Analyzer) highTime(i int) float64 { return float64(i) / a.cfg.HighFPS }

// lowIndex is the low frame shown at time t.
func (a *Analyzer) lowIndex(t float64) int {
	return clampi(int(math.Floor(t*a.cfg.LowFPS+1e-6)), 0, len(a.lowGray)-1)
}

// Plan is what Analyze found before looking at full-size frames.
type Plan struct {
	Duration   float64         `json:"duration"`    // of the source
	ContentEnd float64         `json:"content_end"` // where the edit ends (a platform outro is cut off)
	Events     []Event         `json:"events"`
	Shots      []ShotInfo      `json:"shots"`
	Blocks     []*Block        `json:"texts"`
	Captions   *CaptionBand    `json:"captions,omitempty"`
	Logos      []*Block        `json:"logos,omitempty"`
	Gradients  []spec.Gradient `json:"gradients,omitempty"`
	Audio      *analyze.Audio  `json:"audio,omitempty"`
	Speech     bool            `json:"speech"`
	Stills     []float64       `json:"stills"` // full-size frames Measure needs, in seconds
	Notes      []string        `json:"notes,omitempty"`
	stillOf    map[string][]int
	lowW, lowH int
}

// Resume picks up a finished analysis, so a model's answers can be built
// into a recipe without going through the frames again.
func Resume(cfg Config, p *Plan) *Analyzer { return &Analyzer{cfg: cfg, plan: p} }

// Analyze runs every detector on the frames given so far.
func (a *Analyzer) Analyze() *Plan {
	p := &Plan{Duration: a.cfg.Duration, stillOf: map[string][]int{}}
	if len(a.lowGray) > 0 {
		p.lowW, p.lowH = a.lowGray[0].W, a.lowGray[0].H
	}
	dur := a.cfg.Duration
	if n := float64(len(a.low)) / a.cfg.LowFPS; dur <= 0 || n < dur-0.5 {
		dur = n
	}
	p.ContentEnd = dur
	if len(a.pcm) > 0 {
		snd := analyze.Sound(a.pcm, a.rate)
		p.Audio = &snd
		// A silent tail of 1.5 s or more is the platform's download outro.
		if s := snd.Silences; len(s) > 0 {
			last := s[len(s)-1]
			if last.End >= dur-0.3 && last.End-last.Start >= 1.5 && last.Start > dur*0.5 {
				p.ContentEnd = last.Start
				p.Notes = append(p.Notes, "the silent ending (platform outro) is left out")
			}
		}
	}
	p.Events = a.detectEvents()
	p.ContentEnd = a.trimOutro(p.Events, p.ContentEnd)
	var evs []Event
	for _, e := range p.Events {
		if e.Start < p.ContentEnd-0.2 && e.At > 0.15 {
			evs = append(evs, e)
		}
	}
	p.Events = evs
	p.Shots = a.shots(p.Events, p.ContentEnd)
	a.textResults(p)
	p.Speech = p.Captions != nil
	p.Gradients = a.gradients(p)
	a.plan = p
	a.planStills(p)
	return p
}

// trimOutro cuts a closing card the platform adds (a dark, still screen
// after the last cut that the audio already marked as silent, or a dark
// still shot of at least 1.5 s at the very end).
func (a *Analyzer) trimOutro(evs []Event, end float64) float64 {
	if len(evs) == 0 {
		return end
	}
	last := evs[len(evs)-1]
	if last.At >= end-0.2 {
		// The cut into the outro is at (or right after) the silence.
		return math.Min(end, last.Start)
	}
	tail := a.cfg.Duration - last.End
	if tail < 1.5 {
		return end
	}
	i0, i1 := a.lowIndex(last.End+0.1), len(a.low)-1
	var m, d []float64
	for i := i0; i <= i1; i++ {
		m = append(m, a.low[i].mean)
		d = append(d, a.low[i].diff)
	}
	if median(m) < 45 && median(d) < 1.5 && end >= a.cfg.Duration-0.3 {
		return last.Start
	}
	return end
}

// DebugInfo is everything the detectors measured, for tuning them: the
// change score, brightness and sharpness of every low frame, the text lines
// found in every high frame, and every followed line.
type DebugInfo struct {
	Low    [][4]float64 `json:"low"` // diff, hdist, mean, sharpness
	High   [][]Box      `json:"high"`
	Tracks []DebugTrack `json:"tracks"`
}

type DebugTrack struct {
	Start, End float64
	Box        Box
	Seen       int
	Moved      int
}

// Debug records the high-stream lines for DebugInfo; set it before adding
// frames.
func (a *Analyzer) Debug() { a.debug = &DebugInfo{} }

func (a *Analyzer) DebugInfo() *DebugInfo {
	if a.debug == nil {
		return nil
	}
	d := *a.debug
	d.Low = nil
	for _, s := range a.low {
		d.Low = append(d.Low, [4]float64{r2(s.diff), r2(s.hdist), r2(s.mean), r2(s.sharp)})
	}
	d.Tracks = nil
	for _, t := range a.tracks {
		if t.seen >= 2 || t.seen < 0 {
			d.Tracks = append(d.Tracks, DebugTrack{r2(a.highTime(t.first)), r2(a.highTime(t.last + 1)), t.mean(), t.seen, t.moved})
		}
	}
	return &d
}
