// Command wasm builds the clone analyzer for WeCut Studio:
// studio/engine/clone.wasm and the Go runtime's wasm_exec.js next to it.
//
//	go run ./tools/wasm
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	out := filepath.Join("studio", "engine")
	if err := os.MkdirAll(out, 0o755); err != nil {
		fail(err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(out, "clone.wasm"), "./cmd/wecut-wasm")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail(err)
	}
	// wasm_exec.js moved from misc/wasm to lib/wasm in Go 1.24.
	root := runtime.GOROOT()
	for _, dir := range []string{"lib", "misc"} {
		b, err := os.ReadFile(filepath.Join(root, dir, "wasm", "wasm_exec.js"))
		if err == nil {
			if err := os.WriteFile(filepath.Join(out, "wasm_exec.js"), b, 0o644); err != nil {
				fail(err)
			}
			fmt.Println("wrote", filepath.Join(out, "clone.wasm"), "and wasm_exec.js")
			return
		}
	}
	fail(fmt.Errorf("wasm_exec.js not found under %s", root))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
