---
name: wecut
description: Cut, caption and edit videos locally with the `wecut` CLI (FFmpeg on this machine, no cloud, no API key). Use when asked to make short-form clips, add captions or karaoke subtitles, trim or join videos, turn photos into a slideshow, add text, logos, music or transitions, or render 9:16 / 1:1 / 16:9 video.
---

# WeCut

`wecut` renders a JSON project into an MP4 (or GIF) with the FFmpeg installed on this machine. You describe the edit; WeCut does the video work. It never calls a network service.

If `wecut` is not on PATH, use `npx -y wecut` (Node 16+) in its place everywhere below. If WeCut is connected as an MCP server, the same steps are the tools `wecut_doctor`, `wecut_probe`, `wecut_validate`, `wecut_preview` (returns the frames as images), `wecut_render`, `wecut_list`, `wecut_schema` and `wecut_example`; pass the project as `project` (object) or `projectFile` (path).

## Workflow

1. **Check the machine once:** `wecut doctor`. If it reports FFmpeg missing, follow its `hint` (for example `sudo apt-get install -y ffmpeg`).
2. **Put media in the workspace.** Every path in a project is relative to the folder you run `wecut` from (or `--root DIR`). URLs are refused: download files first.
3. **Look at the media:** `wecut probe media/talk.mp4 media/music.mp3` gives duration, size, fps and whether there is audio. Use these numbers for `from`/`to`.
4. **Write `project.json`** (format below). `wecut example podcast-clip` prints a full example; `wecut schema` prints the JSON Schema; `wecut list` shows every style, font, transition and filter name.
5. **Validate:** `wecut validate project.json`. Errors come back with a JSON pointer (`/clips/0/to`) and a `hint`. Fix and repeat.
6. **Look before rendering:** `wecut preview project.json --at 0.5,3,10` writes small JPG frames to `out/preview/`. Open them and check framing, text size and caption position.
7. **Render:** `wecut render project.json`. Use `--preset draft` for a fast 540p check, the default `final` for delivery.

Every command prints one JSON object. Exit codes: 0 ok, 2 invalid input, 3 missing dependency, 4 render failed, 5 over a limit. You can pipe a project in with `wecut render -`.

## Project format

```json
{
  "wecut": "1",
  "output": { "file": "out/clip-01.mp4", "ratio": "9:16", "res": 1080, "fps": 30 },
  "clips": [
    { "src": "media/talk.mp4", "from": 754.2, "to": 781.0, "focus": { "x": 0.42, "y": 0.5 } },
    { "src": "media/talk.mp4", "from": 795.5, "to": 812.0, "zoom": 1.15 }
  ],
  "texts": [
    { "text": "How I made my first $10k", "start": 0, "end": 3, "style": "hook", "y": 0.2, "in": { "type": "pop" } }
  ],
  "captions": { "words": "media/talk.words.json", "src": "media/talk.mp4", "style": "karaoke" },
  "audio": [ { "src": "media/music.mp3", "volume": 25, "loop": true, "duck": true, "fadeOut": 1.5 } ]
}
```

Rules that matter:

- **Two clocks.** Clip `from`/`to` are seconds of the *source file*. Everything else (`texts`, `overlays`, `audio.at`, `filters`, `effects`) is seconds of the *finished video*. `wecut validate` prints the finished `duration`.
- **Transitions overlap.** A `transition` on clip N blends into clip N+1 and makes the video shorter by its `duration`.
- **Framing.** Default `fit` is `cover` (fill and crop). Steer the crop with `focus` (0..1; x 0.3 keeps the left side) and `zoom`. Use `"fit": "contain"` with `"background": "blur"` to show the whole frame.
- **Positions** are fractions of the canvas, 0.5 is the centre. Font `size` is pixels at 1080p.
- **Images** use `duration` (default 3) and can have `motion`: in, out, left, right, up, down.
- **Emoji** are not drawn in text. Put them in a PNG and use `overlays`.

## Captions

Give WeCut a transcript with word timings and it builds subtitles:

- `words`: a `.json`, `.srt` or `.vtt` path, or an inline array `[{"word": "hi", "start": 0.1, "end": 0.4}]`. OpenAI/Groq `verbose_json` and whisper.cpp JSON are read as they are.
- `src`: set this to the video the transcript belongs to. Then the transcript keeps the source file's own times, and WeCut moves every word onto the output timeline through each clip cut from that file (speed included). Without `src`, times must already be output times.
- `style`: `classic`, `yellow`, `box-black`, `box-white`, `bold`, `comic`, `shadow` (a phrase at a time), `karaoke`, `karaoke-green` (spoken word lights up), `pop` (one big word at a time).

If you need a transcript: `wecut doctor` reports `local_transcription: true` when this FFmpeg has the Whisper filter. Otherwise use whatever speech-to-text you already have and save its word timings as JSON.

## Making several clips from one long video

1. `wecut probe` the source and get a transcript with word timings.
2. Read the transcript and pick the moments (you decide; WeCut does not judge content).
3. Write one project per clip, each with `captions.src` pointing at the source so one transcript serves every clip.
4. `wecut preview` one of them, adjust `focus`, then `wecut render` each.

## Troubleshooting

- `E_RANGE`: `to` is past the end of the file. Use `wecut probe` for the real duration.
- `E_PATH`: the path is outside the workspace or is a URL.
- `E_MEDIA`: the file is not a readable video, image or audio file, or the wrong kind for that field (music in `clips`, video in `overlays`).
- `E_DEPENDENCY`: FFmpeg missing, older than 6, or built without libass. Run `wecut doctor`.
- Render too slow: `--preset draft`, a lower `output.res`, or fewer clips per project. `render_seconds` and `realtime_factor` in the result tell you the speed on this machine.
- To see exactly what FFmpeg will run: `wecut render project.json --dry-run`.
