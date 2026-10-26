import test from 'node:test';
import assert from 'node:assert/strict';
import { initialState, totalDuration, toRecipe } from './model.mjs';
import { splitClip, moveTimed, trimItem, createHistory } from './editing.mjs';

const fixture = () => ({ ...initialState(), clips: [{ kind:'video', from:2, to:8, sourceDuration:10, fit:'cover', mute:false, file:{name:'test.mp4'} }] });
test('split preserves total timing, source offsets and template sections', () => {
  const s = fixture(); assert.equal(splitClip(s, 0, 2), true);
  assert.equal(totalDuration(s), 6); assert.deepEqual(s.clips.map(c => [c.from,c.to]), [[2,4],[4,8]]);
  assert.deepEqual(toRecipe(s).sections.map(c => c.duration), [2,4]);
  assert.equal(splitClip(s, 0, .1), false); assert.equal(s.clips.length, 2);
});
test('trim stays within source and keeps a playable minimum', () => {
  const s = fixture(), c = s.clips[0]; trimItem(c, 'clip', 'left', 100, 6); assert.equal(c.from, 7.5);
  trimItem(c, 'clip', 'right', 100, 6); assert.equal(c.to, 10);
  const audio = { at:2, from:1, duration:4, sourceDuration:10 };
  trimItem(audio, 'music', 'left', -8, 8); assert.deepEqual(audio, {at:1,from:0,duration:5,sourceDuration:10});
  trimItem(audio, 'music', 'right', 50, 8); assert.equal(audio.duration, 7);
});
test('text and audio dragging preserves duration and bounds', () => {
  const t = {start:1,end:3}; moveTimed(t, 20, 6); assert.deepEqual(t,{start:4,end:6});
  const a = {at:2,duration:3}; moveTimed(a,-2,6); assert.equal(a.at,0);
});
test('undo retains original File identity and redo branches safely', () => {
  const s = fixture(), file = s.clips[0].file, h = createHistory(s);
  splitClip(s,0,2); h.record(s); const restored=h.undo(); assert.equal(restored.clips.length,1); assert.equal(restored.clips[0].file,file);
  assert.equal(h.redo().clips.length,2); const original=h.undo(); original.name='Another'; h.record(original); assert.equal(h.canRedo,false);
  original.clips[0].thumbnail='data:image/jpeg;base64,new'; h.record(original); assert.equal(h.undo().name,'Template saya');
});
