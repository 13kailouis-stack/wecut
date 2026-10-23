# WeCut

A video editing engine for AI agents. An agent writes a small JSON project; `wecut` renders it with the FFmpeg already on the machine. No cloud, no API key, no GPU required.

```sh
npx -y wecut doctor                           # is FFmpeg ready?
npx -y wecut probe media/talk.mp4             # duration, size, fps, audio
npx -y wecut validate project.json            # errors with JSON pointers and fix hints
npx -y wecut preview project.json --at 1,5,9  # small JPG frames to look at
npx -y wecut render project.json              # out/clip.mp4
npx -y wecut mcp                              # the same, as an MCP server on stdio
```

Every command prints one JSON object on stdout. Exit codes: `0` ok, `2` invalid input, `3` missing dependency, `4` render failed, `5` over a limit.

## What it does

- Cut, join and speed up clips (0.25x to 4x), with 9:16, 16:9, 1:1, 4:5 and other canvases
- Cover crop with a focus point and zoom, or contain on a colour or blurred background
- Photos with Ken Burns motion
- 36 transitions (FFmpeg `xfade`), clip fades
- Text with 14 styles, 13 bundled fonts, pop / type / slide / fade animations
- Captions from a transcript: phrase, karaoke (spoken word highlighted) or one word at a time. Transcript times can stay in source-file seconds; WeCut maps them through the cuts
- Image overlays (logos, stickers) with opacity, rotation and fades
- Music and sound effects with trim, loop, fades and automatic ducking under speech
- Colour filters and effects (flash, cinema bars, frame, dim, night, sepia)
- MP4 (H.264 + AAC, faststart) or GIF output

## Install

FFmpeg 6 or newer with libass is required (`wecut doctor` checks it):

- Debian/Ubuntu: `sudo apt-get install -y ffmpeg`
- macOS: `brew install ffmpeg-full` (the slim `ffmpeg` formula has no libass; WeCut finds ffmpeg-full on its own)
- Windows: `winget install Gyan.FFmpeg`

Then pick one:

- **npm** (Node 16+): `npx -y wecut <command>`, or `npm i -g wecut`. npm installs only the binary for your platform (`@wefluence/wecut-<os>-<arch>`); there are no install scripts.
- **Binary**: download the archive for your OS from GitHub Releases and check it against `SHA256SUMS`.
- **Source** (Go 1.22+): `go build -o wecut ./cmd/wecut`

Prebuilt for Linux, macOS and Windows on x64 and arm64. Set `WECUT_BINARY` to use your own build through the npm launcher.

## MCP server

`wecut mcp` serves the same features to any MCP client over stdio. The workspace is fixed when the server starts (`--root`, default: the folder it was launched in); tool arguments cannot move it.

```sh
claude mcp add wecut -- npx -y wecut mcp          # Claude Code (workspace = the project folder)
```

```json
{
  "mcpServers": {
    "wecut": { "command": "npx", "args": ["-y", "wecut", "mcp", "--root", "/path/to/workspace"] }
  }
}
```

That block works for Claude Desktop, Cursor and most clients; give `--root` explicitly, because desktop apps start servers from an unpredictable folder.

| Tool | Does |
|---|---|
| `wecut_doctor` | checks FFmpeg, encoders and filters |
| `wecut_probe` | duration, size, fps, audio of media files |
| `wecut_validate` | checks a project; errors carry a JSON pointer and a hint |
| `wecut_preview` | renders frames and returns them as images, so the model can look |
| `wecut_render` | renders MP4/GIF, with progress notifications |
| `wecut_list`, `wecut_schema`, `wecut_example` | names, the project format, working examples |

Projects go in as an object (`project`) or a workspace file (`projectFile`). The server speaks both protocol eras: modern clients (2026-07-28, per-request `_meta`, `server/discover`) and clients that open with `initialize` (2024-11-05 to 2025-11-25). Renders run one at a time; `notifications/cancelled` stops one and removes its partial file.

## For agents

Give your agent [`skill/wecut/SKILL.md`](skill/wecut/SKILL.md). It explains the workflow, the two clocks (source seconds vs output seconds), captions and troubleshooting. `wecut schema` prints the full JSON Schema and `wecut example <name>` prints a working project.

