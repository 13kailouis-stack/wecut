import { splitClip, createHistory } from './editing.mjs?v=desktop-3';
import { timelineEditor } from './timeline.mjs?v=desktop-3';
import { initialState, duration, totalDuration, locate, validate, toBundle, filters, fonts, engineFonts, stateFromTemplate, textFromSlot, MAX_AUDIO_BYTES, gradientCSS } from './model.mjs?v=sources-1';
import { cloneVideo } from './clone.mjs?v=clone-3';

const $ = (id) => document.getElementById(id);
const state = initialState();
let selectedClip = 0, selectedText = 0, tab = 'clip', time = 0, playing = false, anchor = 0, frame = 0, exporting = false;
let statusTimer, importBusy = false, restoring = false;
const history = createHistory(state);
const thumbnailCache = new Map();
const tracks = timelineEditor({state, select: selectTrack, seek: seekTo, pause, changed: () => { checkpoint(); render(); }, getSelection: () => ({kind: tab === 'clip' ? 'clip' : tab === 'text' ? 'text' : 'music', index: tab === 'text' ? selectedText : tab === 'clip' ? selectedClip : 0})});
const video = $('video'), photo = $('photo'), musicPlayer = $('musicPlayer');
const escape = (value) => String(value).replace(/[&<>"']/g, (v) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[v]));
const fmt = (n) => `${String(Math.floor(Math.max(0, n) / 60)).padStart(2, '0')}:${String(Math.floor(Math.max(0, n) % 60)).padStart(2, '0')}`;
const num = (v) => Number.isFinite(Number(v)) ? Number(v) : 0;
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
const node = (tag, text, className) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (className) n.className = className; return n; };

function status(message) {
  clearTimeout(statusTimer); $('status').textContent = message; $('status').hidden = false;
  statusTimer = setTimeout(() => { $('status').hidden = true; }, 5000);
}
function dialog(title, paragraphs, action) {
  const body = $('dialogContent'); body.replaceChildren(node('h2', title));
  paragraphs.forEach((p) => body.append(node('p', p)));
  if (action) { const b = node('button', action.label, 'primary'); b.type = 'button'; b.addEventListener('click', action.run); body.append(b); }
  if (!$('dialog').open) $('dialog').showModal();
}

function readMetadata(file, kind) {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const media = document.createElement(kind === 'image' ? 'img' : kind === 'audio' ? 'audio' : 'video');
    const event = kind === 'image' ? 'load' : 'loadedmetadata';
    let settled = false;
    const finish = (error) => {
      if (settled) return; settled = true; clearTimeout(timer);
      if (error) { URL.revokeObjectURL(url); media.removeAttribute('src'); reject(error); }
      else {
        const sourceDuration = kind === 'image' ? 5 : media.duration;
        if (!Number.isFinite(sourceDuration) || sourceDuration <= 0) { URL.revokeObjectURL(url); reject(new Error(`${file.name}: durasi tidak terbaca.`)); return; }
        resolve({ url, sourceDuration });
      }
    };
    const timer = setTimeout(() => finish(new Error(`${file.name}: tidak bisa dibaca dalam 20 detik.`)), 20000);
    media.addEventListener(event, () => finish(), { once: true });
    media.addEventListener('error', () => finish(new Error(`${file.name}: format tidak bisa diputar di browser ini. Coba MP4 H.264 atau JPG/PNG.`)), { once: true });
    media.preload = 'metadata'; media.src = url;
  });
}

async function addFiles(files) {
  if (importBusy || exporting) { status('Tunggu proses yang sedang berjalan.'); return; }
  const chosen = Array.from(files);
  if (chosen.length + state.clips.length > 100) { status('Maksimal 100 klip.'); return; }
  importBusy = true; pause(); status('Membaca media di perangkat ini…');
  const errors = [];
  try {
    for (const file of chosen) {
      let kind;
      if (/^image\/(jpeg|png|webp)$/.test(file.type) || /\.(jpe?g|png|webp)$/i.test(file.name)) kind = 'image';
      else if (file.type.startsWith('video/') || /\.(mp4|mov|webm|mkv)$/i.test(file.name)) kind = 'video';
      else { errors.push(`${file.name}: pilih video atau foto JPG/PNG/WebP.`); continue; }
      try {
        const meta = await readMetadata(file, kind);
        state.clips.push({ file, kind, ...meta, from: 0, to: Math.min(meta.sourceDuration, 10), duration: 5, fit: 'cover', mute: false });
        selectedClip = state.clips.length - 1;
        makeThumbnail(state.clips[selectedClip]);
      } catch (e) { errors.push(e.message); }
    }
  } finally { importBusy = false; $('mediaFiles').value = ''; }
  tab = 'clip'; time = offsetOf(selectedClip); render();
  if (errors.length) dialog('Beberapa media belum terbaca', errors);
  else status('Media siap. Potong klip, tambahkan teks, lalu pilih musik.');
}
function offsetOf(index) { return state.clips.slice(0, index).reduce((sum, c) => sum + duration(c), 0); }
function selectClip(index) { pause(); selectedClip = index; tab = 'clip'; time = offsetOf(index); render(); }
function reorder(index, destination) {
  if (destination < 0 || destination >= state.clips.length) return;
  pause(); const [clip] = state.clips.splice(index, 1); state.clips.splice(destination, 0, clip); selectedClip = destination; time = offsetOf(destination); render();
}
function renderLists() {
  $('clipCount').textContent = state.clips.length;
  $('mediaList').replaceChildren();
  if (!state.clips.length) {
    $('mediaList').append(node('div', 'Belum ada media. Mulai dari video atau foto milikmu.', 'empty-library'));

  }
  state.clips.forEach((c, i) => {
    c.thumbnail ||= thumbnailCache.get(c.url);
    const item = node('button', undefined, `media-item${i === selectedClip ? ' selected' : ''}`);
    const thumb = (c.thumbnail || c.kind === 'image') ? node('img') : node('span', '▶');
    thumb.className = 'media-thumb'; if (c.thumbnail || c.kind === 'image') { thumb.src = c.thumbnail || c.url; thumb.alt = ''; } else { thumb.style.display = 'grid'; thumb.style.placeItems = 'center'; thumb.style.color = '#fff'; }
    const info = node('span', undefined, 'media-info'); info.append(node('strong', clipName(c, i)), node('small', `${state.template ? 'Slot' : 'Klip'} ${i + 1} · ${duration(c).toFixed(1)} dtk`));
    item.append(thumb, info); item.addEventListener('click', () => selectClip(i)); $('mediaList').append(item);

  });
  tracks.render(); tracks.position(time);
}

