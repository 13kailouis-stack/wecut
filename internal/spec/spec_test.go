package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

func codes(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Code+" "+i.Path)
	}
	return out
}

func TestDecodeReportsEveryShapeProblem(t *testing.T) {
	_, issues := Decode([]byte(`{"wecut":"1","clips":[{"src":"a.mp4","frm":2,"to":"5"}],"texts":[{"txt":"hi"}],"outputs":{}}`))
	got := codes(issues)
	want := []string{"E_SCHEMA /clips/0/frm", "E_SCHEMA /clips/0/to", "E_SCHEMA /outputs", "E_SCHEMA /texts/0/txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	hints := map[string]string{}
	for _, i := range issues {
		hints[i.Path] = i.Hint
	}
	if hints["/clips/0/frm"] != `Did you mean "from"?` || hints["/outputs"] != `Did you mean "output"?` {
		t.Errorf("suggestions: %v", hints)
	}
	if !strings.Contains(hints["/clips/0/to"], "without quotes") {
		t.Errorf("number-as-string hint: %q", hints["/clips/0/to"])
	}
}

func TestDecodeAcceptsEmbeddedLookFieldsAndNulls(t *testing.T) {
	p, issues := Decode([]byte(`{"wecut":"1","clips":[{"src":"a.mp4","to":null}],"texts":[{"text":"x","style":"hook","font":"anton","uppercase":true}],"captions":{"words":[{"word":"a","start":0,"end":1}],"style":"karaoke","highlight":"#FF0000"}}`))
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if p.Texts[0].Font != "anton" || p.Captions.Style != "karaoke" || p.Clips[0].To != nil {
		t.Fatalf("decoded %+v", p)
	}
}

func TestDecodeRejectsBadJSON(t *testing.T) {
	for _, in := range []string{`{`, `{"wecut":"1"} {}`, `[1]`} {
		_, issues := Decode([]byte(in))
		if len(issues) == 0 {
			t.Errorf("%s: no issues", in)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name, json string
		want       []string
	}{
		{"ok", `{"wecut":"1","clips":[{"src":"a.mp4"}]}`, nil},
		{"byte order mark", "\xef\xbb\xbf" + `{"wecut":"1","clips":[{"src":"a.mp4"}]}`, nil},
		{"version", `{"wecut":"2","clips":[{"src":"a.mp4"}]}`, []string{"E_SCHEMA /wecut"}},
		{"no clips", `{"wecut":"1","clips":[]}`, []string{"E_VALUE /clips"}},
		{"url", `{"wecut":"1","clips":[{"src":"https://x.com/a.mp4"}]}`, []string{"E_PATH /clips/0/src"}},
		{"ext", `{"wecut":"1","clips":[{"src":"a.txt"}]}`, []string{"E_MEDIA /clips/0/src"}},
		{"percent", `{"wecut":"1","clips":[{"src":"a%d.png"}]}`, []string{"E_PATH /clips/0/src"}},
		{"ranges", `{"wecut":"1","clips":[{"src":"a.mp4","from":5,"to":2,"speed":9,"rotate":45}]}`, []string{"E_VALUE /clips/0/to", "E_VALUE /clips/0/speed", "E_VALUE /clips/0/rotate"}},
		{"last transition", `{"wecut":"1","clips":[{"src":"a.mp4","transition":{"type":"fade"}}]}`, []string{"E_VALUE /clips/0/transition"}},
		{"bad transition", `{"wecut":"1","clips":[{"src":"a.mp4","transition":{"type":"spin"}},{"src":"b.mp4"}]}`, []string{"E_VALUE /clips/0/transition/type"}},
		{"text", `{"wecut":"1","clips":[{"src":"a.mp4"}],"texts":[{"text":" ","color":"red","style":"nope","in":{"type":"spin"},"end":0}]}`, []string{"E_VALUE /texts/0/text", "E_VALUE /texts/0/end", "E_VALUE /texts/0/style", "E_VALUE /texts/0/color", "E_VALUE /texts/0/in/type"}},
		{"caption style is not a text style", `{"wecut":"1","clips":[{"src":"a.mp4"}],"texts":[{"text":"x","style":"karaoke"}]}`, []string{"E_VALUE /texts/0/style"}},
		{"captions", `{"wecut":"1","clips":[{"src":"a.mp4"}],"captions":{"words":"t.txt","mode":"x"}}`, []string{"E_MEDIA /captions/words", "E_VALUE /captions/mode"}},
		{"captions missing words", `{"wecut":"1","clips":[{"src":"a.mp4"}],"captions":{}}`, []string{"E_VALUE /captions/words"}},
		{"output", `{"wecut":"1","output":{"file":"x.mov","ratio":"5:1","res":100,"fps":120,"background":"red"},"clips":[{"src":"a.mp4"}]}`, []string{"E_VALUE /output/file", "E_VALUE /output/ratio", "E_VALUE /output/res", "E_VALUE /output/fps", "E_VALUE /output/background"}},
		{"audio", `{"wecut":"1","clips":[{"src":"a.mp4"}],"audio":[{"src":"m.mp3","from":3,"to":1,"volume":500}]}`, []string{"E_VALUE /audio/0/to", "E_VALUE /audio/0/volume"}},
		{"filters", `{"wecut":"1","clips":[{"src":"a.mp4"}],"filters":[{"name":"sepia"}],"effects":[{"name":"warm"}]}`, []string{"E_VALUE /filters/0/name", "E_VALUE /effects/0/name"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, issues := Decode([]byte(c.json))
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			got := codes(Validate(p))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The JSON Schema is hand-written for agents to read; keep it in step with
// the Go types so `wecut schema` never lies.
func TestSchemaMatchesTypes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "wecut.project.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]any `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	props := func(names ...string) []string {
		var out []string
		for _, n := range names {
			if n == "" {
				for k := range s.Properties {
					out = append(out, k)
				}
				continue
			}
			def, ok := s.Defs[n]
			if !ok {
				t.Fatalf("schema has no $defs/%s", n)
			}
			for k := range def.Properties {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return slices.Compact(out)
	}
	check := func(name string, v any, defs ...string) {
		if got, want := props(defs...), JSONFieldNames(v); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: schema has %v, Go has %v", name, got, want)
		}
	}
	check("project", Project{}, "")
	check("output", Output{}, "output")
	check("clip", Clip{}, "clip")
	check("text", Text{}, "text", "look")
	check("captions", Captions{}, "captions", "look")
	check("overlay", Overlay{}, "overlay")
	check("track", Track{}, "track")
	check("filter", Filter{}, "filter")
	check("effect", Effect{}, "effect")
	check("gradient", Gradient{}, "gradient")
}

func TestCatalogKeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Fonts {
		if seen["font "+f.Key] || f.Em < 1 {
			t.Errorf("font %s", f.Key)
		}
		seen["font "+f.Key] = true
	}
	for _, s := range TextStyles {
		if seen["text "+s.Key] {
			t.Errorf("duplicate text style %s", s.Key)
		}
		seen["text "+s.Key] = true
		if _, ok := FontByKey(s.Font); !ok {
			t.Errorf("text style %s uses unknown font %s", s.Key, s.Font)
		}
	}
	for _, s := range CaptionStyles {
		if seen["cap "+s.Key] {
			t.Errorf("duplicate caption style %s", s.Key)
		}
		seen["cap "+s.Key] = true
		if _, ok := FontByKey(s.Font); !ok {
			t.Errorf("caption style %s uses unknown font %s", s.Key, s.Font)
		}
		if !slices.Contains(CaptionModes, s.Mode) {
			t.Errorf("caption style %s has mode %s", s.Key, s.Mode)
		}
	}
}
