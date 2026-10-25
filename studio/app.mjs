import { initialState, duration, totalDuration, locate, validate, toBundle, filters, fonts, MAX_AUDIO_BYTES } from './model.mjs';

const $ = (id) => document.getElementById(id);
const state = initialState();
let selectedClip = 0, selectedText = 0, tab = 'clip', time = 0, playing = false, anchor = 0, frame = 0, exporting = false;
let statusTimer, dragId, importBusy = false;
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
  $('mediaList').replaceChildren(); $('timeline').replaceChildren();
  if (!state.clips.length) {
    $('mediaList').append(node('div', 'Belum ada media. Mulai dari video atau foto milikmu.', 'empty-library'));
    $('timeline').append(node('p', 'Klip yang kamu tambahkan akan muncul di sini.', 'hint'));
  }
  state.clips.forEach((c, i) => {
    const item = node('button', undefined, `media-item${i === selectedClip ? ' selected' : ''}`);
    const thumb = c.kind === 'image' ? node('img') : node('span', '▶');
    thumb.className = 'media-thumb'; if (c.kind === 'image') { thumb.src = c.url; thumb.alt = ''; } else { thumb.style.display = 'grid'; thumb.style.placeItems = 'center'; thumb.style.color = '#fff'; }
    const info = node('span', undefined, 'media-info'); info.append(node('strong', c.file.name), node('small', `Klip ${i + 1} · ${duration(c).toFixed(1)} dtk`));
    item.append(thumb, info); item.addEventListener('click', () => selectClip(i)); $('mediaList').append(item);
    const card = node('button', undefined, `timeline-card${i === selectedClip ? ' selected' : ''}`);
    card.style.width = `${clamp(duration(c) * 12, 105, 170)}px`;
    card.append(node('strong', `${c.kind === 'image' ? 'Foto' : 'Klip'} ${i + 1}`), node('small', `${duration(c).toFixed(1)} dtk · ${c.mute ? 'tanpa suara' : 'suara asli'}`));
    card.draggable = true;
    card.addEventListener('dragstart', () => { dragId = i; });
    card.addEventListener('dragover', (e) => e.preventDefault());
    card.addEventListener('drop', (e) => { e.preventDefault(); if (Number.isInteger(dragId)) reorder(dragId, i); dragId = null; });
    card.addEventListener('dragend', () => { dragId = null; });
    card.addEventListener('click', () => selectClip(i)); $('timeline').append(card);
  });
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
    p.innerHTML = c ? `<h3>Klip ${selectedClip + 1}</h3><p class="hint">${escape(c.file.name)}</p>${c.kind === 'image' ? input('clipDuration', 'Durasi foto (detik)', c.duration, 'type="number" min="0.5" max="60" step="0.1"') : `<div class="two-col">${input('clipFrom', 'Mulai (detik)', c.from, 'type="number" min="0" step="0.1"')}${input('clipTo', 'Sampai (detik)', c.to, 'type="number" step="0.1"')}</div>`}<label>Bingkai<select id="clipFit">${options([['cover', 'Isi kanvas'], ['contain', 'Tampilkan seluruh klip']], c.fit)}</select></label><label class="check"><input id="clipMute" type="checkbox"${c.mute ? ' checked' : ''}> Matikan suara klip</label><div class="reorder"><button id="moveLeft">← Sebelumnya</button><button id="moveRight">Berikutnya →</button></div><button id="removeClip" class="danger">Hapus klip</button><hr class="section-rule">` : '<p class="hint">Pilih video atau foto untuk mulai mengedit.</p>';
    p.innerHTML += `<h3>Warna template</h3><label>Filter<select id="filter">${options([['none', 'Tanpa filter'], ['warm', 'Hangat'], ['cool', 'Dingin'], ['rose', 'Mawar'], ['pale', 'Pudar'], ['dark', 'Gelap']], state.filter)}</select></label>${input('filterAmount', 'Kekuatan', state.filterAmount, 'type="range" min="0" max="100"')}`;
    if (c) {
      const changed = () => { time = clamp(time, 0, totalDuration(state)); renderLists(); };
      editNumber('clipFrom', c, 'from', () => 0, () => c.to - .5, changed);
      editNumber('clipTo', c, 'to', () => c.from + .5, () => c.sourceDuration, changed);
      editNumber('clipDuration', c, 'duration', () => .5, () => 60, changed);
      bind('clipFit', 'change', (e) => { c.fit = e.target.value; refreshPreview(); });
      bind('clipMute', 'change', (e) => { c.mute = e.target.checked; renderLists(); refreshPreview(); });
      bind('moveLeft', 'click', () => reorder(selectedClip, selectedClip - 1)); bind('moveRight', 'click', () => reorder(selectedClip, selectedClip + 1));
      bind('removeClip', 'click', () => { pause(); URL.revokeObjectURL(c.url); state.clips.splice(selectedClip, 1); selectedClip = Math.max(0, selectedClip - 1); time = Math.min(time, totalDuration(state)); render(); });
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
    controls.innerHTML = `<label>Isi teks<textarea id="textValue" maxlength="2000">${escape(t.text)}</textarea></label><div class="two-col"><label>Font<select id="textFont">${options(fonts.map((f) => [f, f[0].toUpperCase() + f.slice(1)]), t.font)}</select></label>${input('textSize', 'Ukuran', t.size, 'type="number" min="8" max="400"')}</div><div class="two-col">${input('textColor', 'Warna teks', t.color, 'type="color"')}${input('textBg', 'Warna latar', t.background, 'type="color"')}</div><label class="check"><input id="textBox" type="checkbox"${t.box ? ' checked' : ''}> Gunakan latar teks</label><div class="two-col">${input('textStart', 'Mulai (detik)', t.start, 'type="number" min="0" step="0.1"')}${input('textEnd', 'Sampai (detik)', t.end, 'type="number" min="0" step="0.1"')}</div><div class="two-col">${input('textX', 'Posisi X (%)', Math.round(t.x * 100), 'type="number" min="0" max="100"')}${input('textY', 'Posisi Y (%)', Math.round(t.y * 100), 'type="number" min="0" max="100"')}</div><p class="hint">Teks ini menjadi isian text${selectedText + 1}. Agent bisa mengganti kata-katanya.</p><button id="removeText" class="danger">Hapus teks</button>`;
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
    bind('removeText', 'click', () => { state.texts.splice(selectedText, 1); selectedText = Math.max(0, selectedText - 1); renderProperties(); refreshPreview(); });
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
    bind('removeMusic', 'click', () => { pause(); URL.revokeObjectURL(m.url); musicPlayer.removeAttribute('src'); state.music = null; renderProperties(); });
  }
}

