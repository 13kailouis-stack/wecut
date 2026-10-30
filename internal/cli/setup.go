package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/spec"
)

// setupClient is one agent app WeCut can register with. Paths come from the
// environment at run time, so nothing here reads a real home in tests.
type setupClient struct {
	ID, Name string
	Dir      string // exists when the client is installed
	Config   string
	TOML     bool
	Skills   string // personal skills folder ("" = this client has none)
	Timeouts string // extra TOML lines: an edit outlasts these clients' defaults
	NoCwd    bool   // starts servers in no particular folder, so it needs a fixed workspace
}

func setupClients() ([]setupClient, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	envOr := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}
	claude := envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	claudeJSON := filepath.Join(home, ".claude.json")
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		claudeJSON = filepath.Join(v, ".claude.json")
	}
	var desktop string
	switch runtime.GOOS {
	case "windows":
		desktop = filepath.Join(envOr("APPDATA", filepath.Join(home, "AppData", "Roaming")), "Claude")
		// The Microsoft Store build keeps its own private copy.
		if store := filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude"); os.Getenv("LOCALAPPDATA") != "" && fileExists(store) {
			desktop = store
		}
	case "darwin":
		desktop = filepath.Join(home, "Library", "Application Support", "Claude")
	default:
		desktop = filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "Claude")
	}
	codex := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	grok := envOr("GROK_HOME", filepath.Join(home, ".grok"))
	shared := filepath.Join(home, ".agents", "skills")
	return []setupClient{
		{ID: "claude-code", Name: "Claude Code", Dir: claude, Config: claudeJSON, Skills: filepath.Join(claude, "skills")},
		{ID: "claude-desktop", Name: "Claude Desktop", Dir: desktop, Config: filepath.Join(desktop, "claude_desktop_config.json"), NoCwd: true},
		{ID: "codex", Name: "Codex", Dir: codex, Config: filepath.Join(codex, "config.toml"), TOML: true, Skills: shared, Timeouts: "startup_timeout_sec = 60\ntool_timeout_sec = 1800\n"},
		{ID: "cursor", Name: "Cursor", Dir: filepath.Join(home, ".cursor"), Config: filepath.Join(home, ".cursor", "mcp.json"), Skills: shared},
		{ID: "gemini", Name: "Gemini CLI", Dir: filepath.Join(home, ".gemini"), Config: filepath.Join(home, ".gemini", "settings.json"), Skills: shared},
		{ID: "windsurf", Name: "Windsurf", Dir: filepath.Join(home, ".codeium", "windsurf"), Config: filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), Skills: shared},
		{ID: "grok", Name: "Grok Build", Dir: grok, Config: filepath.Join(grok, "config.toml"), TOML: true, Skills: shared, Timeouts: "startup_timeout_sec = 60\n"},
	}, nil
}

// setup registers WeCut's MCP server and skill with the agent apps on this
// machine. It is the one command that writes outside the workspace, so it only
// shows the changes unless --apply is given, and it is not offered over MCP.
//
//	wecut setup [--client claude-code,codex] [--root DIR] [--apply] [--remove]
func (a *app) setup(args []string) int {
	var clientsArg, rootArg string
	var apply, remove, noSkill bool
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.StringVar(&clientsArg, "client", "", "clients to set up, comma separated (default: every one installed)")
	fs.StringVar(&rootArg, "root", "", "fix the workspace to this folder (default: the folder the client starts in; Claude Desktop needs one, ~/WeCut if not given)")
	fs.BoolVar(&apply, "apply", false, "make the changes (without it, they are only shown)")
	fs.BoolVar(&remove, "remove", false, "take WeCut out of the clients instead")
	fs.BoolVar(&noSkill, "no-skill", false, "register the MCP server only")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "wecut setup takes only flags", Hint: "wecut setup --client claude-code,codex --apply"})
	}
	clients, err := setupClients()
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	want := map[string]bool{}
	for _, id := range strings.Split(clientsArg, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		known := false
		for _, c := range clients {
			known = known || c.ID == id
		}
		if !known {
			var ids []string
			for _, c := range clients {
				ids = append(ids, c.ID)
			}
			return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "unknown client " + id, Hint: "Clients: " + strings.Join(ids, ", ")})
		}
		want[id] = true
	}
	if rootArg != "" {
		abs, err := filepath.Abs(rootArg)
		if err != nil {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
		}
		rootArg = abs
	}
	home, _ := os.UserHomeDir()
	command, cmdArgs := serverCommand()
	stamp := time.Now().Format("20060102-150405")
	var results []map[string]any
	failed := false
	for _, c := range clients {
		detected := fileExists(c.Dir)
		if (len(want) > 0 && !want[c.ID]) || (len(want) == 0 && !detected) {
			results = append(results, map[string]any{"id": c.ID, "name": c.Name, "detected": detected, "action": "skip"})
			continue
		}
		root := rootArg
		if root == "" && c.NoCwd {
			root = filepath.Join(home, "WeCut")
		}
		sa := append(append([]string{}, cmdArgs...), "mcp")
		if root != "" {
			sa = append(sa, "--root", root)
		}
		r := map[string]any{"id": c.ID, "name": c.Name, "detected": detected, "config": c.Config}
		action, backup, err := setupConfig(c, command, sa, remove, apply, stamp)
		r["action"] = action
		if backup != "" {
			r["backup"] = backup
		}
		if err != nil {
			failed = true
			r["problem"] = err.Error()
			r["add_by_hand"] = serverSnippet(c, command, sa)
		} else if apply && action != "none" {
			r["verified"] = true
		}
		if apply && !remove && root != "" && c.NoCwd {
			os.MkdirAll(root, 0o755)
			r["workspace"] = root
		}
		if c.Skills != "" && !noSkill {
			f := filepath.Join(c.Skills, "wecut", "SKILL.md")
			act, err := setupSkill(f, remove, apply)
			r["skill"], r["skill_action"] = f, act
			if err != nil {
				failed = true
				r["problem"] = err.Error()
			}
		}
		results = append(results, r)
	}
	out := map[string]any{"applied": apply, "server": map[string]any{"command": command, "args": append(append([]string{}, cmdArgs...), "mcp")}, "clients": results}
	switch {
	case !apply:
		out["next"] = "Nothing was changed. Run again with --apply to make these changes; each edited file gets a backup next to it."
	case remove:
		out["next"] = "Restart the clients."
	default:
		out["next"] = "Restart the clients, then ask one to clone a video with WeCut. Run wecut doctor to check FFmpeg."
	}
	if failed {
		out["ok"] = false
		a.out(out)
		return exitInput
	}
	return a.ok(out)
}

