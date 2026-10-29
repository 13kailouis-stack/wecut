import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import { initialState, totalDuration, locate, validate, toRecipe, toBundle, MAX_AUDIO_BYTES } from './model.mjs';

function example() {
  const s = initialState();
  s.clips.push({ kind: 'video', file: { name: 'private-video.mp4' }, url: 'blob:private-video', from: 4, to: 8, sourceDuration: 20, fit: 'cover', mute: true });
  s.texts.push({ text: 'Hello <agent>', font: 'poppins', size: 64, start: 0, end: 3, x: .5, y: .25, color: '#FFFFFF', background: '#171717', box: true });
  return s;
}

test('template exports structure, not original footage or browser URLs', () => {
  const state = example();
  state.filter = 'warm';
  const recipe = toRecipe(state);
  assert.equal(recipe.sections[0].duration, 4);
  assert.equal(recipe.sections[0].role, 'clip1');
  assert.equal(recipe.sections[0].mute, true);
  assert.equal(recipe.texts[0].text, 'Hello <agent>');
  assert.equal(recipe.filters[0].name, 'warm');
  assert.ok(!JSON.stringify(recipe).includes('private-video'));
  assert.ok(!JSON.stringify(recipe).includes('blob:'));
});

test('music is self-contained with trim and timeline position', async () => {
  const s = example();
  s.music = { file: { name: 'song.MP3', size: 3, arrayBuffer: async () => Buffer.from('abc') }, sourceDuration: 20, from: 2, at: 1, duration: 3, volume: 40 };
  const b = await toBundle(s, (bytes) => Buffer.from(bytes).toString('base64'));
  assert.equal(b.bundle, 'wecut.bundle/1');
  assert.deepEqual(b.assets, [{ name: 'music.mp3', data: 'YWJj' }]);
  assert.equal(b.recipe.music.src, 'assets/music.mp3');
  assert.equal(b.recipe.music.from, 2);
  assert.equal(b.recipe.music.at, 1);
  assert.equal(b.recipe.music.duration, 3);
});

test('reject invalid cuts, text timings and oversized audio before reading', async () => {
  for (const mutate of [s => { s.clips[0].to = 40; }, s => { s.clips[0].to = s.clips[0].from; }, s => { s.texts[0].end = 9; }, s => { s.texts[0].x = NaN; }, s => { s.ratio = 'bad'; }, s => { s.music = { file: { name: 'song.mp3', size: MAX_AUDIO_BYTES + 1, arrayBuffer: () => assert.fail('must not read') } }; }]) {
    const s = example(); mutate(s); assert.ok(validate(s).length); await assert.rejects(() => toBundle(s, () => ''));
  }
});

test('locate uses source trim durations and image durations', () => {
  const s = example();
  s.clips.push({ kind: 'image', duration: 5, fit: 'contain' });
  assert.equal(totalDuration(s), 9);
  assert.equal(locate(s, 4).index, 1);
  assert.equal(locate(s, 6).local, 2);
  assert.equal(toRecipe(s).sections[1].shot, 5);
});

test('page connects to nothing but its own files and ships fonts locally', () => {
  const html = fs.readFileSync(new URL('./index.html', import.meta.url), 'utf8');
  // 'self' only, to load the clone engine (engine/clone.wasm); no other host.
  const connect = /connect-src ([^;]+);/.exec(html)[1].trim();
  assert.equal(connect, "'self'");
  assert.ok(!/https?:/.test(/Content-Security-Policy" content="([^"]+)"/.exec(html)[1]));
  const css = fs.readFileSync(new URL('./styles.css', import.meta.url), 'utf8');
  assert.ok(!/url\(https?:/.test(css));
  for (const f of ['roboto-700.ttf', 'poppins-700.ttf', 'montserrat-700.ttf']) assert.ok(fs.existsSync(new URL(`./fonts/${f}`, import.meta.url)));
});

test('a template opens as slots and exports with its own detail kept', async () => {
  const { stateFromTemplate, templateRecipe } = await import('./model.mjs');
  const recipe = {
    recipe: '1', name: 'cloned', output: { ratio: '9:16', res: 1080, fps: 30 }, maxDuration: 6.5,
    sections: [
      { role: 'hook', fit: 'cover', audio: 'next', transitionOut: { type: 'flash', duration: 0.4 }, shots: [{ dur: 2, zoomTo: 1.1 }, { dur: 2.4 }] },
      { role: 'body', fit: 'contain', shots: [{ dur: 2.5, about: 'landscape talk' }] },
    ],
    texts: [{ slot: 'headline', text: 'Old words', end: 4, x: 0.08, y: 0.7, align: 'left', font: 'anton', size: 70, color: '#111111', background: '#FFFFFF', box: 'card', lineHeight: 1.1, padding: [20, 12] }],
    captions: { mode: 'pop', y: 0.63 }, overlays: [{ slot: 'logo', x: 0.2, y: 0.08, width: 0.2 }],
  };
  const s = stateFromTemplate(recipe, (i, x) => ({ file: null, kind: 'image', placeholder: true, url: 'data:,', duration: x.shot.dur, from: 0, to: 0, sourceDuration: 600 }));
  assert.equal(s.clips.length, 3);
  assert.equal(s.clips[0].mute, true); // hook plays the talk's sound
  assert.equal(s.clips[2].fit, 'contain');
  assert.equal(s.texts[0].font, 'anton');
  assert.deepEqual(validate(s), []);
  // The flash overlap (0.4 s) is off the timeline: 2 + 2.0 + 2.5 = 6.5 s, as long as the template.
  assert.equal(s.clips[1].duration, 2);
  assert.ok(Math.abs(totalDuration(s) - 6.5) < 1e-9);
  s.texts[0].text = 'New words'; s.clips[1].duration = 2.2;
  const r = templateRecipe(s);
  assert.equal(r.sections[0].audio, 'next');
  assert.equal(r.sections[0].shots[0].zoomTo, 1.1);
  assert.equal(r.sections[0].shots[1].dur, 2.6);
  assert.equal(r.sections[0].transitionOut.type, 'flash');
  assert.equal(r.texts[0].text, 'New words');
  assert.equal(r.texts[0].box, 'card');
  assert.equal(r.texts[0].lineHeight, 1.1);
  assert.equal(r.captions.mode, 'pop');
  assert.equal(r.overlays[0].slot, 'logo');
});
