// Package analyze measures a finished video so an agent can understand why
// it works: how it is cut, how fast it talks, where the beat is. It is pure:
// FFmpeg supplies the scene cuts and the audio samples (see internal/ff),
// this package turns them into numbers. Judging content is left to the
// agent's own model, which looks at the frame sheet.
package analyze

import (
	"math"
	"sort"
)

// Bursts folds cuts that come less than gap seconds apart into one event:
// a white flash, a whip or a strobe trips the cut detector several frames
// in a row, but the viewer sees one transition. Each burst keeps its last
// cut (where the new picture starts); bursts of two or more are returned
// as spans so the agent can see where the effects are.
func Bursts(cuts []float64, gap float64) (clean []float64, bursts []Span) {
	sort.Float64s(cuts)
	for i := 0; i < len(cuts); {
		j := i
		for j+1 < len(cuts) && cuts[j+1]-cuts[j] < gap {
			j++
		}
		clean = append(clean, cuts[j])
		if j > i {
			bursts = append(bursts, Span{r2(cuts[i]), r2(cuts[j])})
		}
		i = j + 1
	}
	return clean, bursts
}

type Shot struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Dur   float64 `json:"dur"`
}

// Shots splits [0, duration] at the cut times.
func Shots(cuts []float64, duration float64) []Shot {
	sort.Float64s(cuts)
	var out []Shot
	prev := 0.0
	for _, c := range append(cuts, duration) {
		if c-prev < 0.04 || c > duration+1e-6 {
			continue // duplicate or out of range
		}
		out = append(out, Shot{r3(prev), r3(c), r3(c - prev)})
		prev = c
	}
	if len(out) == 0 && duration > 0 {
		out = []Shot{{0, r3(duration), r3(duration)}}
	}
	return out
}

type Pace struct {
	Shots      int     `json:"shots"`
	AvgShot    float64 `json:"avg_shot"`
	MedianShot float64 `json:"median_shot"`
	Shortest   float64 `json:"shortest"`
	Longest    float64 `json:"longest"`
	CutsPer10s float64 `json:"cuts_per_10s"`
	// HookCuts is how many cuts fall in the first 3 seconds, the window
	// that decides whether people keep watching.
	HookCuts int    `json:"hook_cuts"`
	Feel     string `json:"feel"`
}

func Measure(shots []Shot, duration float64) Pace {
	p := Pace{Shots: len(shots)}
	if len(shots) == 0 || duration <= 0 {
		return p
	}
	durs := make([]float64, len(shots))
	for i, s := range shots {
		durs[i] = s.Dur
		if i > 0 && s.Start < 3 {
			p.HookCuts++
		}
	}
	sort.Float64s(durs)
	p.AvgShot = r2(duration / float64(len(shots)))
	p.MedianShot = r2(durs[len(durs)/2])
	if len(durs)%2 == 0 {
		p.MedianShot = r2((durs[len(durs)/2-1] + durs[len(durs)/2]) / 2)
	}
	p.Shortest, p.Longest = r2(durs[0]), r2(durs[len(durs)-1])
	p.CutsPer10s = r2(float64(len(shots)-1) / duration * 10)
	switch {
	case len(shots) == 1:
		p.Feel = "one continuous shot"
	case p.MedianShot < 1:
		p.Feel = "very fast cutting"
	case p.MedianShot < 2:
		p.Feel = "fast cutting"
	case p.MedianShot < 4:
		p.Feel = "medium pace"
	default:
		p.Feel = "slow, long takes"
	}
	return p
}

type Span struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type Audio struct {
	RMSdB    float64   `json:"rms_db"`
	PeakdB   float64   `json:"peak_db"`
	Silences []Span    `json:"silences"`
	BPM      float64   `json:"tempo_bpm,omitempty"`
	BPMConf  float64   `json:"tempo_confidence,omitempty"`
	Beats    []float64 `json:"beats,omitempty"`
}

const hop = 256 // samples between onset frames

