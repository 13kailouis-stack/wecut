# Clone a video into a template

`wecut clone` takes a finished short video and writes a template of it: a
recipe whose shots have the original's exact lengths, transitions, framing
and zoom, with slots for the text cards, the captions and the logo. Fill the
slots with new footage, words and a logo and the new video is cut the same
way. People fill templates in WeCut Studio; agents fill them with
`wecut recipe apply` or the `wecut_recipe_apply` MCP tool.

```sh
wecut clone media/viral.mp4 --name news-card
# -> recipes/news-card.json            the template (wecut.recipe/1)
#    recipes/news-card/music.m4a       the original's sound, when it is music
#    out/clone/news-card/slots.jpg     one frame per media slot, numbered
#    out/clone/news-card/compare.jpg   original (left) and template (right)
#    out/clone/news-card/preview.json  the template filled with stand-ins; render it to watch
#    out/clone/news-card/text1.png ... the text crops, to read or check
```

Every clone checks itself: the template is rendered with stand-ins, its
cuts and transitions are measured again, and `timing` in the output lists
each one beside the original's. Hard cuts land on the same frame (the
source is read at its own frame rate, up to 60, and the template plays at
that rate); a transition starts and ends at the same moments, while the
middle of its effect (a flash's peak, a zoom's blur) can sit a few frames
apart because the template's effect is not the original editor's.

The template copies structure only. The original's footage never goes into
it; its words are kept as default text you are expected to replace; its
sound is offered as default music only when it is music, with a note that a
song taken from someone else's video may be muted by the platform.

## What it finds

| | How | In the recipe |
|---|---|---|
| Hard cuts | frame change on the most-changed half of a 4x6 grid, against the motion around it (works inside bars too) | one `shot` per cut, exact `dur` |
| Flash, fade through black | runs of nearly white or black frames | `join` / `transitionOut` `flash`, `fadeblack` with the measured duration |
| Dissolve, zoom, blur transitions | a run of strong change whose ends differ in colour; a blend test tells a dissolve, a sharpness dip a blur | `dissolve`, `zoomin`, `hblur` |
| Glitch bursts | cuts closer than 0.25 s fold into one | one cut |
| Framing | dark bars or soft (blurred) bands at both ends of the frame | `fit: contain`, `output.background` black or `blur` |
| Push in / pull out | the end frame is the start frame scaled, and the middle sits halfway | `zoom`, `zoomTo` |
| Punch-in on one take | the frame after a cut is the frame before it, scaled | `continue: true`, `zoom` |
| Still shots | almost no change inside the shot | shot `about`: a photo or screenshot |
| Text cards | lines of edges followed frame to frame; kept when they stay put while the picture behind them changes, last across a cut, or come and go mid-shot | `texts` slots with font size, colour, box (`card` or per line), line spacing, padding, alignment, italic, uppercase, key-word colour, timing and fade |
| Captions | short-lived words that keep returning to one centred band | `captions` style, position and mode (`pop` for one word at a time) |
| Logo | text-like mark near an edge for most of the video | `overlays` slot |
| Hook over talk | silent B-roll under an opening card while speech already plays | hook section `audio: "next"` (a J-cut) |
| Platform outro | a silent dark still ending | left out (`source.used`) |

Colours, boxes and letter sizes are read from full-size frames. Words come
from Tesseract when it is installed (`--ocr auto`, the default); otherwise
the text slots get placeholder words and the crops are written for a model
or a person to read, then passed back with `--read`:

```sh
wecut clone media/viral.mp4 --name news-card --force --read words.json
# words.json: {"text1": ["FLOX NEWS"], "text1-2": ["First line", "second line"], "caption1": ["WORD"]}
```