const input = (id, label, value, attrs = '') => `<label>${label}<input id="${id}" value="${escape(value)}" ${attrs}></label>`;
const options = (items, selected) => items.map(([value, label]) => `<option value="${value}"${String(value) === String(selected) ? ' selected' : ''}>${label}</option>`).join('');
function bind(id, event, handler) { const el = $(id); if (el) el.addEventListener(event, handler); }
function editNumber(id, object, key, low, high, onChange = () => {}) {
  bind(id, 'change', (e) => { pause(); object[key] = clamp(num(e.target.value), low(), high()); e.target.value = object[key]; onChange(); refreshPreview(); });
}
function renderProperties() {
  document.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', String(b.dataset.tab === tab)));
  const p = $('properties');
  if (tab === 'clip') {
    const c = state.clips[selectedClip];
    p.innerHTML = c ? `<h3>${state.template ? 'Slot' : 'Klip'} ${selectedClip + 1}</h3><p class="hint">${escape(clipName(c, selectedClip))}${c.about ? ' · ' + escape(c.about) : ''}</p><button id="replaceClip" class="secondary full">${c.placeholder ? '＋ Isi slot ini' : '⇄ Ganti media'}</button>${c.kind === 'image' ? input('clipDuration', 'Durasi foto (detik)', c.duration, 'type="number" min="0.5" max="60" step="0.1"') : `<div class="two-col">${input('clipFrom', 'Mulai (detik)', c.from, 'type="number" min="0" step="0.1"')}${input('clipTo', 'Sampai (detik)', c.to, 'type="number" step="0.1"')}</div>`}<label>Bingkai<select id="clipFit">${options([['cover', 'Isi kanvas'], ['contain', 'Tampilkan seluruh klip']], c.fit)}</select></label><label class="check"><input id="clipMute" type="checkbox"${c.mute ? ' checked' : ''}> Matikan suara klip</label><div class="reorder"><button id="moveLeft">← Sebelumnya</button><button id="moveRight">Berikutnya →</button></div><button id="removeClip" class="danger">Hapus klip</button><hr class="section-rule">` : '<p class="hint">Pilih video atau foto untuk mulai mengedit.</p>';
    p.innerHTML += `<h3>Warna template</h3><label>Filter<select id="filter">${options([['none', 'Tanpa filter'], ['warm', 'Hangat'], ['cool', 'Dingin'], ['rose', 'Mawar'], ['pale', 'Pudar'], ['dark', 'Gelap']], state.filter)}</select></label>${input('filterAmount', 'Kekuatan', state.filterAmount, 'type="range" min="0" max="100"')}`;
    if (c) {
      const changed = () => { time = clamp(time, 0, totalDuration(state)); renderLists(); };
      editNumber('clipFrom', c, 'from', () => 0, () => c.to - .5, changed);
      editNumber('clipTo', c, 'to', () => c.from + .5, () => c.sourceDuration, changed);
      editNumber('clipDuration', c, 'duration', () => .5, () => 60, changed);
      bind('clipFit', 'change', (e) => { c.fit = e.target.value; refreshPreview(); });
      bind('replaceClip', 'click', () => $('replaceFile').click());
      bind('clipMute', 'change', (e) => { c.mute = e.target.checked; renderLists(); refreshPreview(); });
      bind('moveLeft', 'click', () => reorder(selectedClip, selectedClip - 1)); bind('moveRight', 'click', () => reorder(selectedClip, selectedClip + 1));
      bind('removeClip', 'click', () => { pause(); state.clips.splice(selectedClip, 1); selectedClip = Math.max(0, selectedClip - 1); time = Math.min(time, totalDuration(state)); render(); });
    }
    bind('filter', 'change', (e) => { state.filter = e.target.value; refreshPreview(); });
    bind('filterAmount', 'input', (e) => { state.filterAmount = num(e.target.value); refreshPreview(); });
  } else if (tab === 'text') {
    p.innerHTML = '<div id="textList" class="text-list"></div><button id="newText" class="secondary full">＋ Tambah isian teks</button>';
    state.texts.forEach((t, i) => { const b = node('button', `${i + 1}. ${t.text || 'Teks kosong'}`, `text-item${i === selectedText ? ' selected' : ''}`); b.addEventListener('click', () => { selectedText = i; time = t.start; pause(); renderProperties(); refreshPreview(); }); $('textList').append(b); });
    bind('newText', 'click', addText);
    const t = state.texts[selectedText];
    if (!t) { p.append(node('p', 'Teks menjadi isian yang bisa diganti agent. Geser teks langsung di preview untuk mengatur posisi.', 'hint')); return; }
    const controls = node('div');
    controls.innerHTML = `<label>Isi teks<textarea id="textValue" maxlength="2000">${escape(t.text)}</textarea></label><div class="two-col"><label>Font<select id="textFont">${options((fonts.includes(t.font) ? fonts : [t.font, ...fonts]).map((f) => [f, f[0].toUpperCase() + f.slice(1)]), t.font)}</select></label>${input('textSize', 'Ukuran', t.size, 'type="number" min="8" max="400"')}</div><div class="two-col">${input('textColor', 'Warna teks', t.color, 'type="color"')}${input('textBg', 'Warna latar', t.background, 'type="color"')}</div><label class="check"><input id="textBox" type="checkbox"${t.box ? ' checked' : ''}> Gunakan latar teks</label><div class="two-col">${input('textStart', 'Mulai (detik)', t.start, 'type="number" min="0" step="0.1"')}${input('textEnd', 'Sampai (detik)', t.end, 'type="number" min="0" step="0.1"')}</div><div class="two-col">${input('textX', 'Posisi X (%)', Math.round(t.x * 100), 'type="number" min="0" max="100"')}${input('textY', 'Posisi Y (%)', Math.round(t.y * 100), 'type="number" min="0" max="100"')}</div><p class="hint">Teks ini menjadi isian text${selectedText + 1}. Agent bisa mengganti kata-katanya.</p><button id="removeText" class="danger">Hapus teks</button>`;
    p.append(controls);
    bind('textValue', 'input', (e) => { t.text = e.target.value; refreshPreview(); });
    bind('textFont', 'change', (e) => { t.font = e.target.value; refreshPreview(); });
    bind('textColor', 'input', (e) => { t.color = e.target.value; refreshPreview(); });
    bind('textBg', 'input', (e) => { t.background = e.target.value; refreshPreview(); });
    bind('textBox', 'change', (e) => { t.box = e.target.checked; refreshPreview(); });
    editNumber('textSize', t, 'size', () => 8, () => 400);
    editNumber('textStart', t, 'start', () => 0, () => Math.max(0, t.end - .1));
    editNumber('textEnd', t, 'end', () => t.start + .1, () => totalDuration(state));
    ['x', 'y'].forEach((key) => bind(`text${key.toUpperCase()}`, 'change', (e) => { t[key] = clamp(num(e.target.value) / 100, 0, 1); refreshPreview(); }));
    bind('removeText', 'click', () => { pause(); state.texts.splice(selectedText, 1); selectedText = Math.max(0, selectedText - 1); render(); });
  } else {
    const m = state.music;
    p.innerHTML = '<h3>Musik bawaan</h3><p class="hint">Musik ikut di dalam paket. Agent dapat mempertahankan, mengganti, atau mematikannya.</p><button id="chooseMusic" class="secondary full">Pilih musik / audio video</button>';
    bind('chooseMusic', 'click', () => $('musicFile').click());
    if (!m) { p.append(node('p', 'Satu berkas, maksimal 32 MiB. Tidak ada unggahan ke server.', 'hint')); return; }
    const controls = node('div');
    controls.innerHTML = `<p class="hint">${escape(m.file.name)} · ${(m.file.size / 1048576).toFixed(1)} MiB</p><div class="two-col">${input('musicFrom', 'Mulai sumber', m.from, 'type="number" min="0" step="0.1"')}${input('musicAt', 'Posisi di video', m.at, 'type="number" min="0" step="0.1"')}</div>${input('musicDuration', 'Durasi musik (detik)', m.duration, 'type="number" min="0.1" step="0.1"')}${input('musicVolume', 'Volume', m.volume, 'type="range" min="0" max="100"')}<p class="hint">Seluruh berkas musik ikut, termasuk bagian di luar potongan. Jika sumbernya video, gambar videonya juga ikut.</p><button id="removeMusic" class="danger">Tanpa musik bawaan</button>`;
    p.append(controls);
    editNumber('musicFrom', m, 'from', () => 0, () => Math.max(0, m.sourceDuration - .1), () => { m.duration = Math.min(m.duration, m.sourceDuration - m.from); $('musicDuration').value = m.duration; });
    editNumber('musicAt', m, 'at', () => 0, () => Math.max(0, totalDuration(state) - .1));
    editNumber('musicDuration', m, 'duration', () => .1, () => Math.min(600, m.sourceDuration - m.from));
    bind('musicVolume', 'input', (e) => { m.volume = num(e.target.value); musicPlayer.volume = m.volume / 100; });
    bind('removeMusic', 'click', () => { pause(); musicPlayer.removeAttribute('src'); state.music = null; render(); });
  }
}

