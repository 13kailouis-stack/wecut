# WeCut

The `wecut` MCP server edits and renders video on this machine with FFmpeg. Nothing is uploaded and no API key is needed.

- Inspect media with `wecut_probe`; write a project (`wecut_example`, `wecut_schema`); check it with `wecut_validate`; look at frames with `wecut_preview`; render with `wecut_render`.
- Clip `from`/`to` are seconds of the source file; texts, overlays, audio and effects use seconds of the finished video.
- To copy a format that worked: `wecut_analyze` the video and look at its frame sheet, pick a recipe (`wecut_recipe_list`), then `wecut_recipe_apply` it to new clips, words and music. Save a finished edit as a template with `wecut_recipe_from_project`.
- Copy structure, not content: other creators' footage, wording and music stay theirs.

Full guide: `npx -y wecut guide`.
