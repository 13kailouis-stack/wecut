package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Word is one spoken word with its time in seconds.
type Word struct {
	Text       string  `json:"word"`
	Start, End float64 `json:"-"`
}

// Chunk is what one caption shows: a short run of words.
type Chunk struct {
	Start, End float64
	Words      []Word
}

func (c Chunk) Text() string {
	parts := make([]string, len(c.Words))
	for i, w := range c.Words {
		parts[i] = w.Text
	}
	return punctSpace.ReplaceAllString(strings.Join(parts, " "), "$1")
}

var punctSpace = regexp.MustCompile(`\s+([,.!?;:])`)

// ParseTranscript reads the transcript formats agents are likely to have:
// a word list, OpenAI/Groq verbose JSON, whisper.cpp JSON, SRT and WebVTT.
// Transcripts that only have phrase timings get word timings spread over
// each phrase by length, which is close enough for captions.
func ParseTranscript(data []byte, ext string) ([]Word, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	switch strings.ToLower(ext) {
	case ".srt", ".vtt":
		segs, err := parseCues(data)
		if err != nil {
			return nil, err
		}
		return wordsFromSegments(segs), nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("transcript is not valid JSON: %v", err)
	}
	words, segs, err := fromJSON(raw)
	if err != nil {
		return nil, err
	}
	if len(words) == 0 {
		words = wordsFromSegments(segs)
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("transcript has no words")
	}
	sort.SliceStable(words, func(i, j int) bool { return words[i].Start < words[j].Start })
	return words, nil
}

type segment struct {
	Text       string
	Start, End float64
}

func fromJSON(raw any) ([]Word, []segment, error) {
	switch x := raw.(type) {
	case []any:
		w, err := wordList(x, "")
		return w, nil, err
	case map[string]any:
		if arr, ok := x["words"].([]any); ok {
			w, err := wordList(arr, "/words")
			return w, nil, err
		}
		if arr, ok := x["segments"].([]any); ok {
			var words []Word
			var segs []segment
			for i, s := range arr {
				m, ok := s.(map[string]any)
				if !ok {
					return nil, nil, fmt.Errorf("/segments/%d is not an object", i)
				}
				if inner, ok := m["words"].([]any); ok && len(inner) > 0 {
					w, err := wordList(inner, fmt.Sprintf("/segments/%d/words", i))
					if err != nil {
						return nil, nil, err
					}
					words = append(words, w...)
					continue
				}
				st, ok1 := numOf(m["start"])
				en, ok2 := numOf(m["end"])
				if !ok1 || !ok2 {
					return nil, nil, fmt.Errorf("/segments/%d needs numeric start and end (seconds)", i)
				}
				segs = append(segs, segment{strOf(m["text"]), st, en})
			}
			return words, segs, nil
		}
		if arr, ok := x["transcription"].([]any); ok { // whisper.cpp -oj
			var segs []segment
			for i, s := range arr {
				m, _ := s.(map[string]any)
				off, _ := m["offsets"].(map[string]any)
				st, ok1 := numOf(off["from"])
				en, ok2 := numOf(off["to"])
				if !ok1 || !ok2 {
					return nil, nil, fmt.Errorf("/transcription/%d needs offsets.from and offsets.to (ms)", i)
				}
				segs = append(segs, segment{strOf(m["text"]), st / 1000, en / 1000})
			}
			return nil, segs, nil
		}
	}
	return nil, nil, fmt.Errorf("unrecognised transcript JSON: expected an array of {word, start, end}, or an object with \"words\", \"segments\" or \"transcription\"")
}

func wordList(arr []any, at string) ([]Word, error) {
	out := make([]Word, 0, len(arr))
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s/%d is not an object", at, i)
		}
		text := strOf(m["word"])
		if text == "" {
			text = strOf(m["text"])
		}
		st, ok1 := numOf(m["start"])
		en, ok2 := numOf(m["end"])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%s/%d needs numeric start and end (seconds)", at, i)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if en < st {
			en = st
		}
		out = append(out, Word{text, st, en})
	}
	return out, nil
}

func numOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

