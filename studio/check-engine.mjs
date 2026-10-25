import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { initialState, toBundle } from './model.mjs';

if (!process.argv[2]) throw new Error('Pass the path to a built WeCut binary.');
const binary = path.resolve(process.argv[2]);
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'wecut-studio-'));
try {
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', 'color=c=blue:s=64x64:r=25:d=4', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', path.join(root, 'replacement.mp4')]);
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', 'sine=f=440:d=6', path.join(root, 'song.wav')]);
  const music = fs.readFileSync(path.join(root, 'song.wav'));
  const state = initialState();
  state.clips.push({ kind: 'video', from: 2, to: 6, sourceDuration: 10, fit: 'cover', mute: true });
  state.texts.push({ text: 'Original', font: 'poppins', size: 64, start: 0, end: 3, x: .5, y: .25, color: '#FFFFFF', background: '#171717', box: true });
  state.music = { file: { name: 'song.wav', size: music.length, arrayBuffer: async () => music }, sourceDuration: 6, from: 2, at: 1, duration: 3, volume: 40 };
  const bundle = await toBundle(state, bytes => Buffer.from(bytes).toString('base64'));
  fs.writeFileSync(path.join(root, 'received.wecut.json'), JSON.stringify(bundle));
  const run = (...args) => JSON.parse(execFileSync(binary, [...args, '--root', root], { encoding: 'utf8' }));
  assert.equal(run('recipe', 'unpack', 'received.wecut.json', '--write', 'template').ok, true);
  fs.writeFileSync(path.join(root, 'request.json'), JSON.stringify({ recipe: 'template/recipe.json', clips: [{ src: 'replacement.mp4', role: 'clip1' }], texts: { text1: { text: 'Replacement' } }, write: 'edit.json' }));
  const result = run('recipe', 'apply', '--input', 'request.json');
  assert.equal(result.ok, true);
  assert.equal(result.project.texts[0].text, 'Replacement');
  assert.equal(result.project.clips[0].src, 'replacement.mp4');
  assert.equal(result.project.audio[0].from, 2);
  assert.equal(result.project.audio[0].at, 1);
  assert.equal(result.project.audio[0].duration, 3);
  assert.deepEqual(fs.readFileSync(path.join(root, 'template/assets/music.wav')), music);
  console.log('Studio bundle → engine import → replacement project: passed');
} finally {
  fs.rmSync(root, { recursive: true, force: true });
}
