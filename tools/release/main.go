// Command release builds everything a WeCut release ships, into dist/:
//
//	dist/bin/<os>-<arch>/wecut[.exe]        one static binary per platform
//	dist/npm/wecut/                         the npm launcher package (`npx wecut`)
//	dist/npm/cli-<os>-<arch>/               one npm package per platform binary
//	dist/release/wecut_<v>_<os>_<arch>.*    archives for GitHub Releases
//	dist/release/SHA256SUMS
//
// Usage: go run ./tools/release -version 0.1.0 [-scope @wecut]
//
// Nothing is published. Publishing happens in CI (.github/workflows/release.yml)
// or by hand with `npm publish` in each dist/npm folder, platforms first.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type target struct{ goos, goarch, npmOS, npmCPU string }

var targets = []target{
	{"linux", "amd64", "linux", "x64"},
	{"linux", "arm64", "linux", "arm64"},
	{"darwin", "amd64", "darwin", "x64"},
	{"darwin", "arm64", "darwin", "arm64"},
	{"windows", "amd64", "win32", "x64"},
	{"windows", "arm64", "win32", "arm64"},
}

const (
	repo        = "https://github.com/13kailouis-stack/wecut"
	mcpName     = "io.github.13kailouis-stack/wecut"
	description = "Video editing engine for AI agents: cut, caption and render video locally with FFmpeg. CLI and MCP server."
)

