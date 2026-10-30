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
	if p.Captions != nil {
		known["captions"] = true
	}
	var out []string
	ids := make([]string, 0, len(read))
	for id := range read {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rd := read[id]
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
		for _, c := range []string{rd.Color, rd.EmphasisColor} {
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
