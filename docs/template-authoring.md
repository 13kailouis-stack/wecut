# Template authoring: current status and next steps

The intended product has two authoring interfaces and one shared engine recipe format: humans edit visually; agents edit structured data from natural-language instructions. The bridge between the existing visual editor and engine recipes is still to be built.

## Available now

- Agent: write project JSON, validate, preview, revise, render, then use `recipe from-project` to save a reusable recipe. Alternatively write recipe JSON directly. There is no requirement to start with a manually edited video.
- Agent reuse: `recipe apply` replaces footage and text slots, then the normal preview/render workflow produces a clip. A recipe can keep a default music source, accept replacement music or disable music.
- Human: the separate Wefluence Creator app has a visual editor and "Jadikan template". Its `templateFromState` stores relative timings for text, stickers, filters and effects, plus canvas settings. That mobile template export currently does not include music or the clip sequence.

Mobile templates and `wecut.recipe/1` are different formats. Neither the CLI nor the mobile editor currently provides automatic interchange between them. The mobile export pipeline also uses Cloudinary; the engine's local rendering guarantee does not describe that pipeline.

## Recommended implementation order (not shipped)

1. Add an explicit adapter from the visual editor's state to engine project JSON. Report unsupported features instead of silently dropping them. Test representative renders before promising visual parity.
2. Add template controls for replaceable video slots, text defaults, duration rules and default/replacement/disabled music. Export engine recipes with validation and a clear list of required media files.
3. Import recipes into the visual editor for the supported subset. Preserve unsupported data or reject editing with a clear explanation rather than corrupting a template.
4. Connect a local rendering companion for humans who want the same FFmpeg output without cloud rendering. Bind it to localhost and authorize access from the editor; a hosted web page alone cannot launch local FFmpeg.

## Extraction and sharing limits

`recipe from-project` derives reusable sections and pacing; it is not a lossless project archive. It currently takes the first audio track as default music and does not serialize all project features such as overlays or an arbitrary multitrack mix. Always apply the result to sample footage and inspect a preview.

Music paths refer to files in the rendering workspace. Sharing the JSON alone does not deliver the music. A future template bundle must include or resolve its required assets and validate their paths. Analysis can measure pacing and expose frames for the agent to interpret; it cannot guarantee that a remix will go viral.