// Sound measures loudness, silences and tempo from mono 16-bit samples.
func Sound(pcm []int16, rate int) Audio {
	var a Audio
	if len(pcm) == 0 || rate <= 0 {
		return a
	}
	var sum, peak float64
	for _, s := range pcm {
		v := float64(s) / 32768
		sum += v * v
		peak = math.Max(peak, math.Abs(v))
	}
	a.RMSdB = r1(db(math.Sqrt(sum / float64(len(pcm)))))
	a.PeakdB = r1(db(peak))
	a.Silences = silences(pcm, rate)
	a.BPM, a.BPMConf, a.Beats = tempo(pcm, rate)
	return a
}

func db(v float64) float64 {
	if v < 1e-6 {
		return -120
	}
	return 20 * math.Log10(v)
}

// silences returns runs quieter than -45 dBFS that last at least 0.35 s,
// measured in 50 ms windows.
func silences(pcm []int16, rate int) []Span {
	win := rate / 20
	var out []Span
	start := -1.0
	n := len(pcm) / win
	for i := 0; i <= n; i++ {
		quiet := false
		if i < n {
			var sum float64
			for _, s := range pcm[i*win : (i+1)*win] {
				v := float64(s) / 32768
				sum += v * v
			}
			quiet = db(math.Sqrt(sum/float64(win))) < -45
		}
		t := float64(i*win) / float64(rate)
		switch {
		case quiet && start < 0:
			start = t
		case !quiet && start >= 0:
			if t-start >= 0.35 {
				out = append(out, Span{r2(start), r2(t)})
			}
			start = -1
		}
	}
	return out
}

// onsets is the positive change in compressed energy per hop: it jumps where
// a drum hits or a note starts. log1p keeps accents: a plain log would make
// a soft tick out of near-silence jump as high as a kick, and the tempo
// would lock onto the wrong pulse.
func onsets(pcm []int16) []float64 {
	frames := len(pcm)/hop - 1
	if frames < 4 {
		return nil
	}
	raw := make([]float64, frames)
	prev := 0.0
	for i := 0; i < frames; i++ {
		var e float64
		for _, s := range pcm[i*hop : i*hop+2*hop] {
			v := float64(s) / 32768
			e += v * v
		}
		le := math.Log1p(10 * e)
		if i > 0 {
			raw[i] = math.Max(0, le-prev)
		}
		prev = le
	}
	// A light blur, so a beat period that is not a whole number of hops
	// still lines up with itself.
	env := make([]float64, frames)
	for i := range raw {
		env[i] = 0.5 * raw[i]
		if i > 0 {
			env[i] += 0.25 * raw[i-1]
		}
		if i+1 < frames {
			env[i] += 0.25 * raw[i+1]
		}
	}
	return env
}

