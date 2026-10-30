package engine

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

// FontData returns the bytes of a font by key. The assets package sets it
// when it is linked in (the CLI and MCP server); without it, as in the
// browser build, text cannot be measured and layout falls back to libass.
var FontData func(key string) ([]byte, error)

// fontMetrics is what laying out text by hand needs from a TrueType file:
// how wide each character is and where libass puts the baseline. libass
// sizes a font by its Windows ascent + descent, so with the CSS-style size
// fs a line's top sits winAsc*fs/upm above its baseline, and lines in one
// event are fs*Em apart.
type fontMetrics struct {
	upm, winAsc, winDesc, capH float64
	adv                        map[rune]float64 // advance in font units
	missing                    float64
}

var (
	metricsMu sync.Mutex
	metricsBy = map[string]*fontMetrics{}
)

// metricsFor parses an embedded font once.
func metricsFor(key string) (*fontMetrics, error) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	if m, ok := metricsBy[key]; ok {
		return m, nil
	}
	if FontData == nil {
		return nil, errors.New("no font data")
	}
	data, err := FontData(key)
	if err != nil {
		return nil, err
	}
	m, err := parseMetrics(data)
	if err != nil {
		return nil, err
	}
	metricsBy[key] = m
	return m, nil
}

// width of s in pixels at CSS size fs, with extra spacing per character.
func (m *fontMetrics) width(s string, fs, spacing float64) float64 {
	var w float64
	n := 0
	for _, r := range s {
		a, ok := m.adv[r]
		if !ok {
			a = m.missing
		}
		w += a
		n++
	}
	return w*fs/m.upm + spacing*float64(n)
}

func parseMetrics(f []byte) (*fontMetrics, error) {
	bad := errors.New("unreadable font")
	if len(f) < 12 {
		return nil, bad
	}
	be := binary.BigEndian
	tables := map[string][]byte{}
	n := int(be.Uint16(f[4:]))
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		if rec+16 > len(f) {
			return nil, bad
		}
		off, ln := int(be.Uint32(f[rec+8:])), int(be.Uint32(f[rec+12:]))
		if off < 0 || ln < 0 || off+ln > len(f) {
			return nil, bad
		}
		tables[string(f[rec:rec+4])] = f[off : off+ln]
	}
	head, hhea, hmtx, os2, cmap := tables["head"], tables["hhea"], tables["hmtx"], tables["OS/2"], tables["cmap"]
	if len(head) < 54 || len(hhea) < 36 || len(os2) < 78 || len(cmap) < 4 {
		return nil, bad
	}
	m := &fontMetrics{upm: float64(be.Uint16(head[18:])), adv: map[rune]float64{}}
	m.winAsc, m.winDesc = float64(be.Uint16(os2[74:])), float64(be.Uint16(os2[76:]))
	m.capH = 0.7 * m.upm
	if be.Uint16(os2) >= 2 && len(os2) >= 90 {
		m.capH = float64(int16(be.Uint16(os2[88:])))
	}
	nh := int(be.Uint16(hhea[34:]))
	if nh == 0 || len(hmtx) < 4*nh {
		return nil, bad
	}
	advOf := func(g int) float64 {
		if g >= nh {
			g = nh - 1
		}
		return float64(be.Uint16(hmtx[4*g:]))
	}
	m.missing = advOf(0)

	// The best Unicode map: format 12 (full range) or format 4 (BMP).
	var sub []byte
	best := 0
	for i := 0; i < int(be.Uint16(cmap[2:])); i++ {
		rec := 4 + 8*i
		if rec+8 > len(cmap) {
			break
		}
		pid, eid, off := be.Uint16(cmap[rec:]), be.Uint16(cmap[rec+2:]), int(be.Uint32(cmap[rec+4:]))
		if off+4 > len(cmap) {
			continue
		}
		format := be.Uint16(cmap[off:])
		rank := 0
		switch {
		case format == 12 && (pid == 3 && eid == 10 || pid == 0):
			rank = 2
		case format == 4 && (pid == 3 && eid == 1 || pid == 0):
			rank = 1
		}
		if rank > best {
			best, sub = rank, cmap[off:]
		}
	}
	switch best {
	case 2:
		if len(sub) < 16 {
			return nil, bad
		}
		groups := int(be.Uint32(sub[12:]))
		for i := 0; i < groups && 16+12*i+12 <= len(sub); i++ {
			g := sub[16+12*i:]
			start, end, glyph := be.Uint32(g), be.Uint32(g[4:]), be.Uint32(g[8:])
			for c := start; c <= end && c < 0x30000; c++ {
				m.adv[rune(c)] = advOf(int(glyph + c - start))
			}
		}
	case 1:
		if len(sub) < 14 {
			return nil, bad
		}
		segs := int(be.Uint16(sub[6:])) / 2
		ends, starts := 14, 16+2*segs
		deltas, ranges := starts+2*segs, starts+4*segs
		if ranges+2*segs > len(sub) {
			return nil, bad
		}
		for s := 0; s < segs; s++ {
			end, start := int(be.Uint16(sub[ends+2*s:])), int(be.Uint16(sub[starts+2*s:]))
			delta, ro := int(int16(be.Uint16(sub[deltas+2*s:]))), int(be.Uint16(sub[ranges+2*s:]))
			for c := start; c <= end && c != 0xFFFF; c++ {
				g := 0
				if ro == 0 {
					g = (c + delta) & 0xFFFF
				} else {
					at := ranges + 2*s + ro + 2*(c-start)
					if at+2 > len(sub) {
						continue
					}
					if g = int(be.Uint16(sub[at:])); g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				if g != 0 {
					m.adv[rune(c)] = advOf(g)
				}
			}
		}
	default:
		return nil, bad
	}
	return m, nil
}

// wrap breaks text into lines no wider than maxW pixels, keeping the line
// breaks already in it. A single word wider than maxW gets a line of its
// own.
func (m *fontMetrics) wrap(text string, fs, spacing, maxW float64) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if m.width(line+" "+w, fs, spacing) <= maxW {
				line += " " + w
				continue
			}
			out = append(out, line)
			line = w
		}
		out = append(out, line)
	}
	return out
}

// CapRatio is a font's capital height as a share of its CSS size, for
// turning a measured letter height into a size: from the catalog, so it
// needs no font file. 0.7 when unknown.
func CapRatio(key string) float64 {
	if f, ok := spec.FontByKey(key); ok && f.Cap > 0 {
		return f.Cap
	}
	return 0.7
}

// TextWidth is how wide text is drawn in the font at CSS size fs, in the
// same pixels as fs. 0 when the font cannot be measured.
func TextWidth(key, text string, fs float64) float64 {
	m, err := metricsFor(key)
	if err != nil {
		return 0
	}
	return m.width(text, fs, 0)
}
