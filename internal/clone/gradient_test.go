package clone

import (
	"math"
	"testing"

	"github.com/13kailouis-stack/wecut/internal/spec"
)

func TestFadeFrom(t *testing.T) {
	// 100 rows: the picture at level 120, a linear fade from row 60 to row 80,
	// black to the bottom.
	row := make([]float64, 100)
	for y := range row {
		switch {
		case y < 60:
			row[y] = 120
		case y < 80:
			row[y] = 120 * float64(80-y) / 20
		}
	}
	g := fadeFrom(row, nil, 99, -1, 120)
	if g == nil || g.From < 0.57 || g.From > 0.63 || g.To < 0.77 || g.To > 0.82 {
		t.Fatalf("bottom fade: %+v", g)
	}
	// The top edge of the same picture is not dark.
	if g := fadeFrom(row, nil, 0, 1, 120); g != nil {
		t.Fatalf("top fade from nothing: %+v", g)
	}
	// A hard black bar has no fade to it.
	bar := make([]float64, 100)
	for y := 0; y < 80; y++ {
		bar[y] = 120
	}
	if g := fadeFrom(bar, nil, 99, -1, 120); g != nil {
		t.Fatalf("a black bar is not a fade: %+v", g)
	}
}

// A hook with its fade high and white text on the black, then a talk with its
// fade low: two gradients, each for its own shots.
func TestGradientsPerSection(t *testing.T) {
	const W, H = 20, 100
	frame := func(to, level int, text bool) *Gray {
		g := &Gray{W: W, H: H, Pix: make([]uint8, W*H)}
		from := to - 20
		for y := 0; y < H; y++ {
			v := level
			if y >= to {
				v = 0
			} else if y >= from {
				v = level * (to - y) / 20
			}
			if text && y >= to+5 && y < to+15 {
				v = 200
			}
			for x := 0; x < W; x++ {
				g.Pix[y*W+x] = uint8(v)
			}
		}
		return g
	}
	a := &Analyzer{cfg: Config{LowFPS: 10}}
	var shots []ShotInfo
	add := func(start, end float64, f *Gray) {
		shots = append(shots, ShotInfo{Start: start, End: end, Fit: "cover"})
		for len(a.lowGray) < int(end*10) {
			a.lowGray = append(a.lowGray, f)
		}
	}
	add(0, 1, frame(60, 120, true))
	add(1, 2, frame(60, 130, true))
	add(2, 3, frame(60, 110, true))
	add(3, 6, frame(85, 140, false))
	add(6, 9, frame(85, 150, false))
	p := &Plan{Shots: shots, Blocks: []*Block{{Start: 0, End: 3, Box: Box{0.1, 0.65, 0.8, 0.1}}}}
	got := a.gradients(p)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	hook, talk := got[0], got[1]
	if hook.To < 0.56 || hook.To > 0.63 || hook.From < 0.36 || hook.From > 0.44 || hook.Start != 0 || hook.End == nil || *hook.End != 3 {
		t.Errorf("hook %+v", hook)
	}
	if talk.To < 0.81 || talk.To > 0.88 || talk.From < 0.61 || talk.From > 0.69 || talk.Start != 3 || talk.End != nil {
		t.Errorf("talk %+v", talk)
	}
	// Nearly the same fade in two groups is one fade.
	a = &Analyzer{cfg: Config{LowFPS: 10}}
	shots = nil
	add(0, 2, frame(85, 140, false))
	add(2, 4, frame(85, 150, false))
	add(4, 8, frame(60, 110, false)) // reaches further: fits the same group
	add(8, 12, frame(85, 150, false))
	if got := a.gradients(&Plan{Shots: shots}); len(got) != 1 || got[0].Start != 0 || got[0].End != nil {
		t.Errorf("one fade: %+v", got)
	}
	// A plain picture has no fade.
	a = &Analyzer{cfg: Config{LowFPS: 10}}
	flat := &Gray{W: W, H: H, Pix: make([]uint8, W*H)}
	for i := range flat.Pix {
		flat.Pix[i] = 120
	}
	shots = nil
	for k := 0; k < 4; k++ {
		add(float64(k), float64(k+1), flat)
	}
	if got := a.gradients(&Plan{Shots: shots}); len(got) != 0 {
		t.Errorf("flat picture: %+v", got)
	}
}