// tempo estimates beats per minute by autocorrelating the onset curve over
// 60 to 200 BPM, gently preferring tempos near 120 as most music sits there.
// Confidence is the normalised autocorrelation at the chosen lag (0..1).
func tempo(pcm []int16, rate int) (bpm, conf float64, beats []float64) {
	env := onsets(pcm)
	if len(env) < 64 {
		return 0, 0, nil
	}
	mean := 0.0
	for _, v := range env {
		mean += v
	}
	mean /= float64(len(env))
	x := make([]float64, len(env))
	var energy float64
	for i, v := range env {
		x[i] = v - mean
		energy += x[i] * x[i]
	}
	if energy == 0 {
		return 0, 0, nil
	}
	frameSec := float64(hop) / float64(rate)
	minLag := int(math.Floor(60 / 200 / frameSec))
	maxLag := int(math.Ceil(60 / 60 / frameSec))
	if maxLag >= len(x)/2 {
		maxLag = len(x)/2 - 1
	}
	ac := func(lag int) float64 {
		var s float64
		for i := lag; i < len(x); i++ {
			s += x[i] * x[i-lag]
		}
		return s / energy
	}
	best, bestScore, bestAC := 0, math.Inf(-1), 0.0
	for lag := max(minLag, 1); lag <= maxLag; lag++ {
		r := ac(lag)
		b := 60 / (float64(lag) * frameSec)
		w := math.Exp(-0.5 * math.Pow(math.Log2(b/120)/1.0, 2))
		if score := r * w; score > bestScore {
			best, bestScore, bestAC = lag, score, r
		}
	}
	if best == 0 || bestAC <= 0.05 {
		return 0, 0, nil
	}
	// Parabolic refinement around the best lag.
	lag := float64(best)
	if best > minLag && best < maxLag {
		a, b, c := ac(best-1), bestAC, ac(best+1)
		if d := a - 2*b + c; d != 0 {
			lag += 0.5 * (a - c) / d
		}
	}
	period := lag * frameSec
	bpm = 60 / period

	// Phase: the offset whose beat grid collects the most onset energy.
	bestPhase, bestSum := 0.0, -1.0
	for p := 0.0; p < period; p += frameSec {
		var s float64
		for t := p; t < float64(len(env))*frameSec; t += period {
			i := int(math.Round(t / frameSec))
			if i < len(env) {
				s += env[i]
			}
		}
		if s > bestSum {
			bestPhase, bestSum = p, s
		}
	}
	for t := bestPhase; t < float64(len(pcm))/float64(rate); t += period {
		beats = append(beats, r3(t))
	}
	return r1(bpm), r2(bestAC), beats
}

// BeatSync is the share of cuts that land within tol seconds of a beat.
func BeatSync(cuts, beats []float64, tol float64) float64 {
	if len(cuts) == 0 || len(beats) == 0 {
		return 0
	}
	hit := 0
	for _, c := range cuts {
		i := sort.SearchFloat64s(beats, c)
		for _, j := range []int{i - 1, i} {
			if j >= 0 && j < len(beats) && math.Abs(beats[j]-c) <= tol {
				hit++
				break
			}
		}
	}
	return r2(float64(hit) / float64(len(cuts)))
}

// SheetTimes picks up to n frame times: the first frame of the video, then
// the middle of shots, spread evenly when there are more shots than slots.
func SheetTimes(shots []Shot, n int) []float64 {
	if n <= 0 || len(shots) == 0 {
		return nil
	}
	out := []float64{r3(math.Min(0.1, shots[0].Dur/2))}
	rest := n - 1
	if rest <= 0 {
		return out
	}
	pick := make([]int, 0, rest)
	if len(shots) <= rest {
		for i := range shots {
			pick = append(pick, i)
		}
	} else {
		for k := 0; k < rest; k++ {
			pick = append(pick, int(math.Round(float64(k)*float64(len(shots)-1)/float64(rest-1))))
		}
	}
	seen := map[int]bool{}
	for _, i := range pick {
		if seen[i] {
			continue
		}
		seen[i] = true
		mid := r3((shots[i].Start + shots[i].End) / 2)
		if mid-out[len(out)-1] > 0.05 {
			out = append(out, mid)
		}
	}
	return out
}

// Ratio names the closest common aspect ratio.
func Ratio(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	r := float64(w) / float64(h)
	best, bestD := "", math.Inf(1)
	for _, c := range []struct {
		name string
		v    float64
	}{{"9:16", 9.0 / 16}, {"16:9", 16.0 / 9}, {"1:1", 1}, {"4:5", 0.8}, {"4:3", 4.0 / 3}, {"3:4", 0.75}, {"2:1", 2}} {
		if d := math.Abs(math.Log(r / c.v)); d < bestD {
			best, bestD = c.name, d
		}
	}
	if bestD > 0.05 {
		return "other"
	}
	return best
}

func r1(v float64) float64 { return math.Round(v*10) / 10 }
func r2(v float64) float64 { return math.Round(v*100) / 100 }
func r3(v float64) float64 { return math.Round(v*1000) / 1000 }