// serverCommand is how a client should start this WeCut: the npx cache moves,
// so a copy run through npx registers npx; a real install registers itself.
func serverCommand() (string, []string) {
	exe, err := os.Executable()
	if err != nil {
		return "npx", []string{"-y", "wecut"}
	}
	p := filepath.ToSlash(exe)
	if strings.Contains(p, "/_npx/") {
		return "npx", []string{"-y", "wecut"}
	}
	if strings.Contains(p, "/node_modules/") {
		if _, err := exec.LookPath("wecut"); err == nil {
			return "wecut", nil
		}
		return "npx", []string{"-y", "wecut"}
	}
	return exe, nil
}

func setupConfig(c setupClient, command string, args []string, remove, apply bool, stamp string) (action, backup string, err error) {
	old, err := os.ReadFile(c.Config)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "manual", "", err
	}
	var next []byte
	if c.TOML {
		next = editTOML(old, "mcp_servers.wecut", tomlServer(command, args, c.Timeouts), remove)
	} else {
		value, _ := marshalNoEscape(map[string]any{"command": command, "args": args}, "    ")
		next, err = editJSON(old, "mcpServers", "wecut", value, remove)
		if err != nil {
			return "manual", "", fmt.Errorf("%s could not be edited safely (%v); nothing was changed", c.Config, err)
		}
	}
	if bytes.Equal(old, next) {
		return "none", "", nil
	}
	action = "update"
	if remove {
		action = "remove"
	} else if !configHasServer(c, old) {
		action = "add"
	}
	if !apply {
		return action, "", nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Config), 0o755); err != nil {
		return "manual", "", err
	}
	if exists {
		backup = c.Config + ".wecut-backup-" + stamp
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "manual", "", err
		}
	}
	if err := writeAtomic(c.Config, next); err != nil {
		return "manual", backup, err
	}
	// Read it back: what the client will read must say what was meant.
	back, err := os.ReadFile(c.Config)
	if err != nil || !bytes.Equal(back, next) || configHasServer(c, back) == remove {
		return "manual", backup, fmt.Errorf("%s did not read back as written; restore %s", c.Config, backup)
	}
	return action, backup, nil
}

func configHasServer(c setupClient, data []byte) bool {
	if c.TOML {
		return bytes.Contains(data, []byte("[mcp_servers.wecut]"))
	}
	var doc struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	_, ok := doc.Servers["wecut"]
	return ok
}

func setupSkill(f string, remove, apply bool) (string, error) {
	old, err := os.ReadFile(f)
	exists := err == nil
	ours := exists && bytes.Contains(old, []byte("name: wecut"))
	switch {
	case remove && !ours:
		return "none", nil
	case remove:
		if apply {
			return "remove", os.RemoveAll(filepath.Dir(f))
		}
		return "remove", nil
	case exists && !ours:
		return "manual", fmt.Errorf("%s is not WeCut's skill; it was left alone", f)
	case exists && bytes.Equal(old, wecut.Skill()):
		return "none", nil
	}
	action := "add"
	if exists {
		action = "update"
	}
	if !apply {
		return action, nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return "manual", err
	}
	return action, writeAtomic(f, wecut.Skill())
}

