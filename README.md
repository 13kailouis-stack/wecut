# WeCut

[![CI](https://github.com/13kailouis-stack/wecut/actions/workflows/ci.yml/badge.svg)](https://github.com/13kailouis-stack/wecut/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/wecut)](https://www.npmjs.com/package/wecut)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**WeCut is a free, open-source video editing engine for AI agents.** The agent writes a small JSON project; `wecut` renders it with the FFmpeg already on the machine. It cuts, captions, reframes and mixes short-form video, and it can analyse a video that went viral and reuse its format as a template on new footage. CLI and MCP server. No cloud, no API key, no GPU, no telemetry.

Things you can ask an agent that has WeCut:

- "Cut this 40-minute podcast into five vertical clips with karaoke captions."
- "Add word-by-word captions to `talk.mp4` and put the music under it, quieter when he speaks."
- "This TikTok went viral. Work out why and make the same kind of video from my footage."
- "Turn these 8 photos into a 9:16 slideshow on the beat with a title."
- "Save this edit as a template so we can do the same every week."

```sh
npx -y wecut doctor                           # is FFmpeg ready?
npx -y wecut guide                            # the full guide for agents
npx -y wecut probe media/talk.mp4             # duration, size, fps, audio
npx -y wecut validate project.json            # errors with JSON pointers and fix hints
npx -y wecut preview project.json --at 1,5,9  # small JPG frames to look at
npx -y wecut render project.json              # out/clip.mp4
npx -y wecut mcp                              # the same, as an MCP server on stdio
```

Every command prints one JSON object on stdout. Exit codes: `0` ok, `2` invalid input, `3` missing dependency, `4` render failed, `5` over a limit.

## Install in your agent

WeCut needs FFmpeg 6 or newer with libass (`npx -y wecut doctor` checks it and says what to install):

- Debian/Ubuntu: `sudo apt-get install -y ffmpeg`
- macOS: `brew install ffmpeg-full` (the slim `ffmpeg` formula has no libass; WeCut finds ffmpeg-full on its own)
- Windows: `winget install Gyan.FFmpeg`

Then connect it to the agent you use. The MCP server works in a fixed workspace folder (`--root`, default: the folder it was started in); every media path is relative to it.

### Claude Code

```sh
claude mcp add wecut -- npx -y wecut mcp
```

Or as a plugin, which also installs the WeCut skill:

```
/plugin marketplace add 13kailouis-stack/wecut
/plugin install wecut@wecut
```

### Any MCP client

Claude Desktop, Cursor, Windsurf, Cline, Roo Code, LM Studio and most other clients take this block in their MCP settings file. Give `--root` explicitly, because desktop apps start servers from an unpredictable folder.

```json
{
  "mcpServers": {
    "wecut": { "command": "npx", "args": ["-y", "wecut", "mcp", "--root", "/path/to/workspace"] }
  }
}
```

[![Add to Cursor](https://cursor.com/deeplink/mcp-install-dark.svg)](https://cursor.com/en/install-mcp?name=wecut&config=eyJjb21tYW5kIjoibnB4IiwiYXJncyI6WyIteSIsIndlY3V0IiwibWNwIl19)

### VS Code (Copilot agent mode)

```sh
code --add-mcp '{"name":"wecut","command":"npx","args":["-y","wecut","mcp"]}'
```

or in `.vscode/mcp.json`:

```json
{ "servers": { "wecut": { "type": "stdio", "command": "npx", "args": ["-y", "wecut", "mcp"] } } }
```

### OpenAI Codex CLI

```sh
codex mcp add wecut -- npx -y wecut mcp
```

or in `~/.codex/config.toml`:

```toml
[mcp_servers.wecut]
command = "npx"
args = ["-y", "wecut", "mcp"]
```

### Gemini CLI

```sh
gemini extensions install https://github.com/13kailouis-stack/wecut
```

### Agents with a terminal

WeCut targets any agent with permission to run local programs and access the user's media. Product names do not determine compatibility: a terminal agent can use the CLI without MCP.

Grok Bot, Manus, Muse, OpenBot, Dots and Claude Duet are examples of requested integration targets, not a list of tested integrations. For each product, verify that its execution environment can run the WeCut binary and FFmpeg. A chat interface alone does not provide that access.

```sh
npx -y wecut guide     # read this first: workflow, project format, templates
npx -y wecut doctor
```

Then follow the guide: `probe`, write `project.json`, `validate`, `preview`, `render`. In a fresh Linux sandbox install FFmpeg first with `sudo apt-get install -y ffmpeg`.

Clients that can launch a local MCP process can use `wecut mcp` over stdio; the client-specific examples above are not an allowlist. An HTTP-only MCP client needs a separate bridge: WeCut does not currently serve remote HTTP MCP. Browser-only agents need an execution environment before they can render. Node is needed for the npm launcher; a standalone binary does not need Node.

### Agent Skill

The skill in [`skills/wecut/SKILL.md`](skills/wecut/SKILL.md) teaches the workflow to agents that load skills (Claude Code, Claude apps, Codex, Cursor and others):

```sh
npx skills add 13kailouis-stack/wecut
```

### Other ways to get the binary

- **npm** (Node 16+): `npx -y wecut <command>`, or `npm i -g wecut`. npm installs only the binary for your platform (`@wefluence/wecut-<os>-<arch>`); there are no install scripts.
- **Binary**: download the archive for your OS from GitHub Releases and check it against `SHA256SUMS`.
- **Source** (Go 1.22+): `go build -o wecut ./cmd/wecut`

Prebuilt for Linux, macOS and Windows on x64 and arm64. Set `WECUT_BINARY` to use your own build through the npm launcher.

## What it does

- Cut, join and speed up clips (0.25x to 4x), with 9:16, 16:9, 1:1, 4:5 and other canvases
- Cover crop with a focus point and zoom, or contain on a colour or blurred background
- Photos with Ken Burns motion
- 36 transitions (FFmpeg `xfade`), clip fades
- Text with 17 styles (including news-style headline cards and labels), 13 bundled fonts, emphasised words in another colour, pop / type / slide / fade animations
- Captions from a transcript: phrase, karaoke (spoken word highlighted) or one word at a time. Transcript times can stay in source-file seconds; WeCut maps them through the cuts
- Image overlays (logos, stickers) with opacity, rotation and fades
- Music and sound effects with trim, loop, fades and automatic ducking under speech
- Colour filters and effects (flash, cinema bars, frame, dim, night, sepia)
- Analysis of an existing video: shots, pace, hook length, flashes, tempo and beat sync, pauses, platform outro, and a frame sheet the model can look at
- Templates ("recipes"): the shape of an edit, applied to new clips, words and music; save your own from any project
- MP4 (H.264 + AAC, faststart) or GIF output

## Analyse a video, reuse its format

```sh
wecut analyze media/viral.mp4                 # shots, pace, hook, flashes, tempo, pauses + a frame sheet
wecut recipe list                             # templates: built-in and the workspace's recipes/*.json
wecut recipe apply --input request.json       # recipe + new clips, words, music -> project
wecut recipe from-project my-edit.json --write recipes/my-style.json   # "save as template"
```

`analyze` measures what a model cannot see in a transcript: where the cuts are (a white flash or a strobe counts once), how long the opening shot holds, whether cuts land on the beat, and whether the file ends with a silent platform outro. Its frame sheet, one frame per shot with timestamps, is how the agent reads the headline card, text style and framing.

A **recipe** (`wecut.recipe/1`, schema in `schema/wecut.recipe.schema.json`) is an edit's shape without its content: sections fed by roles (a 5 s `hook` with a headline card, then the `body`), text slots filled at apply time (with `emphasis` for the words to colour), caption and music style, effects on cuts. A recipe can carry its own default track, like a CapCut template; the music passed at apply time replaces it and `"none"` drops it. The built-in recipes copy the structure of formats that are common on Indonesian short-form video today: news-style reposts with a headline card, quote hooks, phrase-captioned vlogs, punched-in podcasts, photo slideshows.

Copy the structure, not the content: other creators' footage, wording and music stay theirs.

## MCP tools and prompts

| Tool | Does |
|---|---|
| `wecut_doctor` | checks FFmpeg, encoders and filters |
| `wecut_probe` | duration, size, fps, audio of media files |
| `wecut_validate` | checks a project; errors carry a JSON pointer and a hint |
| `wecut_preview` | renders frames and returns them as images, so the model can look |
| `wecut_render` | renders MP4/GIF, with progress notifications |
| `wecut_list`, `wecut_schema`, `wecut_example` | names, the project format, working examples |
| `wecut_analyze` | measures a video and returns its frame sheet as an image |
| `wecut_recipe_list`, `wecut_recipe_show` | templates and their roles and text slots |
| `wecut_recipe_apply` | recipe + new clips, words and music -> project |
| `wecut_recipe_from_project` | saves a finished project as a template |

Prompts (slash commands in clients that show them): `clips-from-video`, `copy-viral-format`, `add-captions`, `save-template`.

Projects go in as an object (`project`) or a workspace file (`projectFile`). The server speaks both protocol eras: modern clients (2026-07-28, per-request `_meta`, `server/discover`) and clients that open with `initialize` (2024-11-05 to 2025-11-25). Renders run one at a time; `notifications/cancelled` stops one and removes its partial file.

## Project format

`wecut guide` (the same text as [`skills/wecut/SKILL.md`](skills/wecut/SKILL.md)) explains the workflow, the two clocks (source seconds vs output seconds), captions and troubleshooting. `wecut schema` prints the full JSON Schema and `wecut example <name>` prints a working project.

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

## FAQ

**What is WeCut?**
An open-source command-line video editor made for AI agents. The agent describes an edit as JSON, WeCut checks it, shows preview frames and renders it with FFmpeg on the same machine. It is made by [Wefluence](https://wefluence.id), an Indonesian UGC and clipping marketplace.

**Is it free?**
Yes. Apache-2.0, no account, no API key, no usage limits. It runs on your own computer or your agent's sandbox, so there is no server bill either.

**How do I let ChatGPT, Claude, Grok or another AI edit videos?**
Give the agent WeCut: as an MCP server (`npx -y wecut mcp`) in clients that support MCP, as the skill, or by letting an agent with a terminal run `npx -y wecut guide` and follow it. See [Install in your agent](#install-in-your-agent). Chat apps without a code sandbox cannot reach files on your computer; use a desktop or terminal agent for those.

**Can an AI agent make TikTok, Reels or YouTube Shorts from a long video?**
Yes. The agent reads a transcript, picks the moments, writes one project per clip with `captions.src` pointing at the source, previews one, and renders them all in 9:16 with captions.

**Can it copy the format of a viral video?**
It measures the video (`wecut analyze`: cuts, pace, hook length, flashes, tempo, pauses, outro) and returns a frame sheet the model looks at. The agent then picks or writes a recipe and applies it to new footage, words and music. It copies the structure, not the footage, wording or music.

**Does it transcribe speech?**
It reads word timings from OpenAI or Groq Whisper `verbose_json`, whisper.cpp JSON, SRT, VTT or a plain word list. If the local FFmpeg has the Whisper filter, `wecut doctor` reports `local_transcription: true`.

**Does it upload my video anywhere?**
No. WeCut makes no network requests and has no telemetry. Media paths are refused if they are URLs or outside the workspace.

**Does it need a GPU?**
No. It encodes with libx264 on the CPU by default and can use NVENC, Quick Sync or VideoToolbox with `--encoder`. A 43-second 1080x1920 clip with captions renders in about 28 seconds on a 2019 laptop.

**Which platforms?**
Linux, macOS and Windows on x64 and arm64, with FFmpeg 6+ and libass.

**Can I make my own templates?**
Yes. An agent can write and revise project JSON from your instructions, validate it, preview it and render it. You do not have to edit manually first. Then run `wecut recipe from-project my-edit.json --write recipes/my-style.json`, or author recipe JSON directly using `wecut recipe schema`. Workspace recipes appear in `wecut recipe list` beside the built-ins.

A recipe can retain a music path, but does not bundle the audio file. Extraction captures a reusable structure rather than a lossless copy of every project feature. Review the applied recipe before reusing it in bulk.

The Wefluence Creator app already has a visual editor and a "Jadikan template" action. Its mobile template format is separate from this engine's recipes; visual import/export of engine recipes is not implemented yet. See [template authoring status](docs/template-authoring.md) for the existing flows and the proposed bridge.

## Compared with other ways to edit video from an agent

| | WeCut | FFmpeg by hand | Remotion / MoviePy | Cloud video APIs | CapCut |
|---|---|---|---|---|---|
| Made for agents (JSON in, JSON errors with fix hints) | yes | no, long filter graphs | code to write | JSON in | no, an app for people |
| Runs locally, media never leaves the machine | yes | yes | yes | no, upload | partly |
| Cost | free | free | free to paid licence | per render minute | free to subscription |
| Captions from a transcript, mapped through cuts | yes | by hand | by hand | some | auto captions in the app |
| Preview frames the model can look at | yes | by hand | by hand | some | no |
| Analyse a viral video, reuse its format | yes | no | no | no | templates, made by people |
| MCP server | yes | no | no | some | no |

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
internal/analyze   shots, pace, tempo, pauses from FFmpeg's measurements (pure)
internal/recipe    the template format: validate, apply to new material, derive from a project
internal/ff        finding, probing and running FFmpeg
internal/cli       the commands; every result is one JSON object
internal/mcp       MCP stdio server, a thin layer over internal/cli
internal/workspace keeps every path inside the workspace
internal/assets    fonts embedded in the binary
schema/            JSON Schema (kept in sync with the Go types by a test)
examples/          example projects (`wecut example`)
recipes/           built-in recipes
skills/wecut/      Agent Skill (`wecut guide` prints it)
npm/wecut/         the npm launcher (`npx wecut`)
tools/release/     builds all platforms, npm packages and release archives into dist/
```

Agent and registry manifests: `server.json` (MCP Registry), `.claude-plugin/` (Claude Code plugin and marketplace), `gemini-extension.json` and `GEMINI.md` (Gemini CLI), `glama.json`, `llms.txt`, `AGENTS.md` (for agents changing this code).

Release build: `go run ./tools/release -version 0.1.0` (nothing is published; see `.github/workflows/release.yml`).

Tests: `go test ./...` (the CLI tests render real video and are skipped without FFmpeg). Golden FFmpeg plans live in `internal/engine/testdata`; after an intended change run `go test ./internal/engine -update` and render the examples to check them.

## Roadmap

- `uvx wecut` (PyPI wheels) and an MCPB bundle for one-click install in Claude Desktop
- Local transcription through FFmpeg's Whisper filter, loudness normalisation
- Video overlays (picture-in-picture), stickers, more effects
- Optional Wefluence bridge for paid clipping campaigns (separate package)

## License

Code: Apache License 2.0 (see `LICENSE`). Bundled fonts: SIL Open Font License 1.1 or Apache 2.0, see `internal/assets/fonts/LICENSES.md`.
