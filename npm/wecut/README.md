# wecut

Free, open-source video editing engine for AI agents. An agent writes a small JSON project; `wecut` renders it with the FFmpeg on this machine: cuts, 9:16 reframing, captions (phrase, karaoke, word by word), text, logos, music with ducking, transitions. It can also analyse a viral video and reuse its format as a template on new footage. No cloud, no API key, no GPU, no telemetry.

Needs FFmpeg 6+ with libass (`npx -y wecut doctor` checks it).

```sh
npx -y wecut guide        # the full guide for agents: workflow, format, templates
npx -y wecut doctor
npx -y wecut probe media/talk.mp4
npx -y wecut example podcast-clip > project.json
npx -y wecut validate project.json
npx -y wecut preview project.json --at 1,5,9
npx -y wecut render project.json
npx -y wecut analyze media/viral.mp4
npx -y wecut recipe list
```

## MCP server

```sh
claude mcp add wecut -- npx -y wecut mcp       # Claude Code
codex mcp add wecut -- npx -y wecut mcp        # OpenAI Codex CLI
gemini extensions install https://github.com/13kailouis-stack/wecut   # Gemini CLI
```

Claude Desktop, Cursor, Windsurf, Cline and other clients:

```json
{
  "mcpServers": {
    "wecut": { "command": "npx", "args": ["-y", "wecut", "mcp", "--root", "/path/to/workspace"] }
  }
}
```

Tools: `wecut_doctor`, `wecut_probe`, `wecut_validate`, `wecut_preview` (returns frames as images), `wecut_render` (with progress), `wecut_list`, `wecut_schema`, `wecut_example`, `wecut_analyze` (measures a video, returns its frame sheet), `wecut_recipe_list`, `wecut_recipe_show`, `wecut_recipe_apply`, `wecut_recipe_from_project` (templates). Prompts: `clips-from-video`, `copy-viral-format`, `add-captions`, `save-template`. Every path stays inside the workspace given with `--root`.

Any agent permitted to run WeCut and FFmpeg and read local media can use the terminal workflow without MCP: `npx -y wecut guide`. Grok Bot, Manus, Muse, OpenBot, Dots and Claude Duet are requested integration targets, not verified product integrations. Compatibility depends on execution permissions and dependencies. MCP clients must support launching a stdio process; WeCut does not currently expose an HTTP MCP endpoint.

Full documentation, install for every agent, the project format and the Agent Skill: https://github.com/13kailouis-stack/wecut

License: Apache-2.0. Made by [Wefluence](https://wefluence.id).