func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wecut-setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	os.Chmod(tmp.Name(), mode)
	return os.Rename(tmp.Name(), path)
}

func marshalNoEscape(v any, prefix string) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}

// editJSON sets or removes section.name and leaves every other byte of the
// file as it was: these files hold the client's own settings, in its own order.
func editJSON(data []byte, section, name string, value []byte, remove bool) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if remove {
			return data, nil
		}
		return []byte(fmt.Sprintf("{\n  %q: {\n    %q: %s\n  }\n}\n", section, name, value)), nil
	}
	if !json.Valid(data) {
		return nil, errors.New("it is not plain JSON")
	}
	top := bytes.IndexByte(data, '{')
	if top < 0 {
		return nil, errors.New("its top level is not an object")
	}
	secStart, secEnd, ok := jsonMember(data, top, section)
	if !ok {
		if remove {
			return data, nil
		}
		return jsonInsert(data, top, fmt.Sprintf("  %q: {\n    %q: %s\n  }", section, name, value)), nil
	}
	if data[secStart] != '{' {
		return nil, fmt.Errorf("%s is not an object", section)
	}
	vs, ve, ok := jsonMember(data, secStart, name)
	var out []byte
	switch {
	case ok && remove:
		out = jsonDeleteMember(data, secStart, secEnd, name)
	case ok:
		if bytes.Equal(data[vs:ve], value) {
			return data, nil
		}
		out = append(append(append([]byte{}, data[:vs]...), value...), data[ve:]...)
	case remove:
		return data, nil
	default:
		out = jsonInsert(data, secStart, fmt.Sprintf("    %q: %s", name, value))
	}
	if !json.Valid(out) {
		return nil, errors.New("the edit would break the file")
	}
	return out, nil
}

// jsonMember finds a key of the object at offset obj and returns where its
// value starts and ends.
func jsonMember(data []byte, obj int, key string) (start, end int, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(data[obj:]))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return 0, 0, false
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, false
		}
		if t == key {
			end = obj + int(dec.InputOffset())
			return end - len(raw), end, true
		}
	}
	return 0, 0, false
}

// jsonInsert puts a member first in the object at offset obj.
func jsonInsert(data []byte, obj int, member string) []byte {
	rest := bytes.TrimLeft(data[obj+1:], " \t\r\n")
	out := append(append([]byte{}, data[:obj+1]...), '\n')
	out = append(out, member...)
	if len(rest) > 0 && rest[0] == '}' {
		return append(append(out, '\n'), rest...)
	}
	// The whitespace that follows keeps the next member where it was.
	return append(append(out, ','), data[obj+1:]...)
}

// jsonDeleteMember removes a key and its comma from the object at offset obj.
func jsonDeleteMember(data []byte, obj, objEnd int, key string) []byte {
	keyJSON, _ := json.Marshal(key)
	vs, ve, _ := jsonMember(data, obj, key)
	ks := bytes.LastIndex(data[:vs], keyJSON)
	// The comma after it, or else the one before it.
	j := ve
	for j < objEnd && strings.ContainsRune(" \t\r\n", rune(data[j])) {
		j++
	}
	if data[j] == ',' {
		j++
		for j < objEnd && strings.ContainsRune(" \t\r\n", rune(data[j])) {
			j++
		}
		return append(append([]byte{}, data[:ks]...), data[j:]...)
	}
	i := ks
	for i > obj+1 && strings.ContainsRune(" \t\r\n", rune(data[i-1])) {
		i--
	}
	if data[i-1] == ',' {
		i--
	}
	return append(append([]byte{}, data[:i]...), data[ve:]...)
}

func tomlServer(command string, args []string, extra string) string {
	cmd, _ := marshalNoEscape(command, "")
	list, _ := json.Marshal(args)
	return "command = " + string(cmd) + "\nargs = " + string(list) + "\n" + extra
}

// editTOML sets or removes a table by its header line. Sub-tables are the
// user's and stay.
func editTOML(data []byte, table, body string, remove bool) []byte {
	header := "[" + table + "]"
	lines := strings.SplitAfter(string(data), "\n")
	start, end := -1, len(lines)
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if start < 0 && t == header {
			start = i
		} else if start >= 0 && strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		if remove {
			return data
		}
		var b strings.Builder
		b.Write(data)
		if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
			b.WriteString("\n")
		}
		if len(data) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(header + "\n" + body)
		return []byte(b.String())
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines[:start], ""))
	if !remove {
		b.WriteString(header + "\n" + body)
		if end < len(lines) {
			b.WriteString("\n")
		}
	}
	b.WriteString(strings.Join(lines[end:], ""))
	return []byte(b.String())
}

func serverSnippet(c setupClient, command string, args []string) string {
	if c.TOML {
		return "[mcp_servers.wecut]\n" + tomlServer(command, args, c.Timeouts)
	}
	v, _ := marshalNoEscape(map[string]any{"mcpServers": map[string]any{"wecut": map[string]any{"command": command, "args": args}}}, "")
	return string(v)
}