func main() {
	version := flag.String("version", "", "release version, e.g. 0.1.0")
	scope := flag.String("scope", "@wecut", "npm scope of the platform packages")
	only := flag.String("only", "", "build only these targets, e.g. win32-x64,linux-x64 (for local tests)")
	flag.Parse()
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`).MatchString(*version) {
		die("give -version as x.y.z")
	}
	if !strings.HasPrefix(*scope, "@") {
		die("-scope must start with @")
	}
	must(os.RemoveAll("dist"))

	var sums []string
	optional := map[string]string{}
	for _, t := range targets {
		key := t.npmOS + "-" + t.npmCPU
		if *only != "" && !strings.Contains(","+*only+",", ","+key+",") {
			continue
		}
		exe := "wecut"
		if t.goos == "windows" {
			exe += ".exe"
		}
		bin := filepath.Join("dist", "bin", key, exe)
		build(t, *version, bin)

		// npm platform package
		pkgName := *scope + "/cli-" + key
		dir := filepath.Join("dist", "npm", "cli-"+key)
		copyFile(bin, filepath.Join(dir, "bin", exe), 0o755)
		copyFile("LICENSE", filepath.Join(dir, "LICENSE"), 0o644)
		writeJSON(filepath.Join(dir, "package.json"), map[string]any{
			"name":            pkgName,
			"version":         *version,
			"description":     fmt.Sprintf("The %s binary of wecut. Install `wecut` instead.", key),
			"license":         "Apache-2.0",
			"repository":      map[string]string{"type": "git", "url": "git+" + repo + ".git"},
			"os":              []string{t.npmOS},
			"cpu":             []string{t.npmCPU},
			"files":           []string{"bin/" + exe},
			"preferUnplugged": true,
		})
		must(os.WriteFile(filepath.Join(dir, "README.md"), []byte(fmt.Sprintf("# %s\n\nThe %s binary used by [`wecut`](https://www.npmjs.com/package/wecut). Install `wecut`, not this package.\n", pkgName, key)), 0o644))
		optional[pkgName] = *version

		// GitHub release archive
		archive := filepath.Join("dist", "release", fmt.Sprintf("wecut_%s_%s_%s", *version, t.goos, t.goarch))
		if t.goos == "windows" {
			archive += ".zip"
			zipFiles(archive, map[string]string{exe: bin, "LICENSE": "LICENSE", "README.md": "README.md"})
		} else {
			archive += ".tar.gz"
			tarFiles(archive, map[string]string{exe: bin, "LICENSE": "LICENSE", "README.md": "README.md"})
		}
		sums = append(sums, sha(archive)+"  "+filepath.Base(archive))
	}
	if len(optional) == 0 {
		die("no targets matched -only")
	}

	// npm launcher package
	main := filepath.Join("dist", "npm", "wecut")
	copyFile(filepath.Join("npm", "wecut", "bin", "wecut.js"), filepath.Join(main, "bin", "wecut.js"), 0o755)
	copyFile(filepath.Join("npm", "wecut", "README.md"), filepath.Join(main, "README.md"), 0o644)
	copyFile("LICENSE", filepath.Join(main, "LICENSE"), 0o644)
	launcher, err := os.ReadFile(filepath.Join(main, "bin", "wecut.js"))
	must(err)
	if *scope != "@wecut" {
		launcher = []byte(strings.Replace(string(launcher), "const SCOPE = '@wecut';", "const SCOPE = '"+*scope+"';", 1))
		must(os.WriteFile(filepath.Join(main, "bin", "wecut.js"), launcher, 0o755))
	}
	writeJSON(filepath.Join(main, "package.json"), map[string]any{
		"name":                 "wecut",
		"version":              *version,
		"description":          description,
		"keywords":             []string{"mcp", "mcp-server", "ai-agent", "video", "video-editing", "ffmpeg", "captions", "subtitles", "karaoke", "shorts", "reels", "tiktok"},
		"license":              "Apache-2.0",
		"homepage":             repo,
		"repository":           map[string]string{"type": "git", "url": "git+" + repo + ".git"},
		"bin":                  map[string]string{"wecut": "bin/wecut.js"},
		"files":                []string{"bin/wecut.js"},
		"engines":              map[string]string{"node": ">=16"},
		"optionalDependencies": optional,
		"mcpName":              mcpName,
	})

	sort.Strings(sums)
	must(os.WriteFile(filepath.Join("dist", "release", "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644))
	fmt.Printf("built wecut %s for %d platforms into dist/\n", *version, len(optional))
}

func build(t target, version, out string) {
	cmd := exec.Command("go", "build", "-trimpath",
		"-ldflags", "-s -w -X github.com/13kailouis-stack/wecut.Version="+version,
		"-o", out, "./cmd/wecut")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.goos, "GOARCH="+t.goarch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		die(fmt.Sprintf("build %s/%s: %v", t.goos, t.goarch, err))
	}
}

func copyFile(from, to string, mode os.FileMode) {
	must(os.MkdirAll(filepath.Dir(to), 0o755))
	data, err := os.ReadFile(from)
	must(err)
	must(os.WriteFile(to, data, mode))
	must(os.Chmod(to, mode))
}

func writeJSON(path string, v any) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep ">=16" readable in package.json
	enc.SetIndent("", "  ")
	must(enc.Encode(v))
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	must(os.WriteFile(path, []byte(b.String()), 0o644))
}

// tarFiles writes a .tar.gz; the binary gets mode 0755 whatever the host OS.
func tarFiles(out string, files map[string]string) {
	must(os.MkdirAll(filepath.Dir(out), 0o755))
	f, err := os.Create(out)
	must(err)
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range sortedKeys(files) {
		data, err := os.ReadFile(files[name])
		must(err)
		mode := int64(0o644)
		if name == "wecut" {
			mode = 0o755
		}
		must(tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}))
		_, err = tw.Write(data)
		must(err)
	}
	must(tw.Close())
	must(gz.Close())
}

func zipFiles(out string, files map[string]string) {
	must(os.MkdirAll(filepath.Dir(out), 0o755))
	f, err := os.Create(out)
	must(err)
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, name := range sortedKeys(files) {
		w, err := zw.Create(name)
		must(err)
		src, err := os.Open(files[name])
		must(err)
		_, err = io.Copy(w, src)
		src.Close()
		must(err)
	}
	must(zw.Close())
}

func sha(path string) string {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	must(err)
	return hex.EncodeToString(h.Sum(nil))
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "release:", msg)
	os.Exit(1)
}
