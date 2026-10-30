// Clone a video into a template in the browser. The page decodes frames
// with a <video> element and the same detectors `wecut clone` uses run as
// WebAssembly (engine/clone.wasm, built by `go run ./tools/wasm`). Nothing
// leaves the device.

const LOW_FPS = 15, HIGH_EVERY = 3; // 15 timing frames a second, every 3rd also sharp (5 a second)
const MAX_SECONDS = 180;
let ready;

function loadEngine() {
  if (ready) return ready;
  ready = (async () => {
    await new Promise((resolve, reject) => {
      const s = document.createElement('script'); s.src = 'engine/wasm_exec.js'; s.onload = resolve; s.onerror = () => reject(new Error('engine/wasm_exec.js tidak ditemukan. Jalankan `go run ./tools/wasm` dari repo WeCut.')); document.head.append(s);
    });
    const go = new globalThis.Go();
    const res = await fetch('engine/clone.wasm');
    if (!res.ok) throw new Error('engine/clone.wasm tidak ditemukan. Jalankan `go run ./tools/wasm` dari repo WeCut.');
    const { instance } = await WebAssembly.instantiate(await res.arrayBuffer(), go.importObject);
    go.run(instance);
    return globalThis.wecutClone;
  })();
  return ready;
}

const call = (fn, ...args) => {
  const out = fn(...args);
  if (typeof out !== 'string') return out;
  const v = JSON.parse(out);
  if (v && v.error) throw new Error(v.error);
  return v;
};

// seek resolves once the frame at t is shown. Asking for the time the
// video is already at fires no event, and a stuck decoder should not hang
// the page, so both end the wait too.
function seek(video, t) {
  if (Math.abs(video.currentTime - t) < 1e-3) return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => { clearTimeout(timer); video.removeEventListener('seeked', done); resolve(); };
    const timer = setTimeout(done, 4000);
    video.addEventListener('seeked', done);
    video.currentTime = t;
  });
}

function gray(ctx, w, h) {
  const rgba = ctx.getImageData(0, 0, w, h).data, out = new Uint8Array(w * h);
  for (let i = 0, j = 0; j < out.length; i += 4, j++) out[j] = (0.2126 * rgba[i] + 0.7152 * rgba[i + 1] + 0.0722 * rgba[i + 2] + 0.5) | 0;
  return out;
}

function rgb(ctx, w, h) {
  const rgba = ctx.getImageData(0, 0, w, h).data, out = new Uint8Array(w * h * 3);
  for (let i = 0, j = 0; j < out.length; i += 4, j += 3) { out[j] = rgba[i]; out[j + 1] = rgba[i + 1]; out[j + 2] = rgba[i + 2]; }
  return out;
}

async function audioOf(file) {
  try {
    const ctx = new OfflineAudioContext(1, 1, 11025);
    const buf = await ctx.decodeAudioData(await file.arrayBuffer());
    const n = Math.min(buf.length, MAX_SECONDS * 11025), out = new Int16Array(n);
    for (let c = 0; c < buf.numberOfChannels; c++) {
      const d = buf.getChannelData(c);
      for (let i = 0; i < n; i++) out[i] += Math.max(-1, Math.min(1, d[i] / buf.numberOfChannels)) * 32767;
    }
    return out;
  } catch { return null; }
}

// cloneVideo analyses a local video file and returns the template
// (recipe), its media slots with thumbnails, and hints.
export async function cloneVideo(file, progress = () => {}, debug = false) {
  const engine = await loadEngine();
  const url = URL.createObjectURL(file);
  const video = document.createElement('video');
  video.muted = true; video.playsInline = true; video.preload = 'auto'; video.src = url;
  try {
    await new Promise((resolve, reject) => { video.onloadeddata = resolve; video.onerror = () => reject(new Error('Browser tidak bisa membuka video ini. Coba MP4 H.264.')); });
    const W = video.videoWidth, H = video.videoHeight, duration = Math.min(video.duration, MAX_SECONDS);
    if (!W || !H || !Number.isFinite(duration)) throw new Error('Video tidak punya gambar yang terbaca.');
    const sizes = call(engine.start, JSON.stringify({ W, H, Duration: duration, LowFPS: LOW_FPS, HighFPS: LOW_FPS / HIGH_EVERY, Debug: debug }));
    const canvas = (w, h) => { const c = document.createElement('canvas'); c.width = w; c.height = h; return c.getContext('2d', { willReadFrequently: true }); };
    const low = canvas(sizes.lowW, sizes.lowH), high = canvas(sizes.highW, sizes.highH);
    const frames = Math.floor(duration * LOW_FPS);
    for (let i = 0; i < frames; i++) {
      await seek(video, (i + 0.5) / LOW_FPS);
      low.drawImage(video, 0, 0, sizes.lowW, sizes.lowH);
      engine.addLow(gray(low, sizes.lowW, sizes.lowH));
      if (i % HIGH_EVERY === 0) { high.drawImage(video, 0, 0, sizes.highW, sizes.highH); engine.addHigh(gray(high, sizes.highW, sizes.highH)); }
      if (i % 15 === 0) progress(0.8 * i / frames, 'Membaca potongan dan teks…');
    }
    progress(0.82, 'Membaca suara…');
    const pcm = await audioOf(file);
    if (pcm) engine.setAudio(pcm, 11025);
    const plan = call(engine.analyze);
    progress(0.88, 'Mengukur warna dan ukuran teks…');
    const k = Math.min(1, 720 / Math.min(W, H)), sw = Math.round(W * k / 2) * 2, sh = Math.round(H * k / 2) * 2;
    const still = canvas(sw, sh), stills = [];
    for (const t of plan.stills) { await seek(video, t); still.drawImage(video, 0, 0, sw, sh); stills.push(rgb(still, sw, sh)); }
    call(engine.measure, sw, sh, stills);
    const music = pcm && (!plan.speech || plan.hook_music) ? `assets/music.${(file.name.split('.').pop() || 'mp4').toLowerCase()}` : '';
    const result = call(engine.build, JSON.stringify({ Name: file.name.replace(/\.[^.]+$/, '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'template', Source: file.name, Music: music, Tool: 'WeCut Studio' }));
    progress(0.94, 'Membuat gambar slot…');
    const thumb = canvas(90, Math.round(90 * H / W));
    for (const s of result.slots) {
      await seek(video, s.ref); thumb.drawImage(video, 0, 0, thumb.canvas.width, thumb.canvas.height);
      s.thumbnail = thumb.canvas.toDataURL('image/jpeg', 0.7);
    }
    progress(1, 'Selesai');
    return { ...result, file, music, debug: debug ? call(engine.debug) : undefined };
  } finally {
    URL.revokeObjectURL(url);
  }
}

// templateBundle packs the cloned recipe (with any edited words) and, when
// the template keeps the original's sound, the file itself as its music.
export async function templateBundle(clone, encode, maxBytes) {
  const recipe = structuredClone(clone.recipe);
  if (recipe.texts) {
    recipe.texts = recipe.texts.filter((t) => t._use !== false).map(({ _use, ...t }) => t);
    if (!recipe.texts.length) delete recipe.texts;
  }
  const assets = [];
  if (recipe.music && clone.music) {
    if (clone.file.size > maxBytes) delete recipe.music;
    else assets.push({ name: clone.music.slice('assets/'.length), data: encode(new Uint8Array(await clone.file.arrayBuffer())) });
  }
  return { bundle: 'wecut.bundle/1', recipe, assets };
}