func TestGradientsFrom(t *testing.T) {
	measured := []spec.Gradient{{From: 0.6, To: 0.8, Color: "#000000"}, {From: 0.2, To: 0.1, Color: "#000000"}}
	from, to := 0.5, 0.9
	got := gradientsFrom(measured, map[string]Reading{
		"gradient1": {From: &from, Color: "#112233"},
		"gradient2": {Drop: true},
		"gradient3": {From: &from, To: &to},
		"text1":     {Drop: true},
	})
	if len(got) != 2 || got[0].From != 0.5 || got[0].To != 0.8 || got[0].Color != "#112233" || got[1].To != 0.9 || got[1].Color != "#000000" {
		t.Fatalf("got %+v", got)
	}
	if measured[0].From != 0.6 {
		t.Fatal("the measured gradient was changed")
	}
}

func TestSplitTightLines(t *testing.T) {
	// Two lines of letters 20 rows high, 4 rows apart, seen as one box.
	f := &RGB{W: 200, H: 100, Pix: make([]uint8, 3*200*100)}
	paint := func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 20; x < 180; x += 2 {
				i := 3 * (y*200 + x)
				f.Pix[i], f.Pix[i+1], f.Pix[i+2] = 255, 255, 255
			}
		}
	}
	paint(30, 50)
	paint(54, 74)
	on := func(c rgb) bool { return c.luma() > 128 }
	one := Rect{10, 28, 180, 48}.box(200, 100)
	got := splitTightLines(f, []Box{one}, on)
	if len(got) != 2 {
		t.Fatalf("%d lines: %+v", len(got), got)
	}
	if a, b := got[0].rect(200, 100), got[1].rect(200, 100); a.Y+a.H < 49 || a.Y+a.H > 54 || b.Y > 54 || b.Y < 50 {
		t.Fatalf("split at %+v, %+v", a, b)
	}
	// One line stays one, thin letter tops and tails or not.
	f = &RGB{W: 200, H: 100, Pix: make([]uint8, 3*200*100)}
	paint(40, 60)
	for y := 34; y < 40; y++ {
		i := 3 * (y*200 + 40)
		f.Pix[i], f.Pix[i+1], f.Pix[i+2] = 255, 255, 255
	}
	if got := splitTightLines(f, []Box{Rect{10, 30, 180, 40}.box(200, 100)}, on); len(got) != 1 {
		t.Fatalf("one line split: %+v", got)
	}
}

func TestSoundsLikeMusic(t *testing.T) {
	const rate = 8000
	tone := func(sec float64, gate func(t float64) float64) []int16 {
		out := make([]int16, int(sec*rate))
		for i := range out {
			tm := float64(i) / rate
			out[i] = int16(9000 * gate(tm) * math.Sin(2*math.Pi*220*tm))
		}
		return out
	}
	// A track with a soft beat holds its level.
	track := tone(6, func(t float64) float64 { return 0.75 + 0.25*math.Cos(2*math.Pi*1.2*t) })
	if !soundsLikeMusic(track, rate, 0, 6) {
		t.Error("a steady track read as talk")
	}
	// Phrases with gaps swing and drop.
	talk := tone(6, func(t float64) float64 {
		if math.Mod(t, 0.6) < 0.35 {
			return 1
		}
		return 0.05
	})
	if soundsLikeMusic(talk, rate, 0, 6) {
		t.Error("talk read as music")
	}
	if soundsLikeMusic(make([]int16, 6*rate), rate, 0, 6) {
		t.Error("silence read as music")
	}
	if soundsLikeMusic(nil, 0, 0, 6) {
		t.Error("no audio read as music")
	}
}

func TestHookSound(t *testing.T) {
	p := &Plan{Speech: true, HookEnd: 7, HookMusic: true}
	if m, end := p.HookSound(nil); !m || end != 7 {
		t.Errorf("measured: %v %v", m, end)
	}
	if m, _ := p.HookSound(map[string]Reading{"sound": {Mode: "talk"}}); m {
		t.Error("a person said talk")
	}
	p.HookMusic = false
	if m, _ := p.HookSound(map[string]Reading{"sound": {Mode: "music"}}); !m {
		t.Error("a person said music")
	}
	p.Speech = false // the whole video is music then: it plays throughout
	if m, _ := p.HookSound(map[string]Reading{"sound": {Mode: "music"}}); m {
		t.Error("no talk, no separate hook music")
	}
}
