# Catatan tim: WeCut Engine

Fase 1 dibangun 23 Okt 2026, fase 2 (MCP + npm) di hari yang sama. Arsitektur dan alasannya ada di dokumen "WeCut Agent Engine" (artifact). File ini ringkasan buat yang mau mencoba atau melanjutkan.

## Yang sudah jadi

- Biner `wecut` (Go, tanpa dependensi luar selain FFmpeg 6+ dengan libass), sekitar 4 MB per OS.
- Perintah: `doctor`, `probe`, `validate`, `preview`, `render`, `schema`, `list`, `example`, `mcp`, `version`. Semua keluar sebagai satu objek JSON, kode keluar tetap.
- Format proyek `wecut.project/1` + JSON Schema yang dicek otomatis supaya selalu sama dengan kode.
- Fitur render: potong, sambung, kecepatan, 8 rasio, cover dengan titik fokus + zoom, contain dengan latar warna atau blur, zoom/geser foto, 36 transisi, fade klip, 14 gaya teks dengan animasi (pop, ketik, naik, pudar, turun, mengecil), caption dari transkrip (frasa, karaoke, satu kata), logo/gambar hamparan, musik dengan loop dan ducking otomatis, 20 filter warna, 6 efek, keluaran MP4 atau GIF.
- 13 font dari app WeCut ikut tertanam di biner, ukurannya sudah dikoreksi supaya `size` sama artinya dengan di app.
- **Server MCP** (`wecut mcp`): 8 tool, preview mengirim frame sebagai gambar ke model, render mengirim progres, bisa dibatalkan. Melayani klien baru (spesifikasi 2026-07-28) dan klien lama (`initialize`, 2024-11-05 sampai 2025-11-25). Folder kerja dikunci saat server dijalankan; argumen tool tidak bisa memindahnya.
- **Paket npm**: peluncur `wecut` + satu paket biner per OS (`@wefluence/wecut-linux-x64` dan seterusnya, 6 platform). Tanpa skrip install. `tools/release` membangun semuanya ke `dist/` plus arsip GitHub Release dan `SHA256SUMS`.
- Penjaga keamanan: tanpa shell, teks tidak pernah masuk graf FFmpeg, folder kerja terkunci, whitelist demuxer (playlist HLS yang menyamar jadi .mp4 ditolak), output tidak boleh menimpa input, hasil dicek ffprobe sebelum dipindah ke nama akhir, tanpa telemetri.
- Test: unit, golden plan FFmpeg, test integrasi yang benar-benar merender, dan test protokol MCP. `go test ./...` lulus di laptop ini.
- Sudah dicoba dengan klien MCP resmi Python (1.12) dan TypeScript (1.32.1), langsung dan lewat peluncur npm. Keduanya masih memakai protokol lama; jalur protokol baru diuji lewat test sendiri.
- Agent Skill di `skill/wecut/SKILL.md`.
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

## Belum dikerjakan

- `uvx wecut` (wheel PyPI), bundel MCPB, daftar di MCP Registry (butuh paket npm sudah terbit; `mcpName` sudah ada di package.json).
- Transkripsi lokal (filter Whisper FFmpeg 8 hanya ada di sebagian build; Ubuntu dan Chocolatey tidak punya).
- Video PiP, stiker Twemoji, efek PNG dari app (vinyet, debu, VHS, dll.), normalisasi loudness, `batch`.
- Render per segmen. Sekarang tiap klip dibuka sebagai input FFmpeg terpisah, jadi batasnya 100 klip per proyek supaya RAM mesin agent aman.
- Jembatan Wefluence untuk kampanye clipping.
- Konverter proyek app WeCut (model v2) ke format ini dan sebaliknya.
- Workflow CI dan rilis sudah ditulis tapi belum pernah jalan.
- Belum dicoba oleh agent sungguhan (Grok Bot, Manus) dan belum diuji di Linux/macOS asli; biner untuk keduanya baru dibangun silang dari Windows.

## Perlu keputusan / dicek sebelum rilis publik

- Lisensi Apache-2.0 (sesuai saran dokumen). Lisensi tiap font dicek ulang di Google Fonts (`internal/assets/fonts/LICENSES.md`).
- Belum di-commit dan belum ada repo git.
