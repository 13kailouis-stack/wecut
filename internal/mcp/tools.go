package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type object = map[string]any

var noArgs = object{"type": "object", "additionalProperties": false}

var projectArgs = object{
	"project": object{
		"type":        "object",
		"description": "The WeCut project (format \"1\") as a JSON object. wecut_schema has the full format; wecut_example shows working projects.",
	},
	"projectFile": object{
		"type":        "string",
		"description": "Instead of project: path of a project .json file inside the workspace.",
	},
}

func withProject(extra object) object {
	props := object{}
	for k, v := range projectArgs {
		props[k] = v
	}
	for k, v := range extra {
		props[k] = v
	}
	return object{"type": "object", "properties": props, "additionalProperties": false}
}

func annotations(readOnly bool) object {
	return object{"readOnlyHint": readOnly, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
}

// tools is the full tool list, in a fixed order so clients can cache it.
var tools = []object{
	{
		"name": "wecut_recipe_pack", "title": "Package a template for another device",
		"description": "Save a recipe and its default music in one portable .wecut.json file. Fonts ship with WeCut. Does not send files or upload anything. Destination must not exist.",
		"inputSchema": object{"type": "object", "additionalProperties": false, "required": []string{"file", "write"}, "properties": object{"file": object{"type": "string", "description": "Recipe name or JSON file."}, "write": object{"type": "string", "description": "New package file in the workspace."}}},
		"annotations": annotations(false),
	},
	{
		"name": "wecut_recipe_unpack", "title": "Import a template from another device",
		"description": "Import a received .wecut.json package and included music into a NEW workspace directory. Returns recipe file, sections and text slots for recipe_apply. Never executes template text or downloads assets.",
		"inputSchema": object{"type": "object", "additionalProperties": false, "required": []string{"file", "write"}, "properties": object{"file": object{"type": "string", "description": "Received package path."}, "write": object{"type": "string", "description": "New directory, e.g. templates/my-style."}}},
		"annotations": annotations(false),
	},
	{
		"name": "wecut_doctor", "title": "Check FFmpeg",
		"description": "Check that FFmpeg is installed and has what WeCut needs (libass, xfade, an H.264 encoder). Run once before the first render.",
		"inputSchema": noArgs, "annotations": annotations(true),
	},
	{
		"name": "wecut_probe", "title": "Inspect media files",
		"description": "Duration, width, height, fps, rotation and audio for video, image or audio files in the workspace. Use the durations for clip from/to.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false, "required": []string{"files"},
			"properties": object{"files": object{"type": "array", "minItems": 1, "maxItems": 50, "items": object{"type": "string"}, "description": "Paths relative to the workspace."}},
		},
		"annotations": annotations(true),
	},
	{
		"name": "wecut_validate", "title": "Check a project",
		"description": "Check a project against the media without rendering. Returns the finished duration and size, or every error with a JSON pointer (e.g. /clips/0/to) and a hint on how to fix it.",
		"inputSchema": withProject(nil), "annotations": annotations(true),
	},
	{
		"name": "wecut_preview", "title": "Look at frames",
		"description": "Render a few still frames (540p JPEG) at chosen times of the finished video and return them as images, so you can check framing, text size and captions before rendering. Also saved under out/preview/.",
		"inputSchema": withProject(object{
			"at": object{"type": "array", "maxItems": 12, "items": object{"type": "number", "minimum": 0}, "description": "Times in seconds of the finished video. Default: 10%, 50% and 90%."},
		}),
		"annotations": annotations(false),
	},
	{
		"name": "wecut_render", "title": "Render video",
		"description": "Render the project to output.file (MP4 or GIF) inside the workspace, overwriting an earlier render with the same name. The result is checked with ffprobe. Sends progress notifications when the call has a progressToken.",
		"inputSchema": withProject(object{
			"preset": object{"type": "string", "enum": []string{"draft", "final", "high"}, "description": "draft = 540p fast check, final = default, high = slower and sharper."},
			"out":    object{"type": "string", "description": "Override output.file (must end in .mp4 or .gif)."},
		}),
		"annotations": annotations(false),
	},
	{
		"name": "wecut_list", "title": "List styles and names",
		"description": "Names usable in a project: text styles, caption styles, fonts, transitions, filters, effects, motions, ratios and text animations.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false,
			"properties": object{"what": object{"type": "string", "enum": []string{"styles", "caption-styles", "fonts", "transitions", "filters", "effects", "motions", "ratios", "text-animations"}}},
		},
		"annotations": annotations(true),
	},
	{
		"name": "wecut_schema", "title": "Project format",
		"description": "The JSON Schema of the WeCut project format, with a description of every field. what=recipe gives the recipe (template) format instead.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false,
			"properties": object{"what": object{"type": "string", "enum": []string{"project", "recipe"}}},
		},
		"annotations": annotations(true),
	},
	{
		"name": "wecut_example", "title": "Example project",
		"description": "A complete working project to copy from: podcast-clip (cuts + karaoke captions + ducked music), slideshow (photos with motion and transitions) or product-promo (contain + blur, logo, text). Omit name to list them.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false,
			"properties": object{"name": object{"type": "string", "enum": []string{"podcast-clip", "slideshow", "product-promo"}}},
		},
		"annotations": annotations(true),
	},
	{
		"name": "wecut_analyze", "title": "Analyse a video",
		"description": "Measure a video that worked: shots and pace, cuts in the first 3 s, flashes, beat and tempo, pauses, a silent platform outro, and (with a transcript) words per minute and the hook's words. Returns a sheet of frames, one per shot, as an image: look at it for the headline card, text style, framing and captions. Use the result to pick or write a recipe.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false, "required": []string{"file"},
			"properties": object{
				"file":       object{"type": "string", "description": "Video in the workspace."},
				"transcript": object{"type": "string", "description": "Word timings for it (.json, .srt, .vtt), optional."},
				"frames":     object{"type": "integer", "minimum": 1, "maximum": 24, "description": "Frames on the sheet (default 12)."},
			},
		},
		"annotations": annotations(false),
	},
	{
		"name": "wecut_clone", "title": "Turn a video into a template",
		"description": "Clone a finished video into a reusable template (a recipe with exact shots, like a CapCut template): every cut and transition (flash, fade, dissolve, zoom, blur), each shot's framing (full screen, or shown whole on black or blurred bars) and zoom, the text cards with their colours, box, font size, line spacing and timing, the caption style and position, the logo slot, and the original's sound. Saves recipes/<name>.json (and recipes/<name>/music.m4a when the sound is music), and returns the media slots, the comparison sheet (original left, template right) and the text crops as images. Words are read with Tesseract when installed; otherwise read the crops yourself and run again with read. Copies structure only: the footage and words of the original never go into the template.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false, "required": []string{"file"},
			"properties": object{
				"file":  object{"type": "string", "description": "The finished video, in the workspace."},
				"name":  object{"type": "string", "description": "Template name (default: the file name)."},
				"read":  object{"type": "object", "description": "Words you read from the crops, by crop id: {\"text1\": [\"first line\", \"second line\"], \"caption1\": [\"WORD\"]}. Line breaks are kept.", "additionalProperties": object{"type": "array", "items": object{"type": "string"}}},
				"force": object{"type": "boolean", "description": "Replace a recipe of the same name."},
			},
		},
		"annotations": annotations(false),
	},
	{
		"name": "wecut_recipe_list", "title": "List recipes",
		"description": "Recipes are templates: the shape of an edit without its content (sections such as hook and body, text slots, caption and music style). Lists the built-in ones and the workspace's own (recipes/*.json).",
		"inputSchema": noArgs, "annotations": annotations(true),
	},
	{
		"name": "wecut_recipe_show", "title": "Show a recipe",
		"description": "A recipe in full, with the roles its clips can feed, its text slots, and its default music.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false, "required": []string{"name"},
			"properties": object{"name": object{"type": "string", "description": "Recipe name or workspace .json path."}},
		},
		"annotations": annotations(true),
	},
	{
		"name": "wecut_recipe_apply", "title": "Apply a recipe",
		"description": "Turn a recipe plus new material into a project: clips (each with an optional role such as hook or body), words for the text slots, a transcript for captions, and music (replaces the recipe's own track; \"none\" drops it). Returns the project (saved with write); then wecut_preview and wecut_render it.",
		"inputSchema": object{
			"type": "object", "additionalProperties": false, "required": []string{"recipe", "clips"},
			"properties": object{
				"recipe": object{"type": []string{"string", "object"}, "description": "Built-in name, workspace .json path, or a recipe object (wecut.recipe/1)."},
				"clips": object{"type": "array", "minItems": 1, "items": object{
					"type": "object", "required": []string{"src"}, "additionalProperties": false,
					"properties": object{
						"src":   object{"type": "string"},
						"from":  object{"type": "number", "minimum": 0},
						"to":    object{"type": "number"},
						"role":  object{"type": "string", "description": "Section it feeds; default body (or the last section)."},
						"focus": object{"type": "object", "properties": object{"x": object{"type": "number"}, "y": object{"type": "number"}}},
					},
				}},
				"texts": object{"type": "object", "description": "Words per slot, e.g. {\"headline\": {\"text\": \"...\", \"emphasis\": [\"key words\"]}}.", "additionalProperties": object{
					"type": "object", "required": []string{"text"},
					"properties": object{"text": object{"type": "string"}, "emphasis": object{"type": "array", "items": object{"type": "string"}}},
				}},
				"transcript": object{"type": "string", "description": "Word timings of captionSrc (or of the source used longest)."},
				"captionSrc": object{"type": "string"},
				"music":      object{"type": "string", "description": "Audio file, a video whose sound is used, or \"none\"."},
				"overlays":   object{"type": "object", "description": "A picture per overlay slot, e.g. {\"logo\": \"media/logo.png\"}; \"none\" leaves it out.", "additionalProperties": object{"type": "string"}},
				"fill":       object{"type": "string", "enum": []string{"auto", "each", "sequence"}, "description": "Exact-shot templates (from wecut_clone): each = one clip per slot, sequence = clips play on through the slots, auto (default) = each when there are enough clips."},
				"output":     object{"type": "string", "description": "Render target (.mp4 or .gif)."},
				"write":      object{"type": "string", "description": "Save the project here (.json)."},
			},
		},
		"annotations": annotations(false),
	},
	{
		"name": "wecut_recipe_from_project", "title": "Save a project as a recipe",
		"description": "Make a reusable template from a finished project: sections, transitions, text slots (current words kept as defaults), caption style, effects, and its music as the default track.",
		"inputSchema": withProject(object{
			"name":  object{"type": "string"},
			"write": object{"type": "string", "description": "Save it here, e.g. recipes/my-style.json, so wecut_recipe_list finds it."},
		}),
		"annotations": annotations(false),
	},
}

