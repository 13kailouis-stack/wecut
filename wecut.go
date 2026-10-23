// Package wecut holds the files shipped inside the binary that live at the
// repository root: the project JSON Schema and the example projects.
package wecut

import (
	"embed"
	"sort"
	"strings"
)

// Version of the wecut engine. Release builds set it with
// -ldflags "-X github.com/13kailouis-stack/wecut.Version=x.y.z".
var Version = "0.1.0"

//go:embed schema/wecut.project.schema.json
var Schema []byte

//go:embed examples/*.json
var examples embed.FS

func ExampleNames() []string {
	entries, _ := examples.ReadDir("examples")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

func Example(name string) ([]byte, bool) {
	if strings.ContainsAny(name, `/\.`) {
		return nil, false
	}
	b, err := examples.ReadFile("examples/" + name + ".json")
	return b, err == nil
}
