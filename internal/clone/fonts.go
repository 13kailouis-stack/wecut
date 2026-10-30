package clone

import (
	"math"
	"strings"
	"unicode"

	"github.com/13kailouis-stack/wecut/internal/engine"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// fontWeight is the stroke weight (Look.Weight) the clone measures for each
// font it may pick, in mixed case and in capitals: dark letters on white at
// 720x1280 with capitals about 33 px high, roman and italic alike. A font not
// listed here is never picked: script, horror and novelty faces are too
// unlike in shape for weight and width to tell them apart, so they are left
// to a person or an agent.
var fontWeight = map[string][2]float64{
	"poppins-medium":             {0.169, 0.240},
	"poppins-semibold":           {0.199, 0.291},
	"poppins":                    {0.239, 0.341},
	"poppins-extrabold":          {0.264, 0.392},
	"poppins-black":              {0.301, 0.441},
	"roboto":                     {0.208, 0.279},
	"montserrat":                 {0.244, 0.328},
	"kanit":                      {0.301, 0.394},
	"kanit-black":                {0.392, 0.501},
	"oswald":                     {0.202, 0.263},
	"barlow-condensed":           {0.214, 0.263},
	"barlow-condensed-extrabold": {0.250, 0.303},
	"bebas":                      {0.200, 0.204},
	"anton":                      {0.208, 0.247},
	"archivo-black":              {0.300, 0.430},
	"lilita":                     {0.272, 0.446},
}

// capsOnly fonts have no real lowercase.
var capsOnly = map[string]bool{"bebas": true, "luckiest": true, "bangers": true, "black-ops": true, "rubik-mono": true, "bungee": true}

// chooseFont picks the bundled font closest to the measured letters: the
// stroke weight, the width of each line when the words are known (the
// font's own advance widths at the measured capital height), and a real
// italic when the text slants.
func chooseFont(l *Look, lines []string, capPx float64, W int, upper bool) string {
	if l == nil || l.Weight == 0 || len(fontWeight) == 0 {
		return ""
	}
	lower := false
	for _, ln := range lines {
		for _, r := range ln {
			lower = lower || (!upper && unicode.IsLower(r))
		}
	}
	// Without the words, a caption set in capitals is measured as capitals.
	caps := 0
	if !lower && (upper || len(lines) > 0) {
		caps = 1
	}
	// A weight far from every font means a glow, an outline or footage got
	// into the letters: the measurement says nothing, keep the default.
	near := math.Inf(1)
	for _, w := range fontWeight {
		near = math.Min(near, math.Abs(l.Weight-w[caps]))
	}
	if near > 0.06 {
		return ""
	}
	best, bestScore := "", math.Inf(1)
	for _, f := range spec.Fonts {
		w, ok := fontWeight[f.Key]
		if !ok {
			continue
		}
		// Blur and compression move the weight more than the line widths,
		// so a width error counts for a little more.
		score := math.Abs(l.Weight-w[caps]) / 0.025
		if l.Italic && f.Italic == "" {
			score += 0.3
		}
		// A font without lowercase only for text known to be in capitals.
		if capsOnly[f.Key] && (lower || (len(lines) == 0 && !upper)) {
			score += 4
		}
		// Line widths, when the words and their measured spans are known.
		if capPx > 0 && len(lines) == len(l.Left) && len(lines) > 0 {
			var errSum float64
			n := 0
			for i, ln := range lines {
				text := ln
				if upper {
					text = strings.ToUpper(ln)
				}
				pred := engine.TextWidth(f.Key, text, capPx/f.Cap)
				meas := (l.Right[i] - l.Left[i]) * float64(W)
				if pred > 0 && meas > 0 && len([]rune(ln)) >= 4 {
					errSum += math.Abs(pred/meas - 1)
					n++
				}
			}
			if n > 0 {
				score += errSum / float64(n) / 0.03
			}
		}
		if score < bestScore {
			best, bestScore = f.Key, score
		}
	}
	return best
}
