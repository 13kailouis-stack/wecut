package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupRun(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	code := Run(context.Background(), append([]string{"setup"}, args...), strings.NewReader(""), &out, &out)
	var r map[string]any
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("not JSON: %s", out.String())
	}
	return r, code
}

func clientOf(t *testing.T, r map[string]any, id string) map[string]any {
	t.Helper()
	for _, c := range r["clients"].([]any) {
		if m := c.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("no client %s in %v", id, r)
	return nil
}

// TestSetup runs against a made-up home: it finds the installed clients, keeps
// their other settings byte for byte, backs up, and can be undone.
func TestSetup(t *testing.T) {
	home := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME", "XDG_CONFIG_HOME", "LOCALAPPDATA"} {
		t.Setenv(k, "")
	}
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	claude := filepath.Join(home, ".claude.json")
	claudeText := "{\n  \"zebra\": 1,\n  \"mcpServers\": {\n    \"other\": { \"command\": \"x\" }\n  },\n  \"apple\": [1,  2]\n}\n"
	os.WriteFile(claude, []byte(claudeText), 0o644)
	codex := filepath.Join(home, ".codex", "config.toml")
	codexText := "model = \"gpt\"\n\n[mcp_servers.other]\ncommand = \"x\"\n"
	os.WriteFile(codex, []byte(codexText), 0o644)

	// Without --apply nothing changes.
	r, code := setupRun(t)
	if code != 0 || r["applied"] != false {
		t.Fatalf("dry run: %v", r)
	}
	if clientOf(t, r, "claude-code")["action"] != "add" || clientOf(t, r, "codex")["action"] != "add" || clientOf(t, r, "cursor")["action"] != "skip" {
		t.Fatalf("plan: %v", r["clients"])
	}
	if b, _ := os.ReadFile(claude); string(b) != claudeText {
		t.Fatal("dry run changed the file")
	}

	r, code = setupRun(t, "--apply")
	if code != 0 {
		t.Fatalf("apply: %v", r)
	}
	c := clientOf(t, r, "claude-code")
	if c["verified"] != true || c["backup"] == nil {
		t.Fatalf("claude-code: %v", c)
	}
	if b, _ := os.ReadFile(c["backup"].(string)); string(b) != claudeText {
		t.Fatal("backup differs from the original")
	}
	b, _ := os.ReadFile(claude)
	var doc struct {
		Zebra   int
		Apple   []int
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if doc.Zebra != 1 || doc.Servers["other"].Command != "x" || doc.Servers["wecut"].Command == "" {
		t.Fatalf("merged: %s", b)
	}
	if !strings.Contains(string(b), `"apple": [1,  2]`) {
		t.Fatalf("other settings were reformatted: %s", b)
	}
	if !fileExists(filepath.Join(home, ".claude", "skills", "wecut", "SKILL.md")) || !fileExists(filepath.Join(home, ".agents", "skills", "wecut", "SKILL.md")) {
		t.Fatal("skills not installed")
	}
	b, _ = os.ReadFile(codex)
	if !strings.HasPrefix(string(b), codexText) || !strings.Contains(string(b), "[mcp_servers.wecut]\ncommand = ") || !strings.Contains(string(b), "tool_timeout_sec") {
		t.Fatalf("codex: %s", b)
	}

	// Again: nothing to do.
	r, _ = setupRun(t, "--apply")
	if a := clientOf(t, r, "claude-code"); a["action"] != "none" || a["backup"] != nil {
		t.Fatalf("second run: %v", a)
	}

	// Undone, every other byte is back.
	r, code = setupRun(t, "--apply", "--remove")
	if code != 0 {
		t.Fatalf("remove: %v", r)
	}
	if b, _ := os.ReadFile(claude); string(b) != claudeText {
		t.Fatalf("after remove:\n%s", b)
	}
	if b, _ := os.ReadFile(codex); string(b) != codexText+"\n" && string(b) != codexText {
		t.Fatalf("after remove:\n%q", b)
	}
	if fileExists(filepath.Join(home, ".claude", "skills", "wecut")) {
		t.Fatal("skill stayed after remove")
	}

	// A file with comments is left alone, with the lines to add by hand.
	cursor := filepath.Join(home, ".cursor", "mcp.json")
	os.MkdirAll(filepath.Dir(cursor), 0o755)
	os.WriteFile(cursor, []byte("{ // mine\n}\n"), 0o644)
	r, code = setupRun(t, "--apply", "--client", "cursor")
	if code == 0 || clientOf(t, r, "cursor")["add_by_hand"] == nil {
		t.Fatalf("jsonc: %v", r)
	}
	if b, _ := os.ReadFile(cursor); string(b) != "{ // mine\n}\n" {
		t.Fatal("jsonc file was changed")
	}

	if _, code := setupRun(t, "--client", "nope"); code == 0 {
		t.Fatal("unknown client accepted")
	}
}

// Headers as TOML reads them: writing a second table next to one spelled
// differently would make the whole file invalid for the client.
func TestEditTOML(t *testing.T) {
	const table, body = "mcp_servers.wecut", "command = \"new\"\n"
	count := func(s string) int {
		n := 0
		for _, ln := range strings.Split(s, "\n") {
			if tomlTable(ln) == table {
				n++
			}
		}
		return n
	}
	for name, in := range map[string]string{
		"plain":   "[mcp_servers.wecut]\ncommand = \"old\"\n\n[other]\nx = 1\n",
		"comment": "[mcp_servers.wecut] # mine\ncommand = \"old\"\n\n[other]\nx = 1\n",
		"spaces":  "[ mcp_servers . wecut ]\ncommand = \"old\"\n\n[other]\nx = 1\n",
		"quotes":  "[mcp_servers.\"wecut\"]\ncommand = \"old\"\n\n[other]\nx = 1\n",
	} {
		out, err := editTOML([]byte(in), table, body, false)
		if err != nil || count(string(out)) != 1 || strings.Contains(string(out), "old") || !strings.Contains(string(out), "[other]\nx = 1") {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
		out, err = editTOML([]byte(in), table, body, true)
		if err != nil || count(string(out)) != 0 || strings.Contains(string(out), "old") || !strings.Contains(string(out), "[other]") {
			t.Errorf("%s remove: %v\n%s", name, err, out)
		}
	}
	// An update keeps the sub-tables; removal takes them too.
	withEnv := "[mcp_servers.wecut]\ncommand = \"old\"\n\n[mcp_servers.wecut.env]\nK = \"v\"\n\n[other]\nx = 1\n"
	if out, _ := editTOML([]byte(withEnv), table, body, false); !strings.Contains(string(out), "[mcp_servers.wecut.env]\nK") {
		t.Errorf("update dropped the env table:\n%s", out)
	}
	if out, _ := editTOML([]byte(withEnv), table, body, true); strings.Contains(string(out), "wecut") || !strings.Contains(string(out), "[other]") {
		t.Errorf("remove left a piece behind:\n%s", out)
	}
	// A look-alike is someone else's.
	if out, _ := editTOML([]byte("[mcp_servers.wecut2]\ncommand = \"x\"\n"), table, body, false); !strings.Contains(string(out), "wecut2") || count(string(out)) != 1 {
		t.Errorf("look-alike:\n%s", out)
	}
	// Written inline: refused, not doubled.
	for _, in := range []string{"mcp_servers = { wecut = { command = \"old\" } }\n", "mcp_servers.wecut.command = \"old\"\n", "[mcp_servers]\nwecut = { command = \"old\" }\n"} {
		if _, err := editTOML([]byte(in), table, body, false); err == nil {
			t.Errorf("inline accepted: %s", in)
		}
	}
	// Other servers written inline in a table of their own are fine.
	if out, err := editTOML([]byte("[mcp_servers]\nother = { command = \"x\" }\n"), table, body, false); err != nil || count(string(out)) != 1 {
		t.Errorf("other inline server: %v\n%s", err, out)
	}
}

func TestSlugNameSafe(t *testing.T) {
	for in, want := range map[string]string{"NUL": "nul-video", "com1": "com1-video", "Aux": "aux-video", "console": "console", "...": "recipe", "../../x": "x", "a b": "a-b"} {
		if got := slugName(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := slugName(strings.Repeat("x", 300)); len(got) > 60 {
		t.Errorf("length %d", len(got))
	}
}

func TestRepeatedJSONKeys(t *testing.T) {
	data := []byte(`{"mcpServers":{"wecut":{"command":"old"}},"mcpServers":{"x":{}}}`)
	out, err := editJSON(data, "mcpServers", "wecut", []byte(`{"command":"new"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !configHasServer(setupClient{}, out) || !strings.Contains(string(out), `"x":{}`) {
		t.Fatalf("got %s", out)
	}
}

func TestLowMemoryHint(t *testing.T) {
	for _, m := range []string{"ffmpeg failed: x264 [error]: malloc of size 7281920 failed", "Task finished with error code: -12 (Cannot allocate memory)"} {
		if !lowMemory(m) {
			t.Errorf("not recognised: %s", m)
		}
	}
	if lowMemory("Invalid data found when processing input") {
		t.Error("a broken file is not low memory")
	}
}