var cueTime = regexp.MustCompile(`(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})\s*-->\s*(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})`)

func parseCues(data []byte) ([]segment, error) {
	var segs []segment
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur *segment
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.Text) != "" {
			segs = append(segs, *cur)
		}
		cur = nil
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := cueTime.FindStringSubmatch(line); m != nil {
			flush()
			cur = &segment{Start: hms(m[1], m[2], m[3], m[4]), End: hms(m[5], m[6], m[7], m[8])}
			continue
		}
		if line == "" {
			flush()
			continue
		}
		if cur != nil {
			line = cueTags.ReplaceAllString(line, "")
			cur.Text = strings.TrimSpace(cur.Text + " " + line)
		}
	}
	flush()
	if len(segs) == 0 {
		return nil, fmt.Errorf("no cues found in subtitle file")
	}
	return segs, nil
}

var cueTags = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)

func hms(h, m, s, frac string) float64 {
	hv, _ := strconv.Atoi(h)
	mv, _ := strconv.Atoi(m)
	sv, _ := strconv.Atoi(s)
	for len(frac) < 3 {
		frac += "0"
	}
	ms, _ := strconv.Atoi(frac)
	return float64(hv*3600+mv*60+sv) + float64(ms)/1000
}

// wordsFromSegments spreads each phrase's time over its words in proportion
// to their length.
func wordsFromSegments(segs []segment) []Word {
	var out []Word
	for _, s := range segs {
		parts := strings.Fields(s.Text)
		if len(parts) == 0 {
			continue
		}
		total := 0
		for _, p := range parts {
			total += utf8.RuneCountInString(p) + 1
		}
		span := math.Max(0, s.End-s.Start)
		t := s.Start
		for _, p := range parts {
			d := span * float64(utf8.RuneCountInString(p)+1) / float64(total)
			out = append(out, Word{p, t, t + d})
			t += d
		}
	}
	return out
}

// MapWords converts word times from a source file onto the output timeline,
// through every clip cut from that file. A word is kept in a clip when at
// least half of it falls inside the clip's source range.
func MapWords(words []Word, clips []ClipT, src string) []Word {
	var out []Word
	for _, c := range clips {
		if c.Src != src || c.Kind != "video" {
			continue
		}
		for _, w := range words {
			s, e := math.Max(w.Start, c.From), math.Min(w.End, c.To)
			d := w.End - w.Start
			inside := e - s
			if d <= 0 {
				if w.Start < c.From || w.Start >= c.To {
					continue
				}
			} else if inside < d/2 {
				continue
			}
			out = append(out, Word{w.Text, c.Offset + (s-c.From)/c.Speed, c.Offset + (math.Max(e, s)-c.From)/c.Speed})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

var sentenceEnd = regexp.MustCompile(`[.!?…。！？]["')\]]*$`)

// Chunks groups words into captions the way the WeCut app does: at most
// maxWords words and maxDur seconds, breaking at sentence ends and at pauses
// longer than 0.45 s. Captions never overlap, never pass the end of the
// video, and stay on screen at least 0.4 s.
func Chunks(words []Word, total float64, maxWords int, maxDur float64) []Chunk {
	var out []Chunk
	var cur []Word
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Chunk{Start: cur[0].Start, End: cur[len(cur)-1].End, Words: cur})
			cur = nil
		}
	}
	for i, w := range words {
		if len(cur) > 0 {
			gap := i > 0 && w.Start-words[i-1].End > 0.45
			if len(cur) >= maxWords || w.End-cur[0].Start > maxDur || gap {
				flush()
			}
		}
		cur = append(cur, w)
		if sentenceEnd.MatchString(w.Text) {
			flush()
		}
	}
	flush()
	kept := out[:0]
	for i, c := range out {
		start := clamp(c.Start, 0, total)
		end := math.Max(c.End, start+0.4)
		if i+1 < len(out) {
			end = math.Min(end, out[i+1].Start)
		}
		end = math.Min(end, total)
		if end-start >= 0.15 {
			c.Start, c.End = start, end
			kept = append(kept, c)
		}
	}
	return kept
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }
