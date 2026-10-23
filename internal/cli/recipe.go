package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wecut "github.com/13kailouis-stack/wecut"
	"github.com/13kailouis-stack/wecut/internal/recipe"
	"github.com/13kailouis-stack/wecut/internal/spec"
	"github.com/13kailouis-stack/wecut/internal/workspace"
)

// Recipes are templates: the shape of an edit without its content.
//
//	wecut recipe list                         built-in and workspace recipes (recipes/*.json)
//	wecut recipe show <name|file>             a recipe, its roles and text slots
//	wecut recipe schema                       JSON Schema of the recipe format
//	wecut recipe validate <file|->            check a recipe
//	wecut recipe apply --input <file|->       recipe + clips, words, music -> project
//	wecut recipe from-project <project|->     save a finished project as a recipe
func (a *app) recipe(args []string) int {
	if len(args) == 0 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give a recipe command", Hint: "list, show, schema, validate, apply, from-project"})
	}
	switch args[0] {
	case "list":
		return a.recipeList(args[1:])
	case "show":
		return a.recipeShow(args[1:])
	case "schema":
		a.stdout.Write(wecut.RecipeSchema)
		return exitOK
	case "validate":
		return a.recipeValidate(args[1:])
	case "apply":
		return a.recipeApply(args[1:])
	case "from-project":
		return a.recipeFromProject(args[1:])
	}
	return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: fmt.Sprintf("unknown recipe command %q", args[0]), Hint: "list, show, schema, validate, apply, from-project"})
}

// readJSON reads a workspace file, or stdin for "-".
func (a *app) readJSON(root *workspace.Root, arg string) ([]byte, error) {
	if arg == "-" {
		return io.ReadAll(io.LimitReader(a.stdin, maxProjSize))
	}
	abs, err := root.Input(arg)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// findRecipe resolves a recipe reference: a .json file in the workspace, a
// built-in name, or recipes/<name>.json in the workspace.
func (a *app) findRecipe(root *workspace.Root, ref string) (*recipe.Recipe, string, []spec.Issue) {
	var data []byte
	from := ref
	switch {
	case ref == "-" || strings.HasSuffix(strings.ToLower(ref), ".json"):
		b, err := a.readJSON(root, ref)
		if err != nil {
			return nil, "", []spec.Issue{pathIssue(ref, "", err)}
		}
		data = b
	default:
		// A workspace recipe with a built-in's name is the user's own version.
		if abs, err := root.Input(filepath.Join("recipes", ref+".json")); err == nil {
			data, _ = os.ReadFile(abs)
			from = "recipes/" + ref + ".json"
		} else if b, ok := wecut.Recipe(ref); ok {
			data, from = b, "built-in"
		} else {
			return nil, "", []spec.Issue{{Code: "E_RECIPE", Message: fmt.Sprintf("no recipe %q", ref), Hint: "Run `wecut recipe list` for the names."}}
		}
	}
	return decodeRecipe(data, from)
}

func decodeRecipe(data []byte, from string) (*recipe.Recipe, string, []spec.Issue) {
	var r recipe.Recipe
	if issues := spec.DecodeAs(data, &r); len(issues) > 0 {
		return nil, "", issues
	}
	if issues := recipe.Validate(&r); len(issues) > 0 {
		return nil, "", issues
	}
	return &r, from, nil
}

func (a *app) recipeList(args []string) int {
	var c common
	fs := flag.NewFlagSet("recipe list", flag.ContinueOnError)
	c.bind(fs)
	if _, err := parse(fs, args); err != nil {
		return a.fail(exitInput, nil, usageErr(err))
	}
	type item struct {
		Name  string `json:"name"`
		About string `json:"about,omitempty"`
		From  string `json:"from"`
	}
	var out []item
	for _, n := range wecut.RecipeNames() {
		b, _ := wecut.Recipe(n)
		var r recipe.Recipe
		json.Unmarshal(b, &r)
		out = append(out, item{n, r.About, "built-in"})
	}
	if root, err := workspace.Open(c.root); err == nil {
		entries, _ := os.ReadDir(filepath.Join(root.Dir(), "recipes"))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			rel := "recipes/" + e.Name()
			b, err := a.readJSON(root, rel)
			if err != nil {
				continue
			}
			if builtin, ok := wecut.Recipe(strings.TrimSuffix(e.Name(), ".json")); ok && bytes.Equal(bytes.TrimSpace(builtin), bytes.TrimSpace(b)) {
				continue // an unchanged copy of a built-in
			}
			r, _, issues := decodeRecipe(b, rel)
			if len(issues) > 0 {
				out = append(out, item{rel, "invalid: " + issues[0].Message, rel})
				continue
			}
			out = append(out, item{strings.TrimSuffix(e.Name(), ".json"), r.About, rel})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From == "built-in" && out[j].From != "built-in" })
	return a.ok(map[string]any{"recipes": out, "hint": "wecut recipe show <name> for roles and text slots"})
}

