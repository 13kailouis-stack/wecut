# Agent reliability improvements

The production replay exposed three separate failures: the caller confused the
style reference with destination footage, mixed cuts/transitions failed to render,
and unreviewed image measurements produced the wrong caption layout. Successful
JSON validation was not evidence of a successful or visually faithful edit.

## Changes

- Restore constant frame rate after timestamp edits and after concatenation.
  FFmpeg 7.1.5 clears frame-rate metadata after `setpts`; `xfade` then rejects
  `1/0`, even with matching time bases. Keep `settb=AVTB` after restoring fps.
- Preserve filter configuration diagnostics before encoder shutdown messages.
  Previously the last six stderr lines hid the cause and suggested an audio issue.
- Bound render decoder, filter graph and encoder threads to two. Each clip opens
  its own decoder, so automatic per-input thread counts multiply memory usage.
  This is not a hard memory ceiling; long, high-resolution projects still cost more.
- Clone answers support horizontal anchors, alignment, width, line spacing and
  text start/end corrections, in addition to existing vertical and font controls.
  Invalid values are rejected. The review reports the current layout numerically.
- Clone output identifies its stand-in preview and `needs_visual_review` or
  `comparison_failed` state. Timing deviations beyond 200 ms (or two frames,
  whichever is greater) and missing matched events produce `W_TIMING`.
- Recipe application reports style-reference and actual-footage identities.
  `W_REFERENCE_REUSED` flags the same reference file, including path aliases,
  used as destination footage. Reconstruction remains supported. This is file
  identity checking, not content hashing: a separately copied file can evade it.
- The bundled guide separates the reference and destination throughout review,
  application and rendering. It describes audio carryover and placeholder previews.

## Validation

`go test ./...`, `go vet ./...`, 13 Studio model/editing tests, and focused clone,
layout, source-identity, diagnostics and mixed-transition regressions passed.
Engine golden plans were updated for the intended fps and thread changes.

On the production machine's FFmpeg 7.1.5, the old binary reproduced `E_RENDER`
on the real ten-shot project. The candidate exposed the hidden `1/0` cause;
the final timing fix rendered its preview successfully. The mixed-transition
integration test also passed there. Cached clone analysis plus visual answers
produced a comparison sheet again in 34.85 seconds. All nine measured events
matched, but the worst transition difference was 400 ms: this is approximate
style extraction, not pixel-exact or frame-exact cloning.

A separate adaptation smoke test used destination campaign footage rather than
the reference: `reference_reused=false`. The 41.6-second 360x640/30fps draft,
including audio, rendered in 18.15 seconds (18.65 seconds wall time); child peak
RSS was 390,456 KiB. That test intentionally used short footage and received
repeat-footage warnings. It is a renderer regression artifact, not an approved
campaign deliverable or evidence of improved autonomous model judgment.

The patched binary was installed for the affected WorkBot agent with an original
binary backup and SHA-256 readback. Its existing WeCut skill gained the new guide
section through WorkBot's revision-preserving skill update. No model settings,
chat history, active tasks or WorkBot service code were changed. The npm release
has not been published; the development build retains the existing version.

Linux development binary SHA-256:
`9c1c0d9ada748cf68aa2fa3917ece5c5eb3f9abaa63762806a94d287f400f61c`.

Private replay media, chat excerpts, backups and render artifacts are kept outside
tracked source. Generic regression fixtures contain no customer footage or text.