function addText() {
  if (!state.clips.length) { status('Tambahkan klip terlebih dahulu.'); return; }
  if (state.texts.length >= 100) { status('Maksimal 100 isian teks.'); return; }
  pause(); const total = totalDuration(state); time = Math.min(time, Math.max(0, total - .5));
  state.texts.push({ text: 'Tulis judulmu di sini', font: 'poppins', size: 64, color: '#FFFFFF', box: true, background: '#171717', start: time, end: Math.min(time + 3, total), x: .5, y: .25 });
  selectedText = state.texts.length - 1; tab = 'text'; renderProperties(); refreshPreview(); tracks.render(); tracks.position(time); checkpoint(); $('textValue')?.focus();
}
function sizeStage() {
  const [a, b] = state.ratio.split(':').map(Number); const box = $('dropZone').getBoundingClientRect();
  const h = Math.min(box.height - 2, (box.width - 2) * b / a); $('stage').style.width = `${h * a / b}px`; $('stage').style.height = `${h}px`;
}
function renderTexts() {
  const layer = $('textLayer'); layer.replaceChildren(); const width = $('stage').clientWidth;
  (state.template?.gradients || []).forEach(g => {
    if (time < (g.start || 0) || (g.end != null && time >= g.end)) return;
    const n = node('div', '', 'preview-gradient'); n.style.background = gradientCSS(g); layer.append(n);
  });
  state.texts.forEach((t, i) => {
    if (time < t.start || time >= t.end) return;
    const n = node('div', t.text, `preview-text${tab === 'text' && selectedText === i ? ' selected' : ''}`);
    n.style.left = `${t.x * 100}%`; n.style.top = `${t.y * 100}%`; n.style.color = t.color; n.style.fontSize = `${t.size * width / 1080}px`;
    n.style.fontFamily = `WC-${fonts.includes(t.font) ? t.font : 'poppins'}`;
    const ex = t.extra || {}, alpha = Math.round(255 * (ex.backgroundOpacity ?? 85) / 100).toString(16).padStart(2, '0');
    n.style.background = t.box ? t.background + alpha : 'transparent';
    // A template's text keeps its look in the preview: slant, capitals,
    // alignment, line spacing, card padding and wrap width.
    if (t.italic) n.style.fontStyle = 'italic';
    if (t.uppercase) n.style.textTransform = 'uppercase';
    if (t.align === 'left') { n.style.transform = 'translate(0,-50%)'; n.style.textAlign = 'left'; }
    if (t.align === 'right') { n.style.transform = 'translate(-100%,-50%)'; n.style.textAlign = 'right'; }
    if (ex.lineHeight) n.style.lineHeight = String(ex.lineHeight);
    if (ex.maxWidth) n.style.maxWidth = `${ex.maxWidth * 100}%`;
    if (ex.padding?.length) { const k = width / 1080, [px, py] = [ex.padding[1] ?? ex.padding[0], ex.padding[0]]; n.style.padding = `${py * k}px ${px * k}px`; }
    if (ex.stroke && ex.stroke !== 'none' && !t.box) n.style.webkitTextStroke = `${Math.max(1, t.size * width / 1080 * 0.05)}px ${ex.stroke}`;
    n.addEventListener('pointerdown', (e) => {
      e.preventDefault(); pause(); selectedText = i; tab = 'text'; renderProperties(); n.setPointerCapture(e.pointerId);
      const box = $('stage').getBoundingClientRect(); const startX = e.clientX, startY = e.clientY, x = t.x, y = t.y;
      const move = (event) => { t.x = clamp(x + (event.clientX - startX) / box.width, 0, 1); t.y = clamp(y + (event.clientY - startY) / box.height, 0, 1); n.style.left = `${t.x * 100}%`; n.style.top = `${t.y * 100}%`; if ($('textX')) $('textX').value = Math.round(t.x * 100); if ($('textY')) $('textY').value = Math.round(t.y * 100); };
      n.addEventListener('pointermove', move);
      const done = () => { n.removeEventListener('pointermove', move); checkpoint(); refreshPreview(); };
      n.addEventListener('pointerup', done, { once: true }); n.addEventListener('pointercancel', done, { once: true });
    });
    layer.append(n);
  });
}
function refreshPreview() {
  tracks.position(time);
  const total = totalDuration(state); time = clamp(time, 0, total);
  $('scrub').max = total; $('scrub').value = time; $('timeLabel').textContent = `${fmt(time)} / ${fmt(total)}`;
  const point = locate(state, Math.min(time, Math.max(0, total - .001)));
  $('stage').hidden = !point; $('emptyPreview').hidden = !!point;
  if (!point) { video.pause(); photo.hidden = true; video.hidden = true; return; }
  sizeStage(); const { clip: c, local } = point;
  photo.hidden = c.kind !== 'image'; video.hidden = c.kind !== 'video';
  if (c.kind === 'image') { video.pause(); if (photo.src !== c.url) photo.src = c.url; photo.style.objectFit = c.fit; }
  else {
    const changed = video.getAttribute('src') !== c.url;
    if (changed) video.src = c.url;
    video.style.objectFit = c.fit; video.muted = !!c.mute;
    const target = Math.min(c.to - .001, c.from + local);
    if (changed || Math.abs(video.currentTime - target) > (playing ? .2 : .02)) { try { video.currentTime = target; } catch { /* metadata loads asynchronously */ } }
    if (playing && video.paused) video.play().catch(() => { pause(); status('Browser tidak dapat memutar klip ini. Coba MP4 H.264.'); });
  }
  const tint = filters[state.filter]; $('tint').style.background = tint ? tint[0] : 'transparent'; $('tint').style.opacity = tint ? tint[1] * state.filterAmount / 100 : 0;
  const m = state.music;
  if (m && playing && time >= m.at && time < m.at + m.duration) {
    if (musicPlayer.getAttribute('src') !== m.url) musicPlayer.src = m.url;
    const target = m.from + time - m.at;
    if (Math.abs(musicPlayer.currentTime - target) > .2) musicPlayer.currentTime = target;
    musicPlayer.volume = m.volume / 100;
    if (musicPlayer.paused) musicPlayer.play().catch(() => { pause(); status('Musik belum dapat diputar. Pilih ulang atau gunakan MP3.'); });
  } else musicPlayer.pause();
  renderTexts();
}
function pause() { playing = false; cancelAnimationFrame(frame); video.pause(); musicPlayer.pause(); $('playButton').textContent = '▶'; $('playButton').setAttribute('aria-label', 'Putar preview'); }
function tick(now) { if (!playing) return; time = (now - anchor) / 1000; if (time >= totalDuration(state)) { time = totalDuration(state); pause(); } refreshPreview(); if (playing) frame = requestAnimationFrame(tick); }
function play() {
  if (!state.clips.length) { status('Tambahkan klip terlebih dahulu.'); return; }
  if (playing) { pause(); return; }
  if (time >= totalDuration(state) - .02) time = 0;
  playing = true; anchor = performance.now() - time * 1000; $('playButton').textContent = 'Ⅱ'; $('playButton').setAttribute('aria-label', 'Jeda preview'); refreshPreview(); frame = requestAnimationFrame(tick);
}
function render() { renderLists(); renderProperties(); refreshPreview(); checkpoint(); }

