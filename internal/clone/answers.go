package clone

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

var answerHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// CheckAnswers lists what is wrong with a model's answers: ids this video
// does not have, and values the engine cannot use. A wrong id usually means
// the answer was written for another run, so it is an error, not ignored.
func CheckAnswers(p *Plan, crops []Crop, read map[string]Reading) []string {
	known := map[string]bool{}
	for _, bl := range p.Blocks {
		known[bl.ID] = true
	}
	for _, lg := range p.Logos {
		known[lg.ID] = true
	}
	for _, c := range crops {
		known[c.ID] = true
	}
	// Also when the detector found none: a model may see captions it missed.
	known["captions"] = true
	var out []string
	ids := make([]string, 0, len(read))
	for id := range read {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rd := read[id]
		if n, ok := strings.CutPrefix(id, "gradient"); ok {
			k, err := strconv.Atoi(n)
			if err != nil || k < 1 || k > len(p.Gradients)+4 {
				out = append(out, fmt.Sprintf("%s: gradients are numbered from 1; this video has %d", id, len(p.Gradients)))
				continue
			}
			if k > len(p.Gradients) && !rd.Drop && (rd.From == nil || rd.To == nil) {
				out = append(out, fmt.Sprintf("%s: a new gradient needs from and to", id))
			}
			for _, v := range []*float64{rd.From, rd.To} {
				if v != nil && (*v < 0 || *v > 1) {
					out = append(out, fmt.Sprintf("%s: from and to are fractions of the height, 0 to 1", id))
				}
			}
			if rd.From != nil && rd.To != nil && *rd.From == *rd.To {
				out = append(out, fmt.Sprintf("%s: from and to must differ", id))
			}
			if rd.Opacity != nil && (*rd.Opacity < 0 || *rd.Opacity > 100) {
				out = append(out, fmt.Sprintf("%s: opacity must be 0 to 100", id))
			}
			if rd.Color != "" && !answerHex.MatchString(rd.Color) {
				out = append(out, fmt.Sprintf("%s: colour %q must look like #RRGGBB", id, rd.Color))
			}
			continue
		}
		if n, ok := strings.CutPrefix(id, "slot"); ok {
			k, err := strconv.Atoi(n)
			if err != nil || k < 1 || k > len(p.Shots) {
				out = append(out, fmt.Sprintf("%s: this video has slots 1 to %d", id, len(p.Shots)))
				continue
			}
			if rd.Join != "" {
				if _, ok := spec.Transitions[rd.Join]; !ok {
					out = append(out, fmt.Sprintf("%s: unknown transition %q (wecut list transitions)", id, rd.Join))
				} else if p.Shots[k-1].Join == "" {
					out = append(out, fmt.Sprintf("%s: this shot ends on a hard cut, so its timing has no transition to rename", id))
				}
			}
			continue
		}
		if !known[id] {
			out = append(out, fmt.Sprintf("%s: not in this video's review", id))
			continue
		}
		if rd.Font != "" {
			if _, ok := spec.FontByKey(rd.Font); !ok {
				out = append(out, fmt.Sprintf("%s: unknown font %q (wecut list fonts)", id, rd.Font))
			}
		}
		if rd.Background != "" && rd.Background != "none" && !answerHex.MatchString(rd.Background) {
			out = append(out, fmt.Sprintf("%s: background %q must be #RRGGBB or none", id, rd.Background))
		}
		if rd.Y != nil && (*rd.Y < 0 || *rd.Y > 1) {
			out = append(out, fmt.Sprintf("%s: y is a fraction of the height, 0 to 1", id))
		}
		if rd.Size != 0 && (rd.Size < 8 || rd.Size > 400) {
			out = append(out, fmt.Sprintf("%s: size must be 8 to 400 pixels at 1080", id))
		}
		for _, c := range []string{rd.Color, rd.EmphasisColor, rd.Highlight} {
			if c != "" && !answerHex.MatchString(c) {
				out = append(out, fmt.Sprintf("%s: colour %q must look like #RRGGBB", id, c))
			}
		}
		if rd.Stroke != "" && rd.Stroke != "none" && !answerHex.MatchString(rd.Stroke) {
			out = append(out, fmt.Sprintf("%s: stroke %q must be #RRGGBB or none", id, rd.Stroke))
		}
		if rd.Mode != "" && !slices.Contains(spec.CaptionModes, rd.Mode) {
			out = append(out, fmt.Sprintf("%s: mode must be one of %s", id, strings.Join(spec.CaptionModes, ", ")))
		}
	}
	return out
}