type toolArgs struct {
	Project     json.RawMessage `json:"project"`
	ProjectFile string          `json:"projectFile"`
	Files       []string        `json:"files"`
	At          []float64       `json:"at"`
	Preset      string          `json:"preset"`
	Out         string          `json:"out"`
	What        string          `json:"what"`
	Name        string          `json:"name"`
	File        string          `json:"file"`
	Transcript  string          `json:"transcript"`
	Frames      int             `json:"frames"`
	Write       string          `json:"write"`
	Read        json.RawMessage `json:"read"`
	Force       bool            `json:"force"`
}

func toolError(msg string) map[string]any {
	return map[string]any{"content": []object{{"type": "text", "text": msg}}, "isError": true}
}

// textResult returns CLI JSON as both text and structured content.
func textResult(out []byte, code int) map[string]any {
	out = bytes.TrimSpace(out)
	res := map[string]any{"content": []object{{"type": "text", "text": string(out)}}, "isError": code != 0}
	var structured map[string]any
	if json.Unmarshal(out, &structured) == nil {
		res["structuredContent"] = structured
	}
	return res
}

func (s *Server) callTool(ctx context.Context, name string, raw json.RawMessage, token json.RawMessage) (map[string]any, *rpcError) {
	if name == "wecut_recipe_apply" {
		// The arguments are the apply request itself; the CLI checks them
		// strictly and points at any mistake.
		if len(raw) == 0 || string(raw) == "null" {
			return toolError("give recipe and clips"), nil
		}
		return textResult(s.runCLI(ctx, raw, nil, "recipe", "apply", "--input", "-")), nil
	}
	var a toolArgs
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&a); err != nil {
			return toolError("invalid arguments: " + err.Error()), nil
		}
	}

	project := func() ([]byte, string, map[string]any) {
		hasInline := len(a.Project) > 0 && string(a.Project) != "null"
		switch {
		case hasInline && a.ProjectFile != "":
			return nil, "", toolError("give either project or projectFile, not both")
		case hasInline:
			return a.Project, "-", nil
		case a.ProjectFile != "":
			return nil, a.ProjectFile, nil
		}
		return nil, "", toolError("give the project as an object in \"project\", or a file path in \"projectFile\"")
	}

	switch name {
	case "wecut_doctor":
		return textResult(s.runCLI(ctx, nil, nil, "doctor")), nil
	case "wecut_probe":
		if len(a.Files) == 0 {
			return toolError("files is required"), nil
		}
		return textResult(s.runCLI(ctx, nil, nil, append([]string{"probe"}, a.Files...)...)), nil
	case "wecut_list":
		args := []string{"list"}
		if a.What != "" {
			args = append(args, a.What)
		}
		return textResult(s.runCLI(ctx, nil, nil, args...)), nil
	case "wecut_schema":
		args := []string{"schema"}
		switch a.What {
		case "", "project":
		case "recipe":
			args = []string{"recipe", "schema"}
		default:
			return toolError(`what is "project" or "recipe"`), nil
		}
		out, code := s.runCLI(ctx, nil, nil, args...)
		return map[string]any{"content": []object{{"type": "text", "text": string(out)}}, "isError": code != 0}, nil
	case "wecut_example":
		if a.Name == "" {
			return textResult(s.runCLI(ctx, nil, nil, "example")), nil
		}
		out, code := s.runCLI(ctx, nil, nil, "example", a.Name)
		if code != 0 {
			return textResult(out, code), nil
		}
		return map[string]any{"content": []object{{"type": "text", "text": string(out)}}, "isError": false}, nil
	case "wecut_analyze":
		if a.File == "" {
			return toolError("file is required"), nil
		}
		args := []string{"analyze", a.File}
		if a.Transcript != "" {
			args = append(args, "--transcript", a.Transcript)
		}
		if a.Frames > 0 {
			args = append(args, "--frames", strconv.Itoa(a.Frames))
		}
		s.heavyMu.Lock()
		out, code := s.runCLI(ctx, nil, nil, args...)
		s.heavyMu.Unlock()
		res := textResult(out, code)
		if sc, ok := res["structuredContent"].(map[string]any); ok && code == 0 {
			if sheet, ok := sc["sheet"].(string); ok {
				if data, err := os.ReadFile(filepath.Join(s.cfg.Root, filepath.FromSlash(sheet))); err == nil {
					res["content"] = append(res["content"].([]object), object{"type": "image", "mimeType": "image/jpeg", "data": base64.StdEncoding.EncodeToString(data)})
				}
			}
		}
		return res, nil
	case "wecut_clone":
		if a.File == "" {
			return toolError("file is required"), nil
		}
		args := []string{"clone", a.File}
		if a.Name != "" {
			args = append(args, "--name", a.Name)
		}
		if a.Force {
			args = append(args, "--force")
		}
		if len(a.Read) > 0 && string(a.Read) != "null" {
			// The words go in through a file the CLI reads like any input.
			f, err := os.CreateTemp(s.cfg.Root, ".wecut-read-*.json")
			if err != nil {
				return toolError(err.Error()), nil
			}
			f.Write(a.Read)
			f.Close()
			defer os.Remove(f.Name())
			args = append(args, "--read", filepath.Base(f.Name()))
		}
		s.heavyMu.Lock()
		out, code := s.runCLI(ctx, nil, nil, args...)
		s.heavyMu.Unlock()
		res := textResult(out, code)
		if sc, ok := res["structuredContent"].(map[string]any); ok && code == 0 {
			var files []string
			for _, k := range []string{"compare", "sheet"} {
				if f, ok := sc[k].(string); ok {
					files = append(files, f)
				}
			}
			if texts, ok := sc["texts"].([]any); ok {
				for _, t := range texts {
					if m, ok := t.(map[string]any); ok {
						if f, ok := m["crop"].(string); ok && len(files) < 8 {
							files = append(files, f)
						}
					}
				}
			}
			for _, f := range files {
				data, err := os.ReadFile(filepath.Join(s.cfg.Root, filepath.FromSlash(f)))
				if err != nil {
					continue
				}
				mime := "image/jpeg"
				if strings.HasSuffix(f, ".png") {
					mime = "image/png"
				}
				res["content"] = append(res["content"].([]object), object{"type": "image", "mimeType": mime, "data": base64.StdEncoding.EncodeToString(data)})
			}
		}
		return res, nil
	case "wecut_recipe_list":
		return textResult(s.runCLI(ctx, nil, nil, "recipe", "list")), nil
	case "wecut_recipe_pack", "wecut_recipe_unpack":
		if a.File == "" || a.Write == "" {
			return toolError("file and write are required"), nil
		}
		command := "pack"
		if name == "wecut_recipe_unpack" {
			command = "unpack"
		}
		s.heavyMu.Lock()
		out, code := s.runCLI(ctx, nil, nil, "recipe", command, a.File, "--write", a.Write)
		s.heavyMu.Unlock()
		return textResult(out, code), nil
	case "wecut_recipe_show":
		if a.Name == "" {
			return toolError("name is required"), nil
		}
		return textResult(s.runCLI(ctx, nil, nil, "recipe", "show", a.Name)), nil
	case "wecut_recipe_from_project":
		stdin, arg, bad := project()
		if bad != nil {
			return bad, nil
		}
		args := []string{"recipe", "from-project", arg}
		if a.Name != "" {
			args = append(args, "--name", a.Name)
		}
		if a.Write != "" {
			args = append(args, "--write", a.Write)
		}
		return textResult(s.runCLI(ctx, stdin, nil, args...)), nil
	case "wecut_validate":
		stdin, arg, bad := project()
		if bad != nil {
			return bad, nil
		}
		return textResult(s.runCLI(ctx, stdin, nil, "validate", arg)), nil
	case "wecut_preview":
		stdin, arg, bad := project()
		if bad != nil {
			return bad, nil
		}
		args := []string{"preview", arg}
		if len(a.At) > 0 {
			parts := make([]string, len(a.At))
			for i, t := range a.At {
				parts[i] = strconv.FormatFloat(t, 'f', -1, 64)
			}
			args = append(args, "--at", strings.Join(parts, ","))
		}
		s.heavyMu.Lock()
		out, code := s.runCLI(ctx, stdin, nil, args...)
		s.heavyMu.Unlock()
		return s.previewResult(out, code), nil
	case "wecut_render":
		stdin, arg, bad := project()
		if bad != nil {
			return bad, nil
		}
		args := []string{"render", arg}
		if a.Preset != "" {
			args = append(args, "--preset", a.Preset)
		}
		if a.Out != "" {
			args = append(args, "--out", a.Out)
		}
		var progress io.Writer
		if token != nil {
			args = append(args, "--progress")
			progress = &progressWriter{s: s, token: token}
		}
		s.heavyMu.Lock()
		out, code := s.runCLI(ctx, stdin, progress, args...)
		s.heavyMu.Unlock()
		return textResult(out, code), nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown tool: " + name}
}

