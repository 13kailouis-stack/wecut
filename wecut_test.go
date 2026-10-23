package wecut

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The registry and plugin manifests are read by other programs, so their
// names and versions must agree with the engine and with each other.
func TestManifestsAgree(t *testing.T) {
	read := func(path string, v any) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	var server struct {
		Name     string
		Version  string
		Packages []struct {
			RegistryType, Identifier, Version string
		}
	}
	read("server.json", &server)
	if server.Version != Version || len(server.Packages) != 1 || server.Packages[0].Version != Version || server.Packages[0].Identifier != "wecut" {
		t.Errorf("server.json: %+v, engine version %s", server, Version)
	}

	release, err := os.ReadFile("tools/release/main.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`mcpName\s*=\s*"([^"]+)"`).FindSubmatch(release)
	if m == nil || string(m[1]) != server.Name {
		t.Errorf("npm mcpName in tools/release must be %q (the MCP Registry checks it)", server.Name)
	}

	for _, path := range []string{".claude-plugin/plugin.json", "gemini-extension.json"} {
		var v struct{ Name, Version string }
		read(path, &v)
		if v.Name != "wecut" || v.Version != Version {
			t.Errorf("%s: name %q version %q, want wecut %s", path, v.Name, v.Version, Version)
		}
	}

	var market struct {
		Plugins []struct{ Name, Source string }
	}
	read(".claude-plugin/marketplace.json", &market)
	if len(market.Plugins) != 1 || market.Plugins[0].Name != "wecut" || market.Plugins[0].Source != "./" {
		t.Errorf("marketplace.json: %+v", market)
	}
}

func TestGuide(t *testing.T) {
	g := Guide()
	if !bytes.HasPrefix(g, []byte("# WeCut")) {
		t.Fatalf("guide should start at the title, without front matter: %.80q", g)
	}
	if !bytes.HasPrefix(bytes.ReplaceAll(skill, []byte("\r\n"), []byte("\n")), []byte("---\nname: wecut\n")) {
		t.Error("SKILL.md front matter must start with name: wecut")
	}
}

// llms.txt points agents at raw files; every one of them must exist.
func TestLLMsTxtLinks(t *testing.T) {
	b, err := os.ReadFile("llms.txt")
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`https://raw\.githubusercontent\.com/13kailouis-stack/wecut/main/([^)\s]+)`).FindAllSubmatch(b, -1)
	if len(links) < 10 {
		t.Fatalf("only %d raw links in llms.txt", len(links))
	}
	for _, l := range links {
		if _, err := os.Stat(string(l[1])); err != nil {
			t.Errorf("llms.txt links to missing file %s", l[1])
		}
	}
}
