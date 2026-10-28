# Catatan tim: WeCut Engine

Fase 1 dibangun 23 Okt 2026, fase 2 (MCP + npm) di hari yang sama. Arsitektur dan alasannya ada di dokumen "WeCut Agent Engine" (artifact). File ini ringkasan buat yang mau mencoba atau melanjutkan.

## Yang sudah jadi

- Biner `wecut` (Go, tanpa dependensi luar selain FFmpeg 6+ dengan libass), sekitar 4 MB per OS.
- Perintah: `guide`, `doctor`, `probe`, `analyze`, `recipe`, `validate`, `preview`, `render`, `schema`, `list`, `example`, `mcp`, `version`. Semua keluar sebagai satu objek JSON (kecuali `guide` dan `schema` yang mencetak dokumen), kode keluar tetap.
- Format proyek `wecut.project/1` + JSON Schema yang dicek otomatis supaya selalu sama dengan kode.
- Fitur render: potong, sambung, kecepatan, 8 rasio, cover dengan titik fokus + zoom, contain dengan latar warna atau blur, zoom/geser foto, 36 transisi, fade klip, 14 gaya teks dengan animasi (pop, ketik, naik, pudar, turun, mengecil), caption dari transkrip (frasa, karaoke, satu kata), logo/gambar hamparan, musik dengan loop dan ducking otomatis, 20 filter warna, 6 efek, keluaran MP4 atau GIF.
- 13 font dari app WeCut ikut tertanam di biner, ukurannya sudah dikoreksi supaya `size` sama artinya dengan di app.
- **Server MCP** (`wecut mcp`): 13 tool dan 4 prompt (muncul sebagai slash command: `clips-from-video`, `copy-viral-format`, `add-captions`, `save-template`), preview mengirim frame sebagai gambar ke model, render mengirim progres, bisa dibatalkan. Melayani klien baru (spesifikasi 2026-07-28) dan klien lama (`initialize`, 2024-11-05 sampai 2025-11-25). Folder kerja dikunci saat server dijalankan; argumen tool tidak bisa memindahnya.
- **Paket npm**: peluncur `wecut` + satu paket biner per OS (`@wefluence/wecut-linux-x64` dan seterusnya, 6 platform). Tanpa skrip install. `tools/release` membangun semuanya ke `dist/` plus arsip GitHub Release dan `SHA256SUMS`.
- Penjaga keamanan: tanpa shell, teks tidak pernah masuk graf FFmpeg, folder kerja terkunci, whitelist demuxer (playlist HLS yang menyamar jadi .mp4 ditolak), output tidak boleh menimpa input, hasil dicek ffprobe sebelum dipindah ke nama akhir, tanpa telemetri.
- Test: unit, golden plan FFmpeg, test integrasi yang benar-benar merender, dan test protokol MCP. `go test ./...` lulus di laptop ini.
- Sudah dicoba dengan klien MCP resmi Python (1.12) dan TypeScript (1.32.1), langsung dan lewat peluncur npm. Keduanya masih memakai protokol lama; jalur protokol baru diuji lewat test sendiri.
- Agent Skill di `skills/wecut/SKILL.md`; `wecut guide` mencetak isinya untuk agent yang cuma punya terminal (Grok, Manus).
- **Analisis + template (23 Okt, fase 3 awal)**: `wecut analyze` mengukur video yang berhasil (titik potong dengan kilat/strobo dihitung sekali, tempo, ketukan, jeda, outro TikTok yang sunyi) dan membuat lembar frame untuk "mata" agent. Format resep `wecut.recipe/1`: bagian (hook, body) yang diisi klip sesuai peran, slot teks dengan kata berwarna (emphasis), gaya caption, musik bawaan seperti template CapCut (bisa diganti atau "none"), efek di tiap potongan. `wecut recipe apply` mengubah resep + bahan baru jadi proyek; `wecut recipe from-project` menyimpan proyek jadi template sendiri. 6 resep bawaan dirancang dari 5 klip viral Indonesia yang dikirim pemilik (strukturnya saja; klipnya tidak masuk repo). Gaya teks baru: headline, label, quote; caption word dan phrase; huruf miring.