async function chooseMusic(file) {
  if (!file) return;
  if (importBusy || exporting) { status('Tunggu proses yang sedang berjalan.'); return; }
  if (!state.clips.length) { status('Tambahkan klip sebelum musik.'); return; }
  if (file.size > MAX_AUDIO_BYTES) { status('Musik maksimal 32 MiB.'); return; }
  pause(); importBusy = true;
  try {
    const metadata = await readMetadata(file, 'audio');

    state.music = { file, ...metadata, from: 0, at: 0, duration: Math.min(metadata.sourceDuration, totalDuration(state), 600), volume: 50 };
    makeWaveform(state.music);
    musicPlayer.src = metadata.url; tab = 'music'; render(); status('Musik ini akan ikut di dalam paket template.');
  } catch (e) { dialog('Musik belum bisa dibaca', [e.message]); }
  finally { importBusy = false; }
  $('musicFile').value = '';
}
function download(name, text, type = 'application/json') {
  const url = URL.createObjectURL(new Blob([text], { type })); const a = node('a'); a.href = url; a.download = name; document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(url), 60000);
}
function encode(bytes) { let binary = ''; for (let i = 0; i < bytes.length; i += 32768) binary += String.fromCharCode(...bytes.subarray(i, i + 32768)); return btoa(binary); }
function beginExport() {
  if (exporting || importBusy) { status('Tunggu proses yang sedang berjalan.'); return; }
  pause(); const errors = validate(state);
  if (errors.length) { dialog('Periksa template dulu', errors); return; }
  const notes = [`${state.clips.length} ${state.template ? 'slot media' : 'bagian video'}, ${state.texts.length} isian teks, ${totalDuration(state).toFixed(1)} detik.${state.template ? ' Potongan, transisi, zoom, teks otomatis dan slot logo dari template ikut.' : ''}`, 'Footage contoh tidak ikut. Kirim paket ini beserta footage pengganti ke agent di perangkat mana pun.', state.music ? `Musik “${state.music.file.name}” ikut utuh, termasuk bagian di luar potongan. Jika sumber musik berupa video, seluruh gambar videonya juga ikut.` : 'Paket ini tanpa musik bawaan. Suara klip mengikuti pengaturan tiap bagian.', 'Paket berisi kata-kata teksmu sebagai contoh. Periksa isinya sebelum membagikan.'];
  dialog('Bawa template ke agent', notes, { label: 'Unduh paket template', run: async () => {
    if (exporting) return; exporting = true;
    dialog('Menyiapkan paket', ['Musik dimasukkan langsung dari perangkatmu. Tidak ada unggahan ke server.']);
    try {
      const bundle = await toBundle(state, encode);
      const name = state.name.replace(/[^a-z0-9-]/gi, '-').replace(/-+/g, '-').slice(0, 60) || 'template';
      download(`${name}.wecut.json`, JSON.stringify(bundle));
      dialog('Template siap dikirim', ['Lampirkan berkas .wecut.json dan footage baru ke chat agent.', 'Contoh pesan: “Pakai template ini, ganti video dan teksnya, musik tetap. Kirim preview dulu.”', state.template ? 'Agent memerlukan WeCut 0.3.0 atau lebih baru.' : 'Agent memerlukan WeCut 0.2.0 atau lebih baru.']);
      const code = node('pre', `wecut recipe unpack ${name}.wecut.json --write templates/my-style\nwecut recipe show templates/my-style/recipe.json`);
      $('dialogContent').append(code);
    } catch (e) { dialog('Paket belum bisa dibuat', [e.message]); }
    finally { exporting = false; }
  } });
}

