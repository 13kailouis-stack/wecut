# AGENTS.md

Notes for coding agents working on this repository.

**Here to edit a video rather than change WeCut?** Read [`skills/wecut/SKILL.md`](skills/wecut/SKILL.md) (or run `npx -y wecut guide`) and use the `wecut` CLI or `wecut mcp`. Nothing below is needed for that.

## What this is

WeCut is a video editing engine for AI agents, written in Go with no third-party Go modules. A JSON project is validated, compiled into one FFmpeg filter graph plus an `.ass` subtitle file, and rendered with the FFmpeg already installed on the machine. The same commands are served over MCP (`wecut mcp`). It never makes network requests.

## Build and test

```sh
go build -o wecut ./cmd/wecut      # Go 1.22+
go vet ./...
go test ./...                      # CLI and MCP render tests need FFmpeg 6+ with libass on PATH, else they skip
go test ./internal/engine -update  # after an intended change to the FFmpeg plan; then render the examples and look at them
go run ./tools/release -version 0.1.0   # all platforms + npm packages into dist/ (publishes nothing)
```

On macOS use `brew install ffmpeg-full`; the plain `ffmpeg` formula has no libass.

## Layout

| Path | Role |
|---|---|
| `cmd/wecut` | entry point; `mcp` goes to `internal/mcp`, everything else to `internal/cli` |
| `internal/spec` | project types, strict decoding with "did you mean" hints, validation, the catalog of styles, fonts, transitions |
| `internal/engine` | timeline, captions, `.ass` builder, FFmpeg graph compiler. Pure: no I/O |
| `internal/analyze` | shots, pace, tempo, pauses from FFmpeg measurements. Pure |
| `internal/clone` | video to template: cuts, transitions, framing, zoom, text cards, captions, logo from grey frames and full-size stills. Pure, no I/O (runs as WebAssembly too). Tune it with `wecut clone --debug` |
| `internal/recipe` | template format `wecut.recipe/1`: validate, apply, derive from a project |
| `internal/ff` | finding, probing and running FFmpeg |
| `internal/cli` | the commands; each prints one JSON object (`guide` and `schema` print a document). `review.go` is the clone review, `setup.go` connects agent apps |
| `internal/mcp` | MCP stdio server; tools are thin wrappers over `internal/cli` |
| `internal/workspace` | keeps every path inside the workspace |
| `schema/`, `examples/`, `recipes/`, `skills/wecut/SKILL.md` | embedded into the binary through `wecut.go` |
| `npm/wecut` | the `npx wecut` launcher; platform binaries are `@wefluence/wecut-<os>-<arch>` |
| `studio` | static browser template editor; no build or backend. `node --test studio/model.test.mjs`; build the engine and run `node studio/check-engine.mjs <binary>` for interoperability |

## Rules

- **Untrusted input.** Projects are written by models that may have read hostile content. Text never goes into the FFmpeg filter graph, only into the escaped `.ass` file. FFmpeg runs from an argument list, never a shell. Every path goes through `internal/workspace`. Inputs are opened with `-protocol_whitelist file` and a demuxer whitelist. Do not weaken any of these.
- **One JSON object on stdout** for every CLI command except `guide` and `schema`, errors included, with a JSON pointer and a `hint` an agent can act on. Exit codes 0, 2, 3, 4, 5 as documented in `internal/cli/cli.go`.
- **Keep CLI and MCP the same.** A new feature is a CLI command first; the MCP tool calls it. The exception is `wecut setup`, the one command that writes outside the workspace: it changes nothing without `--apply`, backs up and reads back every file it edits, and is never offered over MCP.
- **WeCut holds no model.** The agent that runs it is the eyes: `wecut clone` returns a review (measurements, where they came from, limits, images) and takes the answers through `--read`. OCR is only the fallback for running without an agent.
- **Schema follows the Go types.** Change `schema/*.json` together with `internal/spec` or `internal/recipe`; a test fails when they drift.
- **Versions together.** `wecut.go` `Version`, `server.json`, `.claude-plugin/plugin.json` and `gemini-extension.json` carry the same version; a test checks it.
- **No new dependencies** in the engine, and no network calls, telemetry or API keys.
- **Fonts** are embedded under `internal/assets/fonts` with their licences; only OFL or Apache fonts.
- **Reference videos** that users share for analysis belong to other creators and never go into the repository. Recipes copy structure, not footage, wording or music.
- Go files are gofmt'ed and use LF line endings.
