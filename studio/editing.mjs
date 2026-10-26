import { duration } from './model.mjs';

export function splitClip(state, index, local) {
  const c = state.clips[index];
  if (!c || local < .5 || duration(c) - local < .5 || state.clips.length >= 100) return false;
  const first = { ...c }, second = { ...c };
  if (c.kind === 'image') { first.duration = local; second.duration -= local; }
  else { first.to = c.from + local; second.from = first.to; }
  state.clips.splice(index, 1, first, second);
  return true;
}

export function moveTimed(item, start, total) {
  const isText = 'start' in item;
  const length = isText ? item.end - item.start : item.duration;
  const value = Math.max(0, Math.min(total - length, start));
  if (isText) { item.start = value; item.end = value + length; } else item.at = value;
}

export function trimItem(item, kind, edge, delta, total) {
  if (kind === 'clip') {
    if (item.kind === 'image') item.duration = Math.max(.5, Math.min(60, item.duration + (edge === 'left' ? -delta : delta)));
    else if (edge === 'left') item.from = Math.max(0, Math.min(item.to - .5, item.from + delta));
    else item.to = Math.min(item.sourceDuration, Math.max(item.from + .5, item.to + delta));
  } else if (kind === 'text') {
    if (edge === 'left') item.start = Math.max(0, Math.min(item.end - .1, item.start + delta));
    else item.end = Math.min(total, Math.max(item.start + .1, item.end + delta));
  } else if (edge === 'left') {
    const d = Math.max(-item.at, -item.from, Math.min(item.duration - .1, delta));
    item.at += d; item.from += d; item.duration -= d;
  } else item.duration = Math.max(.1, Math.min(item.sourceDuration - item.from, total - item.at, item.duration + delta));
}

const clone = s => ({ ...s, clips: s.clips.map(c => ({ ...c })), texts: s.texts.map(t => ({ ...t })), music: s.music ? { ...s.music } : null });
export function createHistory(initial) {
  let states = [clone(initial)], index = 0;
  const key = s => JSON.stringify(s, (name, value) => ['thumbnail', 'peaks'].includes(name) ? undefined : value);
  return {
    record(s) { if (key(s) === key(states[index])) return; states = states.slice(0, index + 1); states.push(clone(s)); if (states.length > 50) states.shift(); index = states.length - 1; },
    undo() { return index > 0 ? clone(states[--index]) : null; },
    redo() { return index < states.length - 1 ? clone(states[++index]) : null; },
    get canUndo() { return index > 0; }, get canRedo() { return index < states.length - 1; },
  };
}