$('addMedia').addEventListener('click', () => $('mediaFiles').click()); $('pickFirst').addEventListener('click', () => $('mediaFiles').click());
$('mediaFiles').addEventListener('change', (e) => addFiles(e.target.files));
$('musicFile').addEventListener('change', (e) => chooseMusic(e.target.files[0]));
$('dropZone').addEventListener('dragover', (e) => { if (e.dataTransfer.types.includes('Files')) { e.preventDefault(); $('dropZone').classList.add('dragover'); } });
$('dropZone').addEventListener('dragleave', () => $('dropZone').classList.remove('dragover'));
$('dropZone').addEventListener('drop', (e) => { e.preventDefault(); $('dropZone').classList.remove('dragover'); if (e.dataTransfer.files.length) addFiles(e.dataTransfer.files); });
$('addText').addEventListener('click', addText); $('playButton').addEventListener('click', play);
$('scrub').addEventListener('input', (e) => { pause(); time = num(e.target.value); refreshPreview(); });
$('projectName').addEventListener('input', (e) => { state.name = e.target.value; });
$('ratio').addEventListener('change', (e) => { state.ratio = e.target.value; refreshPreview(); });
$('resolution').addEventListener('change', (e) => { state.res = num(e.target.value); }); $('fps').addEventListener('change', (e) => { state.fps = num(e.target.value); });
document.querySelectorAll('[data-tab]').forEach((b) => b.addEventListener('click', () => { tab = b.dataset.tab; renderProperties(); tracks.render(); refreshPreview(); }));
$('exportButton').addEventListener('click', beginExport);
$('helpButton').addEventListener('click', () => dialog('Dari browser ke perangkat agent', ['1. Pilih video/foto contoh, potong klip dan atur urutannya.', '2. Tambahkan teks dan musik. Geser teks langsung di preview.', '3. Unduh template. Kirim file itu dan footage baru lewat chat agent.', '4. Agent mengimpor paket dan membuat video di komputernya sendiri.', 'Browser ini tidak mengunggah media. Alamat localhost hanya untuk perangkatmu. Halaman editor dapat dihosting sebagai situs statis; tautan template publik membutuhkan penyimpanan file tersendiri.']));
new ResizeObserver(refreshPreview).observe($('dropZone'));
document.addEventListener('visibilitychange', () => { if (document.hidden) pause(); });
window.addEventListener('beforeunload', (e) => { if (state.clips.length) { e.preventDefault(); e.returnValue = ''; } });
video.addEventListener('loadedmetadata', refreshPreview);

