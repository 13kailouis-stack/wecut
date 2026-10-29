export const MAX_AUDIO_BYTES = 32 * 1024 * 1024;
export const MAX_PACKAGE_BYTES = 48 * 1024 * 1024;
export const fonts = ['roboto', 'poppins', 'montserrat'];
// Every font the engine ships; a template may use any of them. The Studio
// preview draws the three above and stands in Poppins for the rest.
export const engineFonts = ['roboto', 'montserrat', 'poppins', 'oswald', 'anton', 'bangers', 'archivo-black', 'pacifico', 'lobster', 'permanent-marker', 'playfair', 'dancing-script', 'caveat'];
export const filters = { none: null, warm: ['#FF9A3C', .2], cool: ['#3C8CFF', .18], rose: ['#FF5C8A', .17], pale: ['#FFFFFF', .22], dark: ['#000000', .28] };
export const duration = (clip) => clip.kind === 'image' ? clip.duration : clip.to - clip.from;
export const totalDuration = (state) => state.clips.reduce((n, clip) => n + duration(clip), 0);
export const round = (n) => Math.round(n * 1000) / 1000;
export function locate(state, time) {
  let offset = 0;
  for (let i = 0; i < state.clips.length; i++) {
    const clip = state.clips[i];
    if (time < offset + duration(clip) || i === state.clips.length - 1) return { clip, index: i, offset, local: Math.max(0, time - offset) };
    offset += duration(clip);
  }
  return null;
}
export function initialState() { return { name: 'Template saya', ratio: '9:16', res: 1080, fps: 30, clips: [], texts: [], music: null, filter: 'none', filterAmount: 35 }; }
export function validate(state) {
  const errors = [];
  const total = totalDuration(state);
  if (!state.clips.length || state.clips.length > 100) errors.push('Tambahkan 1–100 klip terlebih dahulu.');
  if (!Number.isFinite(total) || total < 3 || total > 600) errors.push('Durasi template harus 3–600 detik.');
  if (!state.name.trim() || state.name.length > 120) errors.push('Isi nama template, maksimal 120 karakter.');
  if (!['9:16', '16:9', '1:1', '4:5'].includes(state.ratio)) errors.push('Rasio tidak didukung.');
  if (![480, 720, 1080].includes(state.res) || ![24, 25, 30, 60].includes(state.fps)) errors.push('Resolusi atau fps tidak didukung.');
  const fontList = state.template ? engineFonts : fonts;
  state.clips.forEach((c, i) => {
    const d = duration(c);
    if (!['image', 'video'].includes(c.kind) || !Number.isFinite(d) || d < .5 || d > 600 || !['cover', 'contain'].includes(c.fit)) errors.push(`Periksa durasi dan bingkai klip ${i + 1}.`);
    if (c.kind === 'image' && d > 60 && !c.placeholder) errors.push(`Foto ${i + 1} maksimal 60 detik.`);
    if (c.kind === 'video' && (!Number.isFinite(c.from) || c.from < 0 || c.to > c.sourceDuration + .01)) errors.push(`Potongan klip ${i + 1} melewati berkas sumber.`);
  });
  if (!(state.filter in filters) || !Number.isFinite(state.filterAmount) || state.filterAmount < 0 || state.filterAmount > 100) errors.push('Filter tidak valid.');
  if (state.texts.length > 100) errors.push('Maksimal 100 isian teks di Studio.');
  state.texts.forEach((t, i) => {
    if (!t.text.trim() || t.text.length > 2000 || !fontList.includes(t.font) || !Number.isFinite(t.size) || t.size < 8 || t.size > 400) errors.push(`Periksa isi atau font teks ${i + 1}.`);
    if (![t.start, t.end, t.x, t.y].every(Number.isFinite) || t.start < 0 || t.end <= t.start || t.end > total + .01 || t.x < 0 || t.x > 1 || t.y < 0 || t.y > 1) errors.push(`Periksa waktu dan posisi teks ${i + 1}.`);
    if (!/^#[0-9a-f]{6}$/i.test(t.color) || !/^#[0-9a-f]{6}$/i.test(t.background)) errors.push(`Warna teks ${i + 1} tidak valid.`);
  });
  const m = state.music;
  if (m) {
    if (!m.file || m.file.size <= 0 || m.file.size > MAX_AUDIO_BYTES || !/\.(mp3|m4a|wav|aac|ogg|flac|mp4|webm|mov)$/i.test(m.file.name)) errors.push('Gunakan musik MP3/M4A/WAV/AAC/OGG/FLAC atau video MP4/WEBM/MOV, maksimal 32 MiB.');
    if (![m.from, m.at, m.duration, m.volume].every(Number.isFinite) || m.from < 0 || m.at < 0 || m.at >= total || m.duration <= 0 || m.duration > 600 || m.from + m.duration > m.sourceDuration + .01 || m.volume < 0 || m.volume > 100) errors.push('Periksa potongan, posisi dan volume musik.');
  }
  return [...new Set(errors)];
}

export function toRecipe(state) {
  const errors = validate(state);
  if (errors.length) throw new Error(errors.join('\n'));
  if (state.template) return templateRecipe(state);
  const recipe = {
    recipe: '1', name: state.name.trim(), maxDuration: round(totalDuration(state)),
    output: { ratio: state.ratio, res: state.res, fps: state.fps, background: '#000000' },
    sections: state.clips.map((c, i) => ({ role: `clip${i + 1}`, about: `Ganti klip ${i + 1}`, duration: round(duration(c)), fit: c.fit, mute: !!c.mute, ...(c.kind === 'image' ? { shot: round(c.duration) } : {}) })),
    texts: state.texts.map((t, i) => ({ slot: `text${i + 1}`, text: t.text, start: round(t.start), end: round(t.end), x: round(t.x), y: round(t.y), font: t.font, size: t.size, color: t.color, background: t.box ? t.background : 'none', backgroundOpacity: 85, stroke: 'none', shadow: false, align: 'center' })),
  };
  if (state.filter !== 'none') recipe.filters = [{ name: state.filter, amount: state.filterAmount }];
  if (state.music) {
    const m = state.music;
    const extension = m.file.name.split('.').pop().toLowerCase();
    recipe.music = { src: `assets/music.${extension}`, from: round(m.from), at: round(m.at), duration: round(m.duration), volume: m.volume, loop: false };
  }
  return recipe;
}

export async function toBundle(state, encode) {
  const recipe = toRecipe(state);
  const assets = [];
  if (state.music) {
    const bytes = new Uint8Array(await state.music.file.arrayBuffer());
    if (bytes.length > MAX_AUDIO_BYTES) throw new Error('Musik melebihi 32 MiB.');
    assets.push({ name: recipe.music.src.slice('assets/'.length), data: encode(bytes) });
  }
  const bundle = { bundle: 'wecut.bundle/1', recipe, assets };
  if (new TextEncoder().encode(JSON.stringify(bundle)).length > MAX_PACKAGE_BYTES) throw new Error('Paket melebihi 48 MiB.');
  return bundle;
}

// Templates (cloned or imported) keep everything the Studio does not edit:
// exact shots, transitions, zoom, captions, logo slot, J-cut sound, text
// looks. Each shot is one clip on the timeline.

// templateShots lists a recipe's media slots in order.
export function templateShots(recipe) {
  const out = [];
  const sections = recipe.sections || [];
  sections.forEach((section, si) => {
    const shots = section.shots?.length ? section.shots : [{ dur: section.duration || section.shot || 3 }];
    shots.forEach((shot, k) => {
      // How long this shot overlaps the next one (its transition).
      const t = k < shots.length - 1 ? shot.join : si < sections.length - 1 ? section.transitionOut : null;
      out.push({ si, k, shot, section, join: t ? (t.duration || 0.5) : 0 });
    });
  });
  return out;
}

// stateFromTemplate makes an editor state from a recipe. media(i, slot)
// gives each slot's clip: the original footage after a clone, or a
// numbered stand-in after an import.
export function stateFromTemplate(recipe, media, useText = () => true) {
  const s = initialState();
  s.template = structuredClone(recipe);
  s.name = recipe.name || 'Template';
  s.ratio = ['9:16', '16:9', '1:1', '4:5'].includes(recipe.output?.ratio) ? recipe.output.ratio : '9:16';
  s.res = [480, 720, 1080].includes(recipe.output?.res) ? recipe.output.res : 1080;
  s.fps = [24, 25, 30, 60].includes(recipe.output?.fps) ? recipe.output.fps : 30;
  s.clips = templateShots(recipe).map((x, i) => {
    const c = { fit: x.shot.fit || x.section.fit || 'cover', mute: !!(x.section.mute || x.section.audio === 'next'), slot: i, about: x.shot.about || x.section.about || '', ...media(i, x) };
    // The timeline has no overlaps: a slot shows until the next one starts,
    // and gets its transition back on export.
    c.joinDur = Math.min(x.join, duration(c) - 0.1);
    if (c.joinDur > 0) { if (c.kind === 'image') c.duration = round(c.duration - c.joinDur); else c.to = round(c.to - c.joinDur); }
    else c.joinDur = 0;
    return c;
  });
  const total = s.clips.reduce((n, c) => n + duration(c), 0);
  s.texts = (recipe.texts || []).filter(useText).map((t) => textFromSlot(t, total));
  return s;
}

export function textFromSlot(t, total) {
  const start = t.fromEnd ? Math.max(0, total - t.fromEnd) : t.start || 0;
  const box = !!t.background && t.background !== 'none';
  return {
    text: t.text || 'Teks', font: t.font || 'poppins', size: t.size || 64, color: t.color || '#FFFFFF',
    box, background: box ? t.background : '#000000', start: round(start), end: round(Math.min(total, t.end ?? total)),
    x: t.x ?? 0.5, y: t.y ?? 0.5, align: t.align || 'center', italic: !!t.italic, uppercase: !!t.uppercase, extra: t,
  };
}

// templateRecipe writes the template back with the editor's changes:
// slot lengths and framing, words, timing, position, colours, music.
export function templateRecipe(state) {
  const r = structuredClone(state.template);
  const flat = templateShots(r);
  if (flat.length === state.clips.length && flat.every((x) => x.section.shots?.length)) {
    flat.forEach((x, i) => {
      const c = state.clips[i];
      x.shot.dur = round(duration(c) + (c.joinDur || 0));
      if (c.fit !== (x.section.fit || 'cover')) x.shot.fit = c.fit; else delete x.shot.fit;
    });
  } else {
    // Clips were added, split or removed: one section, a shot per clip.
    r.sections = [{ role: 'body', fit: 'cover', shots: state.clips.map((c) => ({ dur: round(duration(c) + (c.joinDur || 0)), ...(c.fit !== 'cover' ? { fit: c.fit } : {}) })) }];
  }
  const taken = new Set();
  r.texts = state.texts.map((t, i) => {
    const base = t.extra ? { ...t.extra } : { stroke: 'none', shadow: false, align: 'center', backgroundOpacity: 85 };
    delete base.fromEnd;
    let slot = base.slot || `text${i + 1}`;
    for (let n = 2; taken.has(slot); n++) slot = `${base.slot || 'text'}${n}`;
    taken.add(slot);
    return { ...base, slot, text: t.text, start: round(t.start), end: round(t.end), x: round(t.x), y: round(t.y), font: t.font, size: t.size, color: t.color, background: t.box ? t.background : 'none' };
  });
  if (!r.texts.length) delete r.texts;
  r.maxDuration = Math.max(3, round(totalDuration(state) + 0.05));
  if (state.music) {
    const m = state.music, extension = m.file.name.split('.').pop().toLowerCase();
    r.music = { ...(r.music || {}), src: `assets/music.${extension}`, from: round(m.from), at: round(m.at), duration: round(m.duration), volume: m.volume };
  } else if (r.music) delete r.music.src;
  if (state.filter !== 'none') r.filters = [{ name: state.filter, amount: state.filterAmount }];
  return r;
}