// previewResult returns the CLI's JSON plus each frame as an image.
func (s *Server) previewResult(out []byte, code int) map[string]any {
	res := textResult(out, code)
	if code != 0 {
		return res
	}
	var r struct {
		Frames []struct {
			T    float64 `json:"t"`
			File string  `json:"file"`
		} `json:"frames"`
	}
	if json.Unmarshal(out, &r) != nil {
		return res
	}
	content := res["content"].([]object)
	for _, f := range r.Frames {
		path := filepath.Join(s.cfg.Root, filepath.FromSlash(f.File))
		data, err := os.ReadFile(path)
		if err != nil {
			content = append(content, object{"type": "text", "text": fmt.Sprintf("frame at %g s could not be read: %v", f.T, err)})
			continue
		}
		content = append(content,
			object{"type": "text", "text": fmt.Sprintf("Frame at %g s:", f.T)},
			object{"type": "image", "mimeType": "image/jpeg", "data": base64.StdEncoding.EncodeToString(data)})
	}
	res["content"] = content
	return res
}

// progressWriter turns the CLI's progress lines on stderr into MCP progress
// notifications. Progress only ever increases, as the spec requires.
type progressWriter struct {
	s     *Server
	token json.RawMessage
	mu    sync.Mutex
	buf   []byte
	last  float64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			break
		}
		line := p.buf[:i]
		p.buf = p.buf[i+1:]
		var ev struct {
			Event   string  `json:"event"`
			Percent float64 `json:"percent"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Event != "progress" || ev.Percent <= p.last {
			continue
		}
		p.last = ev.Percent
		p.s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{
			"progressToken": p.token, "progress": ev.Percent, "total": 100, "message": fmt.Sprintf("rendering %.0f%%", ev.Percent),
		}})
	}
	return len(b), nil
}
