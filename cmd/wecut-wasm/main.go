//go:build js && wasm

// Command wecut-wasm is the clone analyzer for browsers: WeCut Studio
// decodes a video with the page's own <video> element, hands the frames
// to this module, and gets back the same template `wecut clone` writes.
//
//	globalThis.wecutClone.start(configJSON)
//	globalThis.wecutClone.addLow(Uint8Array)   grey frames, LowSize, LowFPS
//	globalThis.wecutClone.addHigh(Uint8Array)  grey frames, HighSize, HighFPS
//	globalThis.wecutClone.setAudio(Int16Array, rate)
//	globalThis.wecutClone.analyze() -> plan JSON (stills: times of full-size frames wanted)
//	globalThis.wecutClone.measure(w, h, [Uint8Array RGB, ...]) -> crops JSON
//	globalThis.wecutClone.build(optionsJSON) -> result JSON (recipe, slots, hints)
//	globalThis.wecutClone.debug() -> every measurement, when started with Debug: true
//
// Every call returns a JSON string; errors come back as {"error": "..."}.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/13kailouis-stack/wecut/internal/clone"
)

type config struct {
	W, H     int
	Duration float64
	LowFPS   float64
	HighFPS  float64
	Debug    bool
}

var (
	an    *clone.Analyzer
	cfg   config
	crops []clone.Crop
)

func jsonOut(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"` + err.Error() + `"}`
	}
	return string(b)
}

func bytesOf(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

func main() {
	api := map[string]any{
		"start": js.FuncOf(func(_ js.Value, args []js.Value) any {
			if err := json.Unmarshal([]byte(args[0].String()), &cfg); err != nil {
				return jsonOut(map[string]string{"error": err.Error()})
			}
			an = clone.New(clone.Config{W: cfg.W, H: cfg.H, Duration: cfg.Duration, LowFPS: cfg.LowFPS, HighFPS: cfg.HighFPS})
			if cfg.Debug {
				an.Debug()
			}
			lw, lh := clone.LowSize(cfg.W, cfg.H)
			hw, hh := clone.HighSize(cfg.W, cfg.H)
			return jsonOut(map[string]int{"lowW": lw, "lowH": lh, "highW": hw, "highH": hh})
		}),
		"addLow": js.FuncOf(func(_ js.Value, args []js.Value) any {
			lw, lh := clone.LowSize(cfg.W, cfg.H)
			an.AddLow(&clone.Gray{W: lw, H: lh, Pix: bytesOf(args[0])})
			return nil
		}),
		"addHigh": js.FuncOf(func(_ js.Value, args []js.Value) any {
			hw, hh := clone.HighSize(cfg.W, cfg.H)
			an.AddHigh(&clone.Gray{W: hw, H: hh, Pix: bytesOf(args[0])})
			return nil
		}),
		"setAudio": js.FuncOf(func(_ js.Value, args []js.Value) any {
			raw := bytesOf(js.Global().Get("Uint8Array").New(args[0].Get("buffer"), args[0].Get("byteOffset"), args[0].Get("byteLength")))
			pcm := make([]int16, len(raw)/2)
			for i := range pcm {
				pcm[i] = int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8)
			}
			an.SetAudio(pcm, args[1].Int())
			return nil
		}),
		"debug": js.FuncOf(func(_ js.Value, _ []js.Value) any {
			return jsonOut(an.DebugInfo())
		}),
		"analyze": js.FuncOf(func(_ js.Value, _ []js.Value) any {
			return jsonOut(an.Analyze())
		}),
		"measure": js.FuncOf(func(_ js.Value, args []js.Value) any {
			w, h := args[0].Int(), args[1].Int()
			list := args[2]
			frames := make([]*clone.RGB, list.Length())
			for i := range frames {
				frames[i] = &clone.RGB{W: w, H: h, Pix: bytesOf(list.Index(i))}
			}
			crops = an.Measure(frames)
			type crop struct {
				ID, Kind string
				At       float64
				Rect     clone.Rect
			}
			out := make([]crop, len(crops))
			for i, c := range crops {
				out[i] = crop{c.ID, c.Kind, c.At, c.Rect}
			}
			return jsonOut(out)
		}),
		"build": js.FuncOf(func(_ js.Value, args []js.Value) any {
			var o clone.Options
			if err := json.Unmarshal([]byte(args[0].String()), &o); err != nil {
				return jsonOut(map[string]string{"error": err.Error()})
			}
			o.Crops = crops
			return jsonOut(an.Build(o))
		}),
	}
	js.Global().Set("wecutClone", js.ValueOf(api))
	select {}
}