## Cara coba

```
go build -o bin/wecut.exe ./cmd/wecut
bin/wecut.exe doctor
bin/wecut.exe example podcast-clip
```

Taruh video di folder kerja, tulis proyek, lalu `validate`, `preview`, `render`. Sebagai server MCP di Claude Code: `claude mcp add wecut -- "D:\0000 claude code\wecut\bin\wecut.exe" mcp`.

## Kecepatan di laptop ini (i5-1035G7, sedang dipakai aplikasi lain)

- 43 detik klip podcast + karaoke + musik: 27,6 detik.
- Slideshow 3 foto dengan zoom + 2 transisi: 9,4 detik untuk video 7,9 detik.
- Preview frame: 0,3 sampai 4 detik.

## Langkah rilis pertama (dijalankan pemilik akun)

1. Repo GitHub: https://github.com/13kailouis-stack/wecut (dibuat 23 Okt 2026, publik). Path modul sudah disesuaikan.
2. Organisasi npm: `wefluence` (dibuat 23 Okt 2026). Paket biner bernama `@wefluence/wecut-<os>-<arch>` dan otomatis masuk organisasi saat pertama diterbitkan; halaman "Add Package" tidak perlu diisi. Peluncur `wecut` sendiri tanpa scope.
3. Publikasi pertama harus manual, karena trusted publishing baru bisa diatur setelah paketnya ada. Boleh dari Windows: `tools/release` membuat tarball sendiri dengan izin eksekusi yang benar.
   ```
   go run ./tools/release -version 0.1.0
   npm login
   npm publish dist/npm/tarballs/wefluence-wecut-linux-x64-0.1.0.tgz --access public
   (ulangi untuk kelima tarball wefluence-wecut-* lainnya)
   npm publish dist/npm/tarballs/wecut-0.1.0.tgz --access public
   ```
4. Di npmjs.com, atur trusted publisher ke `.github/workflows/release.yml` untuk ketujuh paket. Sesudah itu rilis berikutnya cukup push tag `v0.1.1`, tanpa token npm.
5. Sebelum rilis otomatis pertama, kunci action di workflow ke commit SHA.

## Rilis 0.3.0 (kloning video jadi template), disiapkan 28 Okt 2026

Paket sudah dibangun di `dist/` (jangan dihapus sebelum publish selesai): tujuh tarball npm di `dist/npm/tarballs/`, arsip GitHub di `dist/release/` + `SHA256SUMS`. Diuji: tarball win32-x64 + peluncur dipasang di folder kosong, `npx wecut version` = 0.3.0, `wecut clone` jalan, MCP berisi 16 alat termasuk `wecut_clone`, `npm publish --dry-run` diterima. CI commit `b118c26` hijau di Linux, macOS, Windows. Di npm baru ada 0.1.0 (0.2.0 tidak pernah terbit), jadi 0.3.0 langsung menggantikannya.

Jalankan dari folder repo di terminal sendiri (butuh konfirmasi 2FA npm di browser):

```
npm login
npm publish dist/npm/tarballs/wefluence-wecut-linux-x64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wefluence-wecut-linux-arm64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wefluence-wecut-darwin-x64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wefluence-wecut-darwin-arm64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wefluence-wecut-win32-x64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wefluence-wecut-win32-arm64-0.3.0.tgz --access public
npm publish dist/npm/tarballs/wecut-0.3.0.tgz --access public
```

Paket platform dulu, peluncur `wecut` terakhir. npm memakai "staged publishing": beberapa menit pertama `npm view wecut` bisa masih 0.1.0 atau 404, itu normal. Cek: `npx -y wecut@0.3.0 version`.

