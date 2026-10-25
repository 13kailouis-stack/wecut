package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const meta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

// session feeds lines to a server and collects every message it writes.
func session(t *testing.T, root string, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := Serve(context.Background(), Config{Root: root, Version: "test"}, in, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("server wrote a non-JSON line: %q", l)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func byID(msgs []map[string]any, id string) map[string]any {
	for _, m := range msgs {
		if b, _ := json.Marshal(m["id"]); string(b) == id {
			return m
		}
	}
	return nil
}

func errCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestLegacyHandshakeThenTools(t *testing.T) {
	msgs := session(t, t.TempDir(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"wecut_list","arguments":{"what":"fonts"}}}`,
	)
	init := byID(msgs, "1")["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["instructions"] == "" {
		t.Fatalf("initialize: %v", init)
	}
	list := byID(msgs, "2")["result"].(map[string]any)
	if _, modern := list["resultType"]; modern {
		t.Error("legacy result must not carry resultType")
	}
	if n := len(list["tools"].([]any)); n != 15 {
		t.Fatalf("%d tools", n)
	}
	call := byID(msgs, "3")["result"].(map[string]any)
	if call["isError"] != false || !strings.Contains(call["content"].([]any)[0].(map[string]any)["text"].(string), "montserrat") {
		t.Fatalf("tools/call: %v", call)
	}
}

func TestPortableRecipeTools(t *testing.T) {
	root := t.TempDir()
	pack := session(t, root,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wecut_recipe_pack","arguments":{"file":"podcast-punch","write":"share.wecut.json"}}}`,
	)
	if result := byID(pack, "2")["result"].(map[string]any); result["isError"] != false {
		t.Fatalf("pack: %v", result)
	}
	unpack := session(t, root,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wecut_recipe_unpack","arguments":{"file":"share.wecut.json","write":"imported"}}}`,
	)
	if result := byID(unpack, "2")["result"].(map[string]any); result["isError"] != false {
		t.Fatalf("unpack: %v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "imported", "recipe.json")); err != nil {
		t.Fatal(err)
	}
}

func TestPrompts(t *testing.T) {
	msgs := session(t, t.TempDir(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"copy-viral-format","arguments":{"reference":"media/viral.mp4","footage":"media/mine/"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"copy-viral-format","arguments":{"footage":"media/mine/"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"prompts/list","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"wecut_schema","arguments":{"what":"recipe"}}}`,
	)
	caps := byID(msgs, "1")["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps["prompts"] == nil {
		t.Fatalf("prompts capability missing: %v", caps)
	}
	list := byID(msgs, "2")["result"].(map[string]any)["prompts"].([]any)
	if len(list) != len(prompts) || len(list) < 4 {
		t.Fatalf("prompts/list: %v", list)
	}
	for _, p := range list {
		m := p.(map[string]any)
		if m["name"] == "" || m["description"] == "" || m["arguments"] == nil {
			t.Errorf("incomplete prompt: %v", m)
		}
	}
	get := byID(msgs, "3")["result"].(map[string]any)
	text := get["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(text, "wecut_analyze media/viral.mp4") || !strings.Contains(text, "the recipe's own track") || strings.Contains(text, "{") {
		t.Fatalf("prompt text: %s", text)
	}
	for _, id := range []string{"4", "5"} {
		if got := errCode(byID(msgs, id)); got != codeInvalidParams {
			t.Errorf("id %s: code %v", id, got)
		}
	}
	if r := byID(msgs, "6")["result"].(map[string]any); r["resultType"] != "complete" {
		t.Errorf("modern prompts/list: %v", r)
	}
	schema := byID(msgs, "7")["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(schema, `"title": "WeCut recipe`) {
		t.Errorf("recipe schema: %.200s", schema)
	}
}

func TestByteOrderMarkIgnored(t *testing.T) {
	msgs := session(t, t.TempDir(), "\xef\xbb\xbf"+`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if byID(msgs, "1")["result"] == nil {
		t.Fatalf("initialize after a BOM: %v", msgs)
	}
}

func TestUnknownLegacyVersionGetsNewestLegacy(t *testing.T) {
	msgs := session(t, t.TempDir(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := byID(msgs, "1")["result"].(map[string]any)["protocolVersion"]; v != legacyVersions[0] {
		t.Fatalf("got %v", v)
	}
}

func TestModernRequests(t *testing.T) {
	msgs := session(t, t.TempDir(),
		`{"jsonrpc":"2.0","id":"d","method":"server/discover","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2099-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope",`+meta+`}}`,
		`{"jsonrpc":"2.0","id":7,"method":"nope/nope","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"wecut_validate","arguments":{},`+meta+`}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"wecut_validate","arguments":{"projct":{}},`+meta+`}}`,
		`not json`,
	)
	d := byID(msgs, `"d"`)["result"].(map[string]any)
	if d["resultType"] != "complete" || d["supportedVersions"].([]any)[0] != "2026-07-28" {
		t.Fatalf("discover: %v", d)
	}
	if r := byID(msgs, "2")["result"].(map[string]any); r["resultType"] != "complete" || r["_meta"] == nil {
		t.Fatalf("modern tools/list: %v", r)
	}
	for id, want := range map[string]float64{"3": codeInvalidParams, "4": codeBadVersion, "5": codeInvalidParams, "6": codeInvalidParams, "7": codeNoMethod} {
		if got := errCode(byID(msgs, id)); got != want {
			t.Errorf("id %s: code %v, want %v", id, got, want)
		}
	}
	for _, id := range []string{"8", "9"} {
		if r := byID(msgs, id)["result"].(map[string]any); r["isError"] != true {
			t.Errorf("id %s: bad arguments should be a tool error: %v", id, r)
		}
	}
	if errCode(byID(msgs, "null")) != codeParse {
		t.Error("parse error not reported")
	}
}

func TestCancelledCallGetsNoReply(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "m"), 0o755)
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=s=1280x720:r=30:d=60", "-c:v", "libx264", "-preset", "ultrafast", filepath.Join(root, "m", "long.mp4"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		Serve(context.Background(), Config{Root: root, Version: "test"}, pr, &out, io.Discard)
		close(done)
	}()
	io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wecut_render","arguments":{"project":{"wecut":"1","output":{"quality":"high"},"clips":[{"src":"m/long.mp4"}]}},`+meta+`}}`+"\n")
	time.Sleep(1500 * time.Millisecond)
	io.WriteString(pw, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"test"}}`+"\n")
	pw.Close()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("server did not stop the cancelled render")
	}
	if strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("cancelled request was answered: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "out", "wecut.mp4")); err == nil {
		t.Fatal("cancelled render left an output file")
	}
}