```json
{
  "wecut": "1",
  "output": { "file": "out/clip-01.mp4", "ratio": "9:16" },
  "clips": [ { "src": "media/talk.mp4", "from": 754.2, "to": 781.0, "focus": { "x": 0.42, "y": 0.5 } } ],
  "texts": [ { "text": "How I made my first $10k", "end": 3, "style": "hook", "y": 0.2, "in": { "type": "pop" } } ],
  "captions": { "words": "media/talk.words.json", "src": "media/talk.mp4", "style": "karaoke" },
  "audio": [ { "src": "media/music.mp3", "volume": 25, "loop": true, "duck": true } ]
}
```

## Safety

WeCut runs on someone else's machine and takes instructions from a model that may have read hostile content. So:

- FFmpeg is started with an argument list, never through a shell.
- Text never enters the FFmpeg filter graph. All text goes through an `.ass` file with braces and backslashes escaped, so a caption cannot inject override tags or filters.
- Every input and output must be inside the workspace (`--root`, default: current folder). `..`, absolute paths outside it, symlinks pointing out, and URLs are refused. Output may not overwrite an input.
- Inputs are opened with `-protocol_whitelist file` and a demuxer whitelist (MP4/MOV, Matroska/WebM, common audio and image formats). HLS playlists, concat lists and network protocols are refused, including a playlist renamed to `.mp4`.
- Limits: 10 minutes of output, 4K, 100 clips; renders time out (default scales with length).
- Renders are written to a temporary name, checked with ffprobe (size, duration, audio), and only then moved into place.
- No telemetry. WeCut makes no network requests.

## Speed

Measured on a 2019 laptop (Intel i5-1035G7, 4 cores, under load), `final` preset, 1080x1920 unless noted:

| Project | Video length | Render time |
|---|---|---|
| 2 cuts from a 14-min 720p source, karaoke captions, ducked music | 43.3 s | 27.6 s |
| 3 photos with zoom/pan, 2 transitions, text, music | 7.9 s | 9.4 s |
| 3 clips, slide transition, contain, Ken Burns, logo, captions (720p) | 7.4 s | 4.0 s |
| Preview frames (540p JPG) | any | 0.3 to 4 s |

Encoding dominates; text and captions cost almost nothing. A plain cut without re-encoding is not used, because any crop, caption or effect needs a re-encode.

## Project layout

```
cmd/wecut          entry point (CLI and `wecut mcp`)
internal/spec      project format: types, strict decoding, validation, catalog
internal/engine    timeline, captions, .ass builder, FFmpeg graph compiler (pure, no I/O)
internal/ff        finding, probing and running FFmpeg
internal/cli       the commands; every result is one JSON object
internal/mcp       MCP stdio server, a thin layer over internal/cli
internal/workspace keeps every path inside the workspace
internal/assets    fonts embedded in the binary
schema/            JSON Schema (kept in sync with the Go types by a test)
examples/          example projects (`wecut example`)
skill/wecut/       Agent Skill
npm/wecut/         the npm launcher (`npx wecut`)
tools/release/     builds all platforms, npm packages and release archives into dist/
```

Release build: `go run ./tools/release -version 0.1.0` (nothing is published; see `.github/workflows/release.yml`).

Tests: `go test ./...` (the CLI tests render real video and are skipped without FFmpeg). Golden FFmpeg plans live in `internal/engine/testdata`; after an intended change run `go test ./internal/engine -update` and render the examples to check them.

## Roadmap

- `uvx wecut` (PyPI wheels), an MCPB bundle and a listing in the MCP Registry
- `wecut analyze`: silences, scene changes, loudness, local transcription via FFmpeg's Whisper filter
- Video overlays (picture-in-picture), stickers, more effects, loudness normalisation
- Optional Wefluence bridge for paid clipping campaigns (separate package)

## License

Code: Apache License 2.0 (see `LICENSE`). Bundled fonts: SIL Open Font License 1.1 or Apache 2.0, see `internal/assets/fonts/LICENSES.md`.
