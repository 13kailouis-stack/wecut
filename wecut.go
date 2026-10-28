// Package wecut holds the files shipped inside the binary that live at the
// repository root: JSON Schemas, example projects, built-in recipes and the
// agent guide.
package wecut

import (
	"bytes"
	"embed"
	"sort"
	"strings"
)

// Version of the wecut engine. Release builds set it with
// -ldflags "-X github.com/13kailouis-stack/wecut.Version=x.y.z".
var Version = "0.3.0"

//go:embed schema/wecut.project.schema.json
var Schema []byte

//go:embed schema/wecut.recipe.schema.json
var RecipeSchema []byte

//go:embed examples/*.json
var examples embed.FS

//go:embed recipes/*.json
var recipes embed.FS

//go:embed skills/wecut/SKILL.md
var skill []byte

// Guide is the Agent Skill without its front matter: what `wecut guide`
// prints for agents that only have a terminal.
func Guide() []byte {
	b := bytes.ReplaceAll(skill, []byte("\r\n"), []byte("\n"))
	if rest, ok := bytes.CutPrefix(b, []byte("---\n")); ok {
		if _, body, ok := bytes.Cut(rest, []byte("\n---\n")); ok {
			return bytes.TrimLeft(body, "\n")
		}
	}
	return b
}

func ExampleNames() []string { return names(examples, "examples") }

func Example(name string) ([]byte, bool) { return read(examples, "examples", name) }

// RecipeNames lists the built-in recipes.
func RecipeNames() []string { return names(recipes, "recipes") }

func Recipe(name string) ([]byte, bool) { return read(recipes, "recipes", name) }

func names(fs embed.FS, dir string) []string {
	entries, _ := fs.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

func read(fs embed.FS, dir, name string) ([]byte, bool) {
	if strings.ContainsAny(name, `/\.`) {
		return nil, false
	}
	b, err := fs.ReadFile(dir + "/" + name + ".json")
	return b, err == nil
}
