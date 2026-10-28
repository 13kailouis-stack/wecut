// Runs the browser clone engine (engine/clone.wasm) under Node on a made-up
// video: a cut at 2 s, then a flash into a third shot shown inside bars.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const wasm = join(here, 'engine', 'clone.wasm');

test('clone engine finds cuts, a flash and framing', { skip: !existsSync(wasm) && 'run go run ./tools/wasm first' }, async () => {
  const require = createRequire(import.meta.url);
  globalThis.require = require;
  globalThis.fs ??= require('node:fs');
  require(join(here, 'engine', 'wasm_exec.js'));
  const go = new globalThis.Go();
  const { instance } = await WebAssembly.instantiate(readFileSync(wasm), go.importObject);
  go.run(instance);
  const e = globalThis.wecutClone;
  const sizes = JSON.parse(e.start(JSON.stringify({ W: 1080, H: 1920, Duration: 6, LowFPS: 15, HighFPS: 5 })));
  const { lowW: w, lowH: h } = sizes;
  const paint = (seed, i, bars, white) => {
    const f = new Uint8Array(w * h);
    for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
      let v = 128 + 90 * Math.sin((x * (seed + 2) + i) / 7) * Math.cos((y * (seed + 3)) / 9 + seed);
      if (bars && (y < h / 4 || y >= h * 3 / 4)) v = 6;
      f[y * w + x] = Math.max(0, Math.min(255, v * (1 - white) + 255 * white));
    }
    return f;
  };
  for (let i = 0; i < 90; i++) {
    let f;
    if (i < 30) f = paint(1, i, false, 0);
    else if (i < 60) f = paint(5, i, false, i >= 57 ? (i - 56) / 3 : 0);
    else f = paint(9, i, true, i < 63 ? 1 - (i - 59) / 4 : 0);
    e.addLow(f);
  }
  const plan = JSON.parse(e.analyze());
  assert.equal(plan.events.length, 2, JSON.stringify(plan.events));
  assert.equal(plan.events[0].kind, 'cut');
  assert.ok(Math.abs(plan.events[0].at - 2) < 0.08);
  assert.equal(plan.events[1].kind, 'flash');
  assert.equal(plan.shots[2].fit, 'contain');
  e.measure(16, 16, []);
  const res = JSON.parse(e.build(JSON.stringify({ Name: 'made-up', Source: 'made-up.mp4' })));
  assert.equal(res.recipe.recipe, '1');
  assert.equal(res.slots.length, 3);
  assert.equal(res.recipe.output.background, '#000000');
});