function checkpoint() {
  if (!restoring) history.record(state);
  $('undo').disabled = !history.canUndo; $('redo').disabled = !history.canRedo;
}
function restore(direction) {
  if (importBusy || exporting) return;
  const snapshot = history[direction](); if (!snapshot) return;
  pause(); restoring = true; Object.assign(state, snapshot);
  selectedClip = Math.max(0, Math.min(selectedClip, state.clips.length - 1)); selectedText = Math.max(0, Math.min(selectedText, state.texts.length - 1));
  $('projectName').value = state.name; $('ratio').value = state.ratio; $('resolution').value = state.res; $('fps').value = state.fps;
  time = Math.min(time, totalDuration(state)); render(); restoring = false;
}
function seekTo(value) { pause(); time = clamp(value, 0, totalDuration(state)); refreshPreview(); }
function selectTrack(kind, index) {
  pause(); tab = kind === 'clip' ? 'clip' : kind === 'text' ? 'text' : 'music';
  if (kind === 'clip') selectedClip = index;
  else if (kind === 'text') { selectedText = index; time = state.texts[index].start; }
  renderProperties(); tracks.render(); refreshPreview();
}
function splitSelected() {
  pause(); const point = locate(state, time);
  if (!point || !splitClip(state, point.index, point.local)) { status('Letakkan playhead di dalam klip, minimal 0,5 detik dari tepinya.'); return; }
  selectedClip = point.index + 1; tab = 'clip'; render();
}
function deleteSelected() {
  pause();
  if (tab === 'text') { state.texts.splice(selectedText, 1); selectedText = Math.max(0, selectedText - 1); }
  else if (tab === 'music') { state.music = null; musicPlayer.removeAttribute('src'); }
  else { state.clips.splice(selectedClip, 1); selectedClip = Math.max(0, selectedClip - 1); }
  time = Math.min(time, totalDuration(state)); render();
}
async function makeThumbnail(clip) {
  if (clip.kind !== 'video') return;
  const v = document.createElement('video'); v.preload = 'metadata'; v.muted = true;
  let timer;
  try {
    await new Promise((resolve, reject) => { timer = setTimeout(reject, 10000); v.onloadedmetadata = () => { v.currentTime = Math.min(.15, v.duration / 2); }; v.onseeked = resolve; v.onerror = reject; v.src = clip.url; });
    const canvas = document.createElement('canvas'); canvas.width = 160; canvas.height = Math.round(160 * v.videoHeight / v.videoWidth);
    canvas.getContext('2d').drawImage(v, 0, 0, canvas.width, canvas.height); clip.thumbnail = canvas.toDataURL('image/jpeg', .65);
    thumbnailCache.set(clip.url, clip.thumbnail);
    renderLists(); refreshPreview();
  } catch { /* The source remains usable without a thumbnail. */ }
  finally { clearTimeout(timer); v.removeAttribute('src'); v.load(); }
}
async function makeWaveform(music) {
  if (music.file.size > 16 * 1024 * 1024 || music.sourceDuration > 600) return;
  let context;
  try {
    context = new AudioContext();
    const decoded = await context.decodeAudioData(await music.file.arrayBuffer());
    const samples = decoded.getChannelData(0), count = 160, stride = Math.max(1, Math.floor(samples.length / count));
    music.peaks = Array.from({length:count}, (_, i) => { let peak = 0; for (let j = i * stride; j < Math.min(samples.length, (i + 1) * stride); j += 8) peak = Math.max(peak, Math.abs(samples[j])); return peak; });
    if (state.music === music) { tracks.render(); tracks.position(time); }
  } catch { /* Audio preview/export works even if waveform decoding is unsupported. */ }
  finally { if (context) await context.close(); }
}
function showAsset(section) {
  for (const name of ['media', 'audio', 'text', 'filter', 'clone']) $('asset' + name[0].toUpperCase() + name.slice(1)).hidden = name !== section;
  document.querySelectorAll('[data-asset]').forEach(b => b.classList.toggle('active', b.dataset.asset === section));
}
document.querySelectorAll('[data-asset]').forEach(b => b.addEventListener('click', () => showAsset(b.dataset.asset)));
$('importAudio').addEventListener('click', () => $('musicFile').click());

// Templates: a cloned video or an imported template opens as a project.
// Each shot is a clip on the timeline (a slot); its media can be replaced,
// and the export keeps the template's own detail (exact shots,
// transitions, zoom, captions, logo slot, text looks).
function clipName(c, i) { return c.file?.name || (c.placeholder ? `Slot ${i + 1} kosong` : `Klip ${i + 1}`); }
function replaceState(next) {
  pause();
  for (const key of Object.keys(state)) delete state[key];
  Object.assign(state, next);
  selectedClip = 0; selectedText = 0; time = 0; tab = 'clip';
  $('projectName').value = state.name; $('ratio').value = state.ratio;
  render();
}
// slate draws a numbered stand-in for an empty slot.
function slate(n, seconds, about, ratio) {
  const [a, b] = ratio.split(':').map(Number), w = 360, h = Math.round(w * b / a);
  const c = document.createElement('canvas'); c.width = w; c.height = h; const g = c.getContext('2d');
  const hues = ['#3a4c6e', '#604068', '#366256', '#705434', '#464646', '#683840'];
  g.fillStyle = hues[(n - 1) % hues.length]; g.fillRect(0, 0, w, h);
  g.fillStyle = 'rgba(255,255,255,.92)'; g.textAlign = 'center'; g.font = `700 ${Math.round(w / 4)}px Arial`; g.fillText(String(n), w / 2, h / 2);
  g.font = `600 ${Math.round(w / 16)}px Arial`; g.fillText(`${seconds.toFixed(2)} dtk`, w / 2, h / 2 + w / 9);
  g.fillStyle = 'rgba(255,255,255,.7)'; g.font = `${Math.round(w / 22)}px Arial`;
  const words = (about || 'Isi dengan videomu').split(' '); let line = '', y = h / 2 + w / 5;
  for (const word of words) { if (g.measureText(line + word).width > w * .85) { g.fillText(line, w / 2, y); line = ''; y += w / 18; } line += word + ' '; }
  g.fillText(line, w / 2, y);
  return c.toDataURL('image/png');
}
function openTemplate(recipe, media, useText) {
  replaceState(stateFromTemplate(recipe, media, useText));
  showAsset('media');
  status(`Template “${state.name}” dibuka: ${state.clips.length} slot. Pilih slot lalu “Ganti media”, ubah teks di tab Teks, lalu Ekspor.`);
}

