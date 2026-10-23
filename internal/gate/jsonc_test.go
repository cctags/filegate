package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONCComments(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"plain", `{"value":1}`, `{"value":1}`},
		{"line comments", "// heading\r\n{\"value\": 1 // inline\r\n}\n// end", `{"value":1}`},
		{"block comments", "/* 中文\n说明 */ { /* key */ \"value\": /* value */ 1 } /* end */", `{"value":1}`},
		{"comment between tokens", `{"value"/* note */:/* note */1}`, `{"value":1}`},
		{"array", `{"value":[1,/* two */2,//three` + "\n3]}", `{"value":[1,2,3]}`},
		{"strings", `{"url":"https://example.com/a//b","text":"/* not a comment */ // still text","quote":"say \"//hello\"","path":"C:\\files\\","中文":"文件夹"}`, `{"url":"https://example.com/a//b","text":"/* not a comment */ // still text","quote":"say \"//hello\"","path":"C:\\files\\","中文":"文件夹"}`},
		{"CR line ending", "// heading\r{\"value\":1}//end", `{"value":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := stripJSONComments([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err = json.Unmarshal(output, &got); err != nil {
				t.Fatalf("invalid cleaned JSON: %s: %v", output, err)
			}
			if err = json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			normalized, _ := json.Marshal(got)
			expected, _ := json.Marshal(want)
			if !bytes.Equal(normalized, expected) {
				t.Fatalf("got %s; want %s", normalized, expected)
			}
			if len(output) != len(tc.input) {
				t.Fatal("byte offsets changed")
			}
			for i, b := range []byte(tc.input) {
				if b == '\n' || b == '\r' {
					if output[i] != b {
						t.Fatal("line ending changed")
					}
				}
			}
		})
	}
}

func TestJSONCRejectsMalformedSyntax(t *testing.T) {
	for _, input := range []string{`/* unfinished`, `{"a":1} /*`, `{"a":1,}`, `[1,]`, `{"a":1/* no joining numbers */2}`, `{"a":tr/* no joining keywords */ue}`, `{unquoted:1}`, `{'a':1}`, "{\"a\":\"bad\nstring\"}", `{"a":"unfinished\"}`, `{/* outer /* nested */ still outer */"a":1}`} {
		output, err := stripJSONComments([]byte(input))
		if err == nil && json.Valid(output) {
			t.Errorf("accepted invalid JSONC: %s", input)
		}
	}
}

func TestJSONCPreservesErrorOffset(t *testing.T) {
	input := []byte("/* 中文说明 */\n{\"a\": 1,\n \"b\": @}")
	output, err := stripJSONComments(input)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	err = json.Unmarshal(output, &value)
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("expected syntax error: %v", err)
	}
	if syntax.Offset != int64(bytes.IndexByte(input, '@')+1) {
		t.Fatalf("error location shifted: %d", syntax.Offset)
	}
}

func TestLoadJSONCAndStrictValidation(t *testing.T) {
	a := setup(t)
	file, _ := reloadConfig(t, a)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	withComments := append([]byte("// 配置说明\r\n/* block */\n"), data...)
	withComments = append(withComments, []byte("\n// end")...)
	for _, name := range []string{"config.json", "config.jsonc"} {
		p := filepath.Join(filepath.Dir(file), name)
		if err = os.WriteFile(p, withComments, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = Load(p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Comments must not hide a misspelled setting from strict field validation.
	unknown := bytes.Replace(withComments, []byte(`"code_ttl":`), []byte(`"code_ttl_typo":`), 1)
	os.WriteFile(file, unknown, 0600)
	if _, err = Load(file); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field must be rejected by strict decoding: %v", err)
	}
	// The size limit includes comments and is checked before stripping them.
	large := append(data, []byte("\n//")...)
	large = append(large, bytes.Repeat([]byte("x"), 1<<20)...)
	os.WriteFile(file, large, 0600)
	if _, err = Load(file); err == nil {
		t.Fatal("oversized config accepted")
	}
}

func TestReloadJSONC(t *testing.T) {
	a := setup(t)
	file, c := reloadConfig(t, a)
	cookie := access(t, a)
	c.CodeTTL = "45m"
	writeReloadConfig(t, file, c)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, append([]byte("// Updated settings\n"), data...), 0600)
	if _, err = a.Reload(file); err != nil {
		t.Fatal(err)
	}
	if request(a, "GET", "/file/hello.txt", "", cookie).Code != 200 {
		t.Fatal("commented reload invalidated unchanged session")
	}
	os.WriteFile(file, append(data, []byte("\n/* unfinished")...), 0600)
	if _, err = a.Reload(file); err == nil {
		t.Fatal("malformed commented reload accepted")
	}
	if a.cfg.CodeTTL != "45m" || request(a, "GET", "/file/hello.txt", "", cookie).Code != 200 {
		t.Fatal("failed reload affected prior configuration")
	}
}

func FuzzStripJSONComments(f *testing.F) {
	for _, seed := range []string{`{"url":"https://example.com"}`, "/* 中文 */\n{} //end", `{"quote":"\\\"//"}`, `/*`, "//\r\n{}"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		output, err := stripJSONComments(input)
		if !bytes.Equal(input, original) {
			t.Fatal("input mutated")
		}
		if json.Valid(input) && (err != nil || !bytes.Equal(output, input)) {
			t.Fatal("valid standard JSON changed")
		}
		if err != nil {
			return
		}
		if len(output) != len(input) {
			t.Fatal("offsets changed")
		}
		for i, b := range input {
			if b == '\n' || b == '\r' {
				if output[i] != b {
					t.Fatal("line ending changed")
				}
			}
		}
	})
}