A laid-over mark with no readable words (a burned-in timer, a channel bug, a
watermark such as a platform's @handle) is reported and left out of the
template.

## The template

A cloned recipe is an ordinary `wecut.recipe/1` file
([schema](../schema/wecut.recipe.schema.json)). What clone adds to it:

```jsonc
{
  "recipe": "1",
  "name": "news-card",
  "source": {"file": "media/viral.mp4", "duration": 30.1, "used": 28.03, "tool": "wecut 0.3.0 clone"},
  "output": {"ratio": "9:16", "res": 1080, "fps": 30, "background": "#000000"},
  "maxDuration": 28.04,
  "sections": [
    {"role": "hook", "fit": "cover", "audio": "next",
     "transitionOut": {"type": "flash", "duration": 0.53},
     "shots": [
       {"dur": 2.033, "about": "full-screen footage", "ref": 1.02},
       {"dur": 1.3, "zoomTo": 1.12, "about": "full-screen footage, slow push in", "ref": 2.68}
     ]},
    {"role": "body", "fit": "contain",
     "shots": [{"dur": 12.77, "about": "landscape footage shown whole on a black background, someone talking (captions run here)"}]}
  ],
  "texts": [
    {"slot": "headline", "text": "Tolak Ngantor di Jakarta,\nAmbisi Bos Ini Bangun",
     "start": 0, "end": 5.97, "x": 0.046, "y": 0.756, "align": "left",
     "font": "poppins", "size": 70.8, "italic": true, "color": "#020202",
     "background": "#FEFEFE", "box": "card", "lineHeight": 1.0, "padding": [18, 26], "maxWidth": 0.92}
  ],
  "captions": {"mode": "pop", "maxWords": 1, "y": 0.632, "size": 120, "color": "#FDF68D", "uppercase": true},
  "overlays": [{"slot": "logo", "x": 0.238, "y": 0.103, "width": 0.28}]
}
```

- `shots` are the media slots. `dur` includes the overlap of a transition
  into the next shot, so the template plays exactly as long as the
  original. `ref` is where the shot sits in the source, for thumbnails.
- `continue` shots take the seconds right after the shot before, from the
  same clip.
- `audio: "next"` plays the next section's sound under this section: the
  talk's opening seconds are heard over the B-roll and the talk's footage
  then starts that much later, so nothing repeats.
- Text with `lineHeight` is laid out line by line with the font's own
  widths, and its background is drawn as one card sized to the text (or one
  box per line): the same text box an editor shows.

## Filling a template

```jsonc
// wecut recipe apply --input request.json --write out/mine.json
{
  "recipe": "news-card",
  "clips": [
    {"src": "media/broll-1.mp4", "role": "hook"},
    {"src": "media/broll-2.mp4", "role": "hook"},
    {"src": "media/podcast.mp4", "from": 120, "role": "body"}
  ],
  "fill": "auto",
  "texts": {"headline": {"text": "Your headline\non two lines", "emphasis": ["two lines"]}},
  "transcript": "media/podcast.words.json",
  "overlays": {"logo": "media/logo.png"}
}
```

`fill` decides how clips go into a section's shots: `each` gives every slot
its own clip (the CapCut way), `sequence` plays the clips one after another
so one long take feeds every slot, and `auto` (the default) uses `each` when
there are at least as many clips as slots. Footage that runs out wraps
around, with a warning.

## Using it from other tools

| Surface | Use |
|---|---|
| CLI | `wecut clone` prints one JSON object (events, slots, texts with crops and looks, captions, logos, hints). Exit codes as for every command. |
| MCP | `wecut_clone` returns the same JSON plus the comparison sheet, the slot sheet and the text crops as images, so a model can check the result and read the words. |
| Go | `internal/clone` is pure Go with no I/O: feed it grey frames (`AddLow`, `AddHigh`) and samples (`SetAudio`), call `Analyze`, give it the full-size frames it asks for (`Measure`), then `Build`. The same code can run in a browser as WebAssembly, with the page decoding frames. |
| Files | The recipe is plain JSON with a JSON Schema; `wecut recipe pack` bundles it with its music into one `.wecut.json` for another device or for WeCut Studio. |
| Studio | The Klon tab clones in the browser and opens the result as a project: the video sits in every slot, the cards on the text tracks, the sound on the music track. **Impor template** opens any recipe or `.wecut.json` with numbered stand-ins; pick a slot and **Isi slot ini** / **Ganti media** to put your own footage in. **Ekspor** keeps the template's shots, transitions, zoom, captions, logo slot and text looks. |

## Limits

- One frame per 1/30 s is looked at for timing (a 60 fps source is read at
  30), and 6 sharper frames a second for text.
- Up to 300 s of a source is cloned.
- Fonts are matched to the 13 bundled ones by weight, slant and shape, not
  identified; gradient or textured text gets its main colour.
- Stickers, emoji, masks, speed ramps, keyframed text paths and colour
  grades are not cloned yet.
- Text animations other than fade-in are not detected yet.