// Clone: a finished video becomes a template, analysed on this device,
// and opens in the editor with the video itself in every slot.
let cloned = null, cloning = false;
$('cloneVideo').addEventListener('click', () => { if (!cloning) $('cloneFile').click(); });
$('cloneFile').addEventListener('change', async (event) => {
  const file = event.target.files[0]; event.target.value = '';
  if (!file) return;
  if (importBusy || exporting) { status('Tunggu proses yang sedang berjalan.'); return; }
  cloning = true; pause(); $('cloneResult').replaceChildren();
  const progress = $('cloneProgress'); progress.hidden = false;
  try {
    cloned = await cloneVideo(file, (f, label) => { progress.textContent = `${label} ${Math.round(f * 100)}%`; });
    const meta = await readMetadata(file, 'video');
    const r = cloned.recipe;
    // Without OCR in the browser, signs and screens in the footage can look
    // like laid-over text; the headline card and its label are found
    // reliably, so only they start in the editor. The rest are listed here.
    for (const t of r.texts || []) {
      t._use = t.slot === 'headline' || t.slot === 'label';
      // The browser reads no words; say what to type, in the Studio's language.
      t.text = { 'Your headline here': 'Tulis judulmu di sini', LABEL: 'LABEL', 'Your text': 'Tulis teks di sini', 'Follow for more': 'Ikuti untuk lanjutannya' }[t.text] ?? t.text.replace(/Your headline here/g, 'Tulis judulmu di sini');
    }
    openTemplate(r, (i) => {
      const s = cloned.slots[i];
      return { file, kind: 'video', url: meta.url, sourceDuration: meta.sourceDuration, from: s.start, to: Math.min(meta.sourceDuration, s.start + s.dur), duration: s.dur, thumbnail: s.thumbnail };
    }, (t) => t._use);
    if (r.music && cloned.music) {
      state.music = { file, url: meta.url, sourceDuration: meta.sourceDuration, from: 0, at: 0, duration: Math.min(meta.sourceDuration, totalDuration(state)), volume: 100 };
      makeWaveform(state.music); render();
    }
    progress.hidden = true; renderClone(); showAsset('clone');
  } catch (e) { progress.hidden = true; dialog('Video belum bisa dijadikan template', [e.message]); }
  finally { cloning = false; }
});
function renderClone() {
  const box = $('cloneResult'), r = cloned.recipe;
  const head = node('div', undefined, 'panel-heading'); head.append(node('h2', r.name), node('span', `${cloned.slots.length} slot`, 'count'));
  const info = node('p', `${r.maxDuration.toFixed(1)} dtk · ${r.output.ratio}${r.captions ? ' · teks otomatis' : ''}${r.overlays ? ' · slot logo' : ''}${r.music ? ' · musik asli' : ''}. Sudah dibuka di editor: tiap potongan jadi satu slot di timeline.`, 'hint');
  const slots = node('div', undefined, 'clone-slots');
  cloned.slots.forEach((s, i) => {
    const item = node('button', undefined, 'clone-slot');
    const img = node('img'); img.src = s.thumbnail; img.alt = '';
    const text = node('div'); text.append(node('strong', `Slot ${s.n} · ${s.dur.toFixed(2)} dtk${s.join ? ' → ' + s.join : ''}`), node('span', s.about));
    item.append(img, text); item.addEventListener('click', () => { if (state.template && i < state.clips.length) selectClip(i); }); slots.append(item);
  });
  const words = node('div', undefined, 'clone-texts');
  (r.texts || []).forEach((t) => {
    const label = node('label');
    const head = node('span', undefined, 'clone-text-head');
    const use = node('input'); use.type = 'checkbox'; use.checked = !!t._use;
    use.addEventListener('change', () => {
      t._use = use.checked;
      const at = state.texts.findIndex((x) => x.extra?.slot === t.slot);
      if (use.checked && at < 0) state.texts.push(textFromSlot(t, totalDuration(state)));
      if (!use.checked && at >= 0) state.texts.splice(at, 1);
      selectedText = Math.max(0, Math.min(selectedText, state.texts.length - 1)); render();
    });
    head.append(use, node('span', `“${t.slot}” · ${(t.start ?? 0).toFixed(1)}–${(t.end ?? r.maxDuration).toFixed(1)} dtk${t.background && t.background !== 'none' ? ' · kartu' : ''}`));
    label.append(head); words.append(label);
  });
  const hints = node('ul', undefined, 'clone-hints'); (cloned.hints || []).forEach((h) => hints.append(node('li', h)));
  box.replaceChildren(head, info, node('h3', 'Slot media'), slots, ...(r.texts?.length ? [node('h3', 'Teks yang ditemukan (centang = masuk editor)'), words] : []), node('h3', 'Catatan'), hints);
}

// Replace the selected clip's media, keeping its slot length.
$('replaceFile').addEventListener('change', async (event) => {
  const file = event.target.files[0]; event.target.value = '';
  const c = state.clips[selectedClip];
  if (!file || !c) return;
  const kind = /^image\//.test(file.type) || /\.(jpe?g|png|webp)$/i.test(file.name) ? 'image' : 'video';
  const want = duration(c);
  try {
    const meta = await readMetadata(file, kind);
    Object.assign(c, { file, kind, ...meta, placeholder: false, thumbnail: undefined, from: 0, to: kind === 'video' ? Math.min(meta.sourceDuration, want) : c.to, duration: want });
    if (kind === 'video' && meta.sourceDuration < want - .05) status(`Video ini ${meta.sourceDuration.toFixed(1)} dtk, slotnya ${want.toFixed(1)} dtk. Slot jadi lebih pendek.`);
    makeThumbnail(c);
    // Slots that are still this source take its next seconds.
    let at = kind === 'video' ? c.to : 0;
    for (let k = selectedClip + 1; state.clips[k]?.continues; k++) {
      const n = state.clips[k], need = duration(n);
      Object.assign(n, { file, kind, ...meta, placeholder: false, thumbnail: undefined, from: at, to: kind === 'video' ? Math.min(meta.sourceDuration, at + need) : n.to, duration: need });
      if (kind === 'video') at = n.to;
      makeThumbnail(n);
    }
    render();
  } catch (e) { dialog('Media belum bisa dipakai', [e.message]); }
});

