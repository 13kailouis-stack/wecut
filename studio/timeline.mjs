import { duration, totalDuration } from './model.mjs';
import { moveTimed, trimItem } from './editing.mjs';

export function timelineEditor({ state, select, seek, changed, pause, getSelection }) {
  const root = document.getElementById('timeline');
  let scale = 70;
  const el = (tag, text, cls) => { const n = document.createElement(tag); n.className = cls || ''; if (text) n.textContent = text; return n; };
  function render() {
    root.replaceChildren();
    const total = totalDuration(state), width = Math.max(root.parentElement.clientWidth - 108, Math.max(total + 2, 10) * scale);
    const ruler = el('div', '', 'ruler'); ruler.style.width = `${width}px`;
    for (let t = 0; t <= width / scale; t += scale < 35 ? 5 : scale < 80 ? 2 : 1) { const tick = el('span', `${Math.floor(t / 60).toString().padStart(2, '0')}:${(t % 60).toString().padStart(2, '0')}`, 'tick'); tick.style.left = `${t * scale}px`; ruler.append(tick); }
    ruler.addEventListener('pointerdown', e => scrub(e, ruler)); root.append(ruler);
    const selection = getSelection();
    function row(kind, label, items) {
      const line = el('div', '', `track ${kind}-track`); line.style.width = `${width}px`;
      const name = el('span', label, 'track-label'); line.append(name);
      line.addEventListener('pointerdown', e => { if (e.target === line) scrub(e, line); });
      if (!items.length) line.append(el('span', kind === 'clip' ? 'Impor media untuk mulai mengedit' : kind === 'text' ? 'Tambahkan teks' : 'Tambahkan musik', 'track-empty'));
      items.forEach(({ item, start, length, index }) => {
        const block = el('div', '', `track-block ${kind}-block${selection.kind === kind && selection.index === index ? ' selected' : ''}`);
        block.tabIndex = 0; block.setAttribute('role', 'button'); block.setAttribute('aria-label', `${label} ${index + 1}: ${kind === 'text' ? item.text : item.file?.name || ''}`);
        block.style.left = `${start * scale}px`; block.style.width = `${Math.max(8, length * scale)}px`;
        if (kind === 'clip' && (item.thumbnail || item.kind === 'image')) { const film = el('div', '', 'filmstrip'); film.style.backgroundImage = `url("${item.thumbnail || item.url}")`; block.append(film); }
        block.append(el('span', kind === 'text' ? item.text : item.file?.name, 'block-title'));
        if (kind === 'music' && item.peaks) {
          const svg = document.createElementNS('http://www.w3.org/2000/svg','svg'); svg.setAttribute('viewBox','0 0 320 20'); svg.setAttribute('preserveAspectRatio','none'); svg.classList.add('waveform');
          const path = document.createElementNS('http://www.w3.org/2000/svg','path');
          path.setAttribute('d', Array.from({length:160}, (_, i) => { const source = (item.from + i / 160 * item.duration) / item.sourceDuration; const amp = (item.peaks[Math.min(159, Math.floor(source * 160))] || 0) * 9; return `M${i*2} ${10-amp}v${amp*2}`; }).join(' '));
          path.setAttribute('stroke','#9cc8c0'); path.setAttribute('stroke-width','1'); svg.append(path); block.append(svg);
        }
        if (kind === 'clip') block.append(el('small', `${length.toFixed(1)}s${item.mute ? ' · mute' : ''}`, 'block-duration'));
        const focus = () => select(kind, index);
        block.addEventListener('keydown', e => { if (e.key === 'Enter') focus(); });
        block.addEventListener('pointerdown', e => {
          if (e.target.classList.contains('trim-handle')) return;
          e.preventDefault(); e.stopPropagation(); pause();
          const original = { ...item }, x = e.clientX; let delta = 0;
          block.setPointerCapture(e.pointerId);
          const move = event => { delta = (event.clientX - x) / scale; if (kind !== 'clip') { Object.assign(item, original); moveTimed(item, start + delta, total); block.style.left = `${(kind === 'text' ? item.start : item.at) * scale}px`; } else block.style.transform = `translateX(${delta * scale}px)`; };
          const up = () => {
            block.removeEventListener('pointermove', move); block.removeEventListener('pointerup', up); block.removeEventListener('pointercancel', cancel);
            let next = index;
            if (kind === 'clip' && Math.abs(delta * scale) > 8) {
              const targetTime = Math.max(0, start + delta + length / 2); let cursor = 0;
              next = state.clips.length - 1;
              for (let i = 0; i < state.clips.length; i++) { if (targetTime < cursor + duration(state.clips[i])) { next = i; break; } cursor += duration(state.clips[i]); }
              const [clip] = state.clips.splice(index, 1); state.clips.splice(next, 0, clip);
            }
            focusSelection(kind, next); changed();
          };
          const cancel = () => { Object.assign(item, original); block.removeEventListener('pointermove', move); block.removeEventListener('pointerup', up); changed(); };
          block.addEventListener('pointermove', move); block.addEventListener('pointerup', up); block.addEventListener('pointercancel', cancel, { once: true });
        });
        for (const edge of ['left', 'right']) {
          const handle = el('span', '', `trim-handle ${edge}`); handle.title = edge === 'left' ? 'Potong awal' : 'Potong akhir';
          handle.addEventListener('pointerdown', e => {
            e.preventDefault(); e.stopPropagation(); pause(); handle.setPointerCapture(e.pointerId);
            const original = { ...item }, x = e.clientX;
            const move = event => {
              Object.assign(item, original); trimItem(item, kind, edge, (event.clientX - x) / scale, total);
              const length = kind === 'clip' ? duration(item) : kind === 'text' ? item.end - item.start : item.duration;
              block.style.width = `${length * scale}px`;
              if (kind !== 'clip') block.style.left = `${(kind === 'text' ? item.start : item.at) * scale}px`;
            };
            const finish = event => { if (event.type === 'pointercancel') Object.assign(item, original); handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', finish); handle.removeEventListener('pointercancel', finish); focus(); changed(); };
            handle.addEventListener('pointermove', move); handle.addEventListener('pointerup', finish); handle.addEventListener('pointercancel', finish);
          }); block.append(handle);
        }
        line.append(block);
      }); root.append(line);
    }
    const focusSelection = (kind, index) => select(kind, index);
    state.texts.forEach((item, index) => row('text', `T${index + 1}`, [{ item, index, start: item.start, length: item.end - item.start }]));
    if (!state.texts.length) row('text', 'Teks', []);
    let offset = 0;
    row('clip', 'Video', state.clips.map((item, index) => { const start = offset; offset += duration(item); return { item, index, start, length: duration(item) }; }));
    row('music', 'Audio', state.music ? [{ item: state.music, index: 0, start: state.music.at, length: state.music.duration }] : []);
    const playhead = el('div', '', 'playhead'); playhead.id = 'playhead'; playhead.append(el('span', '', 'playhead-cap')); root.append(playhead);
    root.style.minWidth = `${width + 96}px`;
  }
  function scrub(e, area) {
    pause(); area.setPointerCapture(e.pointerId);
    const update = event => seek(Math.max(0, Math.min(totalDuration(state), (event.clientX - area.getBoundingClientRect().left) / scale)));
    const end = () => { area.removeEventListener('pointermove', update); area.removeEventListener('pointerup', end); area.removeEventListener('pointercancel', end); };
    update(e); area.addEventListener('pointermove', update); area.addEventListener('pointerup', end); area.addEventListener('pointercancel', end);
  }
  return { render, position(time) { const p = document.getElementById('playhead'); if (p) p.style.left = `${96 + time * scale}px`; }, zoom(value) { scale = value; render(); } };
}
