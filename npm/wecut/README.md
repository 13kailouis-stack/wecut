# wecut

Video editing engine for AI agents. An agent writes a small JSON project; `wecut` renders it with the FFmpeg on this machine. No cloud, no API key, no GPU.

Needs FFmpeg 6+ with libass (`npx wecut doctor` checks it).

```sh
npx -y wecut doctor
npx -y wecut probe media/talk.mp4
npx -y wecut example podcast-clip > project.json
npx -y wecut validate project.json
npx -y wecut preview project.json --at 1,5,9
npx -y wecut render project.json
```

## MCP server

```sh
# Claude Code
claude mcp add wecut -- npx -y wecut mcp

# any MCP client (stdio)
npx -y wecut mcp --root /path/to/workspace
```

Claude Desktop / Cursor style config:

```json
{
  "mcpServers": {
    "wecut": { "command": "npx", "args": ["-y", "wecut", "mcp", "--root", "/path/to/workspace"] }
  }
}
```

Tools: `wecut_doctor`, `wecut_probe`, `wecut_validate`, `wecut_preview` (returns frames as images), `wecut_render` (with progress), `wecut_list`, `wecut_schema`, `wecut_example`. Every path stays inside the workspace given with `--root`.

Full documentation, the project format and the Agent Skill: https://github.com/13kailouis-stack/wecut

License: Apache-2.0.