func (a *app) recipeShow(args []string) int {
	var c common
	fs := flag.NewFlagSet("recipe show", flag.ContinueOnError)
	c.bind(fs)
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one recipe name or file", Hint: "wecut recipe show news-headline"})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	r, from, issues := a.findRecipe(root, pos[0])
	if len(issues) > 0 {
		return a.fail(exitInput, nil, issues...)
	}
	roles := make([]map[string]any, len(r.Sections))
	for i, s := range r.Sections {
		roles[i] = map[string]any{"role": s.Role, "about": s.About, "duration": s.Duration}
	}
	music := "none"
	if r.Music != nil && r.Music.Src != "" {
		music = r.Music.Src
	}
	return a.ok(map[string]any{"recipe": r, "from": from, "roles": roles, "slots": recipe.Slots(r), "captions": r.Captions != nil, "default_music": music})
}

func (a *app) recipeValidate(args []string) int {
	var c common
	fs := flag.NewFlagSet("recipe validate", flag.ContinueOnError)
	c.bind(fs)
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one recipe file", Hint: "wecut recipe validate recipes/mine.json"})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	b, err := a.readJSON(root, pos[0])
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(pos[0], "", err))
	}
	r, _, issues := decodeRecipe(b, pos[0])
	if len(issues) > 0 {
		return a.fail(exitInput, nil, issues...)
	}
	return a.ok(map[string]any{"name": r.Name, "sections": len(r.Sections), "slots": recipe.Slots(r)})
}

// applyRequest is the input of `recipe apply`: which recipe, the new
// material, and where to save the project.
type applyRequest struct {
	Recipe json.RawMessage `json:"recipe"` // built-in name, file, or the recipe itself
	recipe.Input
	Write string `json:"write,omitempty"`
}

