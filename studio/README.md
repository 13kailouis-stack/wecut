# WeCut Studio

A visual template editor for humans, served as static files. Video, photos and music are read in the user's browser. The exported `.wecut.json` travels to an agent on another device, where WeCut and FFmpeg render new footage.

## Desktop workspace

The workspace follows familiar desktop editor placement: media/audio/text/filter tabs at the upper left, player in the centre, properties at the right, and a full-width timeline below. Panels scroll independently. Drag the divider above the timeline to resize it. Desktop layout needs at least 760px of width; this is not a mobile editor.

Video sections, individual text layers and the music track appear at their actual times. Drag a video block to reorder; drag text or audio to move its timing. Drag block edges to trim. Click the time ruler to seek, use the zoom slider or **Sesuaikan** to fit the project, and select a block to inspect its settings. Video thumbnails use one representative frame repeated across a clip; audio waveforms are decoded locally when the browser supports the source (up to 16 MiB and ten minutes).

Shortcuts outside text fields: **Space** play/pause, **← / →** one frame, **Ctrl/Cmd+B** split at the playhead, **Delete** remove the selected item, **Ctrl/Cmd+Z** undo, **Ctrl/Cmd+Shift+Z** redo. Undo retains up to 50 editing states in memory; it is not autosave. Trimming video can leave text outside the new duration; export validation asks you to correct those timings.

The layout is informed by the [official CapCut desktop workspace reference](https://www.capcut.com/resource/capcut-for-laptop). WeCut retains its own branding and supported controls; this is not complete CapCut feature parity.

## Template from a finished video (Klon)

The **Klon** tab turns a finished video into a template on the user's device: the same detectors as `wecut clone` run as WebAssembly (`engine/clone.wasm`), fed by frames the page decodes with its own video element. It lists the media slots with a thumbnail, length, framing and transition, the text slots (the headline card and its label start ticked; tick others and type their words, since the browser has no OCR), captions, the logo slot and notes, and opens the template as a project in the editor: every slot holds its piece of the original video, the headline card and label sit on the text tracks (tick other texts in the Klon tab), and the sound is on the music track when it is music. Select a slot and **Ganti media** to try your own footage; **Ekspor template** downloads a `.wecut.json` that keeps the template's exact shots, transitions, zoom, J-cut sound, captions and logo slot. **Impor template** (top bar) opens any recipe or `.wecut.json` the same way, with numbered stand-ins in the slots (**Isi slot ini**). A 30 s video takes about a minute. Rebuild the engine after changing `internal/clone` with `go run ./tools/wasm`; `node --test studio/clone.test.mjs` runs it under Node.

The page fetches only its own files (`connect-src 'self'`) and needs `'wasm-unsafe-eval'` to start WebAssembly; nothing is uploaded.

## Run locally

From the repository root:

```sh
python -m http.server 8770 --bind 127.0.0.1 --directory studio
```

Open `http://127.0.0.1:8770`. Use an HTTP server; ES modules cannot reliably load from `file://`. No build, npm dependencies or backend are needed. The local address is accessible only on this computer.

## Make it publicly accessible

Deploy the contents of `studio/` to a static host, including `fonts/`. Use HTTPS and serve `.mjs` as JavaScript. All paths are relative, so subdirectory hosting works. This serves the editor's HTML, CSS, JavaScript and fonts; it does not receive media or render videos. Hosting bandwidth and any domain are separate from rendering costs. This repository does not automatically deploy the site.

A public editor URL opens a separate editing session on each device. It does **not** share the current project, local files or localhost. Download the template and attach it to the agent chat with replacement footage. Public template links would require a separate storage service, which is not included.

## Create and send

1. Choose local video or photos; trim clips and arrange their order.
2. Add text slots, adjust font, colours, timing and position, and choose a canvas ratio.
3. Add optional default music and adjust its source offset, timeline position, duration and volume.
4. Preview, then choose **Unduh template** and review what is included.
5. Send the downloaded file and replacement footage to the agent. Ask it to retain the music and return a preview before rendering the final video.

The agent needs WeCut **0.2.0+**. The published npm 0.1.0 lacks bundle import; use the 0.2.0 source/build until that release is published. See the [agent workflow](../docs/template-authoring.md#agent-workflow).

## Boundaries

- Editing and preview run locally; no uploads, analytics, remote fonts or API keys. CSP blocks application network connections. The static host still receives ordinary requests for site files.
- Export includes template structure, default text and one optional music asset. Example clip footage is excluded. If the music source is a video, its entire file, including its images and untrimmed parts, is included; the UI warns before download.
- Templates last 3–600 seconds, with up to 100 clips and 100 text slots. Music is limited to 32 MiB; the encoded package to 48 MiB.
- Preview is an approximation; review the agent's FFmpeg output. Browser codec support determines which files can be previewed.
- This first version supports trimming, ordering, fit, mute, photos, text, colour filters and one music track. No transitions, captions, effects, keyframes, video export, project autosave or reopening exported templates yet. Download before closing the tab.

## Checks

```sh
node --check studio/app.mjs
node --test studio/model.test.mjs studio/editing.test.mjs
go build -o studio-test-engine ./cmd/wecut
node studio/check-engine.mjs ./studio-test-engine
```

The integration check uses FFmpeg to generate synthetic fixtures, imports a package generated by the actual editor model into a temporary agent workspace, applies replacement media and checks preserved music timing.