// Import a template package (.wecut.json) or a recipe (.json): its slots
// open as numbered stand-ins to fill.
$('importTemplate').addEventListener('click', () => $('templateFile').click());
$('templateFile').addEventListener('change', async (event) => {
  const file = event.target.files[0]; event.target.value = '';
  if (!file) return;
  try {
    const data = JSON.parse(await file.text());
    const recipe = data.bundle === 'wecut.bundle/1' ? data.recipe : data;
    if (!recipe || recipe.recipe !== '1' || !Array.isArray(recipe.sections) || !recipe.sections.length) throw new Error('Ini bukan template WeCut (wecut.recipe/1 atau paket .wecut.json).');
    const ratio = ['9:16', '16:9', '1:1', '4:5'].includes(recipe.output?.ratio) ? recipe.output.ratio : '9:16';
    openTemplate(recipe, (i, x) => ({ file: null, kind: 'image', placeholder: true, url: slate(i + 1, x.shot.dur || 3, x.shot.about || x.section.about, ratio), sourceDuration: 600, from: 0, to: 0, duration: x.shot.dur || x.section.duration || 3 }));
    // The package's own music, if it carries one.
    const src = recipe.music?.src || '', asset = (data.assets || []).find((a) => src === `assets/${a.name}`);
    if (asset) {
      const bytes = Uint8Array.from(atob(asset.data), (ch) => ch.charCodeAt(0));
      const music = new File([bytes], asset.name, { type: /\.(mp4|mov|webm)$/i.test(asset.name) ? 'video/mp4' : 'audio/mpeg' });
      const meta = await readMetadata(music, 'audio');
      state.music = { file: music, ...meta, from: recipe.music.from || 0, at: recipe.music.at || 0, duration: Math.min(meta.sourceDuration, recipe.music.duration || totalDuration(state)), volume: recipe.music.volume ?? 100 };
      makeWaveform(state.music); render();
    }
  } catch (e) { dialog('Template belum bisa dibuka', [e.message]); }
});
for (const [key, label] of [['none','Original'],['warm','Hangat'],['cool','Dingin'],['rose','Mawar'],['pale','Pudar'],['dark','Gelap']]) {
  const b = node('button', undefined, 'filter-preset'); b.append(node('span', '◐'), node('small', label));
  b.addEventListener('click', () => { state.filter = key; tab = 'clip'; render(); }); $('filterPresets').append(b);
}
$('undo').addEventListener('click', () => restore('undo')); $('redo').addEventListener('click', () => restore('redo'));
$('split').addEventListener('click', splitSelected); $('deleteSelection').addEventListener('click', deleteSelected);
$('frameBack').addEventListener('click', () => seekTo(time - 1 / state.fps)); $('frameNext').addEventListener('click', () => seekTo(time + 1 / state.fps));
$('fullscreen').addEventListener('click', () => { if (document.fullscreenElement) document.exitFullscreen(); else $('dropZone').requestFullscreen().catch(() => status('Layar penuh tidak tersedia di browser ini.')); });
function zoomTo(value) { $('timelineZoom').value = clamp(Math.floor(value), 1, 200); tracks.zoom(Number($('timelineZoom').value)); tracks.position(time); }
$('timelineZoom').addEventListener('input', e => zoomTo(Number(e.target.value)));
$('zoomIn').addEventListener('click', () => zoomTo(Number($('timelineZoom').value) + 20)); $('zoomOut').addEventListener('click', () => zoomTo(Number($('timelineZoom').value) - 20));
$('fitTimeline').addEventListener('click', () => zoomTo(($('timelineScroll').clientWidth - 120) / Math.max(1, totalDuration(state))));
for (const name of ['input', 'change']) document.addEventListener(name, e => { if (['scrub', 'timelineZoom'].includes(e.target.id)) return; checkpoint(); tracks.render(); tracks.position(time); });
document.addEventListener('keydown', e => {
  if ($('dialog').open || e.target.closest('input,textarea,select,[contenteditable]')) return;
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key.toLowerCase() === 'z') { e.preventDefault(); restore(e.shiftKey ? 'redo' : 'undo'); }
  else if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); restore('redo'); }
  else if (mod && e.key.toLowerCase() === 'b') { e.preventDefault(); splitSelected(); }
  else if (e.code === 'Space') { e.preventDefault(); play(); }
  else if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); deleteSelected(); }
  else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); seekTo(time + (e.key === 'ArrowLeft' ? -1 : 1) / state.fps); }
});
$('timelineResizer').addEventListener('pointerdown', e => {
  e.preventDefault(); const handle = e.currentTarget; handle.setPointerCapture(e.pointerId);
  const change = event => document.documentElement.style.setProperty('--timeline-height', `${clamp(window.innerHeight - event.clientY - 22, 170, window.innerHeight * .6)}px`);
  const end = () => { handle.removeEventListener('pointermove', change); handle.removeEventListener('pointerup', end); handle.removeEventListener('pointercancel', end); };
  handle.addEventListener('pointermove', change); handle.addEventListener('pointerup', end); handle.addEventListener('pointercancel', end);
});
$('timelineResizer').addEventListener('keydown', e => { if (!['ArrowUp', 'ArrowDown'].includes(e.key)) return; e.preventDefault(); const height = document.querySelector('.timeline-panel').clientHeight; document.documentElement.style.setProperty('--timeline-height', `${clamp(height + (e.key === 'ArrowUp' ? 20 : -20), 170, window.innerHeight * .6)}px`); });
const icons = {
  undo:'M8 5 3 10l5 5M3 10h10a6 6 0 0 1 0 12', redo:'m16 5 5 5-5 5m5-5H11a6 6 0 0 0 0 12',
  split:'M9 9 20 20M9 15 20 4M3 6a3 3 0 1 0 6 0 3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0 3 3 0 1 0-6 0',
  deleteSelection:'M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7',
  frameBack:'M5 5v14m13-14-9 7 9 7Z', frameNext:'M19 5v14M6 5l9 7-9 7Z', fullscreen:'M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5',
  media:'M3 4h18v16H3ZM3 15l5-5 5 5 3-3 5 5M16 8h.01', audio:'M9 18V5l12-2v13M9 5v5l12-2M3 18a3 2 0 1 0 6 0 3 2 0 1 0-6 0m12-2a3 2 0 1 0 6 0 3 2 0 1 0-6 0',
  text:'M4 4h16M12 4v16m-4 0h8M4 4v4m16-4v4', filter:'M10 12a6 6 0 1 0 0 .01M20 12a6 6 0 1 0 0 .01',
};
for (const [key, path] of Object.entries(icons)) {
  const target = $(key) || document.querySelector(`[data-asset="${key}"] span`);
  if (target) target.innerHTML = `<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="${path}"/></svg>`;
}
render();