function addText() {
  if (!state.clips.length) { status('Tambahkan klip terlebih dahulu.'); return; }
  if (state.texts.length >= 100) { status('Maksimal 100 isian teks.'); return; }
  pause(); const total = totalDuration(state); time = Math.min(time, Math.max(0, total - .5));
  state.texts.push({ text: 'Tulis judulmu di sini', font: 'poppins', size: 64, color: '#FFFFFF', box: true, background: '#171717', start: time, end: Math.min(time + 3, total), x: .5, y: .25 });
  selectedText = state.texts.length - 1; tab = 'text'; renderProperties(); refreshPreview(); $('textValue')?.focus();
}
function sizeStage() {
  const [a, b] = state.ratio.split(':').map(Number); const box = $('dropZone').getBoundingClientRect();
  const h = Math.min(box.height - 2, (box.width - 2) * b / a); $('stage').style.width = `${h * a / b}px`; $('stage').style.height = `${h}px`;
}
function renderTexts() {
  const layer = $('textLayer'); layer.replaceChildren(); const width = $('stage').clientWidth;
  state.texts.forEach((t, i) => {
    if (time < t.start || time >= t.end) return;
    const n = node('div', t.text, `preview-text${tab === 'text' && selectedText === i ? ' selected' : ''}`);
    n.style.left = `${t.x * 100}%`; n.style.top = `${t.y * 100}%`; n.style.color = t.color; n.style.fontSize = `${t.size * width / 1080}px`;
    n.style.fontFamily = `WC-${t.font[0].toUpperCase() + t.font.slice(1)}`;
    n.style.background = t.box ? t.background + 'd9' : 'transparent';
    n.addEventListener('pointerdown', (e) => {
      e.preventDefault(); pause(); selectedText = i; tab = 'text'; renderProperties(); n.setPointerCapture(e.pointerId);
      const box = $('stage').getBoundingClientRect(); const startX = e.clientX, startY = e.clientY, x = t.x, y = t.y;
      const move = (event) => { t.x = clamp(x + (event.clientX - startX) / box.width, 0, 1); t.y = clamp(y + (event.clientY - startY) / box.height, 0, 1); n.style.left = `${t.x * 100}%`; n.style.top = `${t.y * 100}%`; if ($('textX')) $('textX').value = Math.round(t.x * 100); if ($('textY')) $('textY').value = Math.round(t.y * 100); };
      n.addEventListener('pointermove', move);
      const done = () => { n.removeEventListener('pointermove', move); refreshPreview(); };
      n.addEventListener('pointerup', done, { once: true }); n.addEventListener('pointercancel', done, { once: true });
    });
    layer.append(n);
  });
}
function refreshPreview() {
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
function render() { renderLists(); renderProperties(); refreshPreview(); }

async function chooseMusic(file) {
  if (!file) return;
  if (importBusy || exporting) { status('Tunggu proses yang sedang berjalan.'); return; }
  if (!state.clips.length) { status('Tambahkan klip sebelum musik.'); return; }
  if (file.size > MAX_AUDIO_BYTES) { status('Musik maksimal 32 MiB.'); return; }
  pause(); importBusy = true;
  try {
    const metadata = await readMetadata(file, 'audio');
    if (state.music) URL.revokeObjectURL(state.music.url);
    state.music = { file, ...metadata, from: 0, at: 0, duration: Math.min(metadata.sourceDuration, totalDuration(state), 600), volume: 50 };
    musicPlayer.src = metadata.url; tab = 'music'; renderProperties(); status('Musik ini akan ikut di dalam paket template.');
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
  const notes = [`${state.clips.length} bagian video, ${state.texts.length} isian teks, ${totalDuration(state).toFixed(1)} detik.`, 'Footage contoh tidak ikut. Kirim paket ini beserta footage pengganti ke agent di perangkat mana pun.', state.music ? `Musik “${state.music.file.name}” ikut utuh, termasuk bagian di luar potongan. Jika sumber musik berupa video, seluruh gambar videonya juga ikut.` : 'Paket ini tanpa musik bawaan. Suara klip mengikuti pengaturan tiap bagian.', 'Paket berisi kata-kata teksmu sebagai contoh. Periksa isinya sebelum membagikan.'];
  dialog('Bawa template ke agent', notes, { label: 'Unduh paket template', run: async () => {
    if (exporting) return; exporting = true;
    dialog('Menyiapkan paket', ['Musik dimasukkan langsung dari perangkatmu. Tidak ada unggahan ke server.']);
    try {
      const bundle = await toBundle(state, encode);
      const name = state.name.replace(/[^a-z0-9-]/gi, '-').replace(/-+/g, '-').slice(0, 60) || 'template';
      download(`${name}.wecut.json`, JSON.stringify(bundle));
      dialog('Template siap dikirim', ['Lampirkan berkas .wecut.json dan footage baru ke chat agent.', 'Contoh pesan: “Pakai template ini, ganti video dan teksnya, musik tetap. Kirim preview dulu.”', 'Agent memerlukan WeCut 0.2.0 atau lebih baru. Build 0.2.0 tersedia dari source; npm 0.1.0 belum mendukung paket ini.']);
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
document.querySelectorAll('[data-tab]').forEach((b) => b.addEventListener('click', () => { tab = b.dataset.tab; renderProperties(); refreshPreview(); }));
$('exportButton').addEventListener('click', beginExport);
$('helpButton').addEventListener('click', () => dialog('Dari browser ke perangkat agent', ['1. Pilih video/foto contoh, potong klip dan atur urutannya.', '2. Tambahkan teks dan musik. Geser teks langsung di preview.', '3. Unduh template. Kirim file itu dan footage baru lewat chat agent.', '4. Agent mengimpor paket dan membuat video di komputernya sendiri.', 'Browser ini tidak mengunggah media. Alamat localhost hanya untuk perangkatmu. Halaman editor dapat dihosting sebagai situs statis; tautan template publik membutuhkan penyimpanan file tersendiri.']));
new ResizeObserver(refreshPreview).observe($('dropZone'));
document.addEventListener('visibilitychange', () => { if (document.hidden) pause(); });
window.addEventListener('beforeunload', (e) => { if (state.clips.length) { e.preventDefault(); e.returnValue = ''; } });
video.addEventListener('loadedmetadata', refreshPreview);
render();