Lalu MCP Registry (`server.json` sudah 0.3.0; harus setelah npm terbit karena registry memeriksa paket npm-nya):

```
mcp-publisher login github
mcp-publisher publish
```

Cek: `https://registry.modelcontextprotocol.io/v0.1/servers/io.github.13kailouis-stack%2Fwecut/versions/latest`.

Jangan push tag `v0.3.0` setelah publish manual: tag itu menjalankan `release.yml`, yang akan mencoba menerbitkan versi yang sama lagi dan gagal. Push tag hanya dipakai kalau trusted publishing sudah diatur di npm untuk ketujuh paket dan langkah manual di atas TIDAK dijalankan.

## Supaya agent menemukan dan memakai WeCut

File di repo (dibaca agent, registry dan mesin pencari AI):

- `llms.txt`: peta repo untuk model, menunjuk ke file mentah (test mengecek semua tautannya ada).
- `AGENTS.md`: untuk agent yang mengubah kode WeCut. `GEMINI.md` + `gemini-extension.json`: ekstensi Gemini CLI.
- `server.json`: entri MCP Registry `io.github.13kailouis-stack/wecut`. Harus sama dengan `mcpName` di package.json npm; test mengecek nama dan versinya.
- `.claude-plugin/`: plugin + marketplace Claude Code (server MCP + skill sekaligus). `glama.json`: klaim di Glama.
- README: cara pasang per agent (Claude Code, klien MCP umum, Cursor, VS Code, Codex, Gemini, agent terminal, skill), FAQ bentuk tanya-jawab, tabel perbandingan.

Yang hanya bisa dilakukan pemilik akun, setelah npm terbit:

1. MCP Registry: `mcp-publisher login github` lalu `mcp-publisher publish` dari folder repo. Beberapa katalog lain (misalnya PulseMCP) menyalin data dari registry ini.
2. Topik repo GitHub (mcp, mcp-server, ai-agent, video-editing, ffmpeg, captions, tiktok, shorts, claude-code-plugin, agent-skills), deskripsi dan homepage.
3. Daftar manual: Smithery, Glama, mcp.so, mcpservers.org, PulseMCP, Cursor Directory, Cline MCP Marketplace, awesome-mcp-servers (PR), Docker MCP Catalog, skills.sh, direktori plugin Claude.
4. Konten yang dibaca mesin jawaban (Grok membaca X, Perplexity dan ChatGPT membaca web): artikel blog Wefluence, thread X dengan video demo, Show HN, Reddit (r/LocalLLaMA, r/ClaudeAI, r/mcp), dev.to, YouTube.
5. Masuk ke data latih model butuh berbulan-bulan; registry, marketplace dan pencarian web bekerja dalam hitungan hari.

## Belum dikerjakan

- `uvx wecut` (wheel PyPI), bundel MCPB. Daftar di MCP Registry sudah siap (`server.json`), tinggal diterbitkan setelah npm.
- Transkripsi lokal (filter Whisper FFmpeg 8 hanya ada di sebagian build; Ubuntu dan Chocolatey tidak punya).
- Video PiP, stiker Twemoji, efek PNG dari app (vinyet, debu, VHS, dll.), normalisasi loudness, `batch`.
- Render per segmen. Sekarang tiap klip dibuka sebagai input FFmpeg terpisah, jadi batasnya 100 klip per proyek supaya RAM mesin agent aman.
- Jembatan Wefluence untuk kampanye clipping.
- Konverter proyek app WeCut (model v2) ke format ini dan sebaliknya.
- Workflow rilis sudah ditulis tapi belum pernah jalan. CI hijau di Linux, macOS dan Windows.
- Belum dicoba oleh agent sungguhan (Grok Bot, Manus).

## Perlu keputusan / dicek sebelum rilis publik

- Lisensi Apache-2.0 (sesuai saran dokumen). Lisensi tiap font dicek ulang di Google Fonts (`internal/assets/fonts/LICENSES.md`).
