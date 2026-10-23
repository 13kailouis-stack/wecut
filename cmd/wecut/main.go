// Command wecut renders WeCut projects with the FFmpeg on this machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/cli"
	"github.com/13kailouis-stack/wecut/internal/mcp"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	var code int
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		code = serveMCP(ctx, os.Args[2:])
	} else {
		code = cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	stop()
	os.Exit(code)
}

// serveMCP runs the MCP server on stdin/stdout. Diagnostics go to stderr,
// because stdout carries only protocol messages.
func serveMCP(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", "", "workspace folder (default: current folder)")
	ffmpeg := fs.String("ffmpeg", "", "ffmpeg binary")
	ffprobe := fs.String("ffprobe", "", "ffprobe binary")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "wecut mcp:", err)
		return 2
	}
	ws, err := workspace.Open(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wecut mcp:", err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "wecut %s MCP server, workspace %s\n", wecut.Version, ws.Dir())
	cfg := mcp.Config{Root: ws.Dir(), FFmpeg: *ffmpeg, FFprobe: *ffprobe, Version: wecut.Version}
	if err := mcp.Serve(ctx, cfg, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "wecut mcp:", err)
		return 1
	}
	return 0
}