func (a *app) recipeApply(args []string) int {
	var c common
	var input, write string
	fs := flag.NewFlagSet("recipe apply", flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&input, "input", "", "apply request (.json file or - for stdin)")
	fs.StringVar(&write, "write", "", "save the project here")
	pos, err := parse(fs, args)
	if err != nil || input == "" || len(pos) > 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give the request with --input", Hint: `wecut recipe apply --input - <<< '{"recipe": "news-headline", "clips": [{"src": "media/talk.mp4", "from": 30, "to": 75}], "texts": {"headline": {"text": "..."}}}'`})
	}
	root, err := workspace.Open(c.root)
	if err != nil {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_PATH", Message: err.Error()})
	}
	data, err := a.readJSON(root, input)
	if err != nil {
		return a.fail(exitInput, nil, pathIssue(input, "", err))
	}
	var req applyRequest
	if issues := spec.DecodeAs(data, &req); len(issues) > 0 {
		return a.fail(exitInput, nil, issues...)
	}
	if write != "" {
		req.Write = write
	}
	// Name the render after the project file, so applying a recipe twice
	// does not overwrite the first video.
	if req.Output == "" && req.Write != "" {
		req.Output = "out/" + strings.TrimSuffix(filepath.Base(req.Write), filepath.Ext(req.Write)) + ".mp4"
	}

	// The recipe: a name or file from the request, the positional argument,
	// or an inline object.
	var r *recipe.Recipe
	var issues []spec.Issue
	raw := bytes.TrimSpace(req.Recipe)
	switch {
	case len(pos) == 1:
		r, _, issues = a.findRecipe(root, pos[0])
	case len(raw) > 0 && raw[0] == '"':
		var ref string
		json.Unmarshal(raw, &ref)
		r, _, issues = a.findRecipe(root, ref)
	case len(raw) > 0 && raw[0] == '{':
		r, _, issues = decodeRecipe(raw, "inline")
	default:
		issues = []spec.Issue{{Code: "E_USAGE", Path: "/recipe", Message: "say which recipe to use", Hint: "\"recipe\": \"news-headline\", a .json path, or the recipe object. `wecut recipe list` shows the names."}}
	}
	if len(issues) > 0 {
		return a.fail(exitInput, nil, issues...)
	}

	t, iss := a.tools(c)
	if iss != nil {
		return a.fail(exitDep, nil, *iss)
	}
	var uses []use
	for i, cl := range req.Clips {
		uses = append(uses, use{cl.Src, fmt.Sprintf("/clips/%d/src", i)})
	}
	if src, _ := recipe.MusicSource(r, req.Input); src != "" {
		uses = append(uses, use{src, "/music"})
	}
	set, errs := a.probeAll(root, t, uses, map[string]bool{})
	if len(errs) > 0 {
		return a.fail(exitInput, nil, errs...)
	}
	p, warns, errs := recipe.Apply(r, req.Input, set)
	if len(errs) > 0 {
		return a.fail(exitFor(errs), warns, errs...)
	}
	projectJSON, _ := json.MarshalIndent(p, "", "  ")

	// Check the result exactly as `validate` would, transcript and all.
	l, issues, code := a.prepare(c, root, projectJSON, slugName(r.Name))
	if len(issues) > 0 {
		res := map[string]any{"ok": false, "errors": issues, "project": p}
		if len(warns) > 0 {
			res["warnings"] = warns
		}
		a.out(res)
		return code
	}
	res := map[string]any{
		"recipe": r.Name, "project": p, "duration": l.edit.Total, "width": l.edit.W, "height": l.edit.H,
		"next": "wecut preview <project> to look, then wecut render <project>",
	}
	if req.Write != "" {
		if ext := strings.ToLower(filepath.Ext(req.Write)); ext != ".json" {
			return a.fail(exitInput, warns, spec.Issue{Code: "E_USAGE", Message: "write must be a .json file"})
		}
		abs, err := root.Output(req.Write)
		if err != nil {
			return a.fail(exitInput, warns, pathIssue(req.Write, "/write", err))
		}
		if err := os.WriteFile(abs, append(projectJSON, '\n'), 0o644); err != nil {
			return a.fail(exitRender, warns, spec.Issue{Code: "E_RENDER", Message: err.Error()})
		}
		res["file"] = root.Rel(abs)
		res["next"] = fmt.Sprintf("wecut preview %s, then wecut render %s", root.Rel(abs), root.Rel(abs))
	}
	all := append(warns, l.edit.Warnings...)
	if len(all) > 0 {
		res["warnings"] = all
	}
	return a.ok(res)
}

func (a *app) recipeFromProject(args []string) int {
	var c common
	var name, write string
	fs := flag.NewFlagSet("recipe from-project", flag.ContinueOnError)
	c.bind(fs)
	fs.StringVar(&name, "name", "", "recipe name")
	fs.StringVar(&write, "write", "", "save the recipe here (e.g. recipes/mine.json)")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "give one project file", Hint: "wecut recipe from-project out/my-edit.json --name my-style --write recipes/my-style.json"})
	}
	l, code := a.load(c, pos[0])
	if l == nil {
		return code
	}
	if name == "" {
		name = l.name
	}
	r := recipe.FromEdit(l.edit, slugName(name))
	if issues := recipe.Validate(r); len(issues) > 0 {
		return a.fail(exitRender, nil, issues...)
	}
	res := map[string]any{"recipe": r, "slots": recipe.Slots(r)}
	if r.Music != nil && r.Music.Src != "" {
		res["note"] = "the recipe keeps " + r.Music.Src + " as its default music; it must stay in the workspace for the recipe to use it"
	}
	if write != "" {
		if !strings.HasSuffix(strings.ToLower(write), ".json") {
			return a.fail(exitInput, nil, spec.Issue{Code: "E_USAGE", Message: "write must be a .json file"})
		}
		abs, err := l.root.Output(write)
		if err != nil {
			return a.fail(exitInput, nil, pathIssue(write, "", err))
		}
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(abs, append(b, '\n'), 0o644); err != nil {
			return a.fail(exitRender, nil, spec.Issue{Code: "E_RENDER", Message: err.Error()})
		}
		res["file"] = l.root.Rel(abs)
	}
	return a.ok(res)
}

func slugName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "recipe"
}
