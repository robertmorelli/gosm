package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gosm/internal/wasm"
)

type specCmd struct {
	Type       string `json:"type"`
	Line       int    `json:"line"`
	Filename   string `json:"filename"`
	Text       string `json:"text"`
	ModuleType string `json:"module_type"`
}

// TestSpec runs the validator over converted spec tests. Set
// GOSM_SPECTEST_JSON to a directory containing <test>/test.json files
// (wasm-tools json-from-wast format).
func TestSpec(t *testing.T) {
	dir := os.Getenv("GOSM_SPECTEST_JSON")
	if dir == "" {
		t.Skip("GOSM_SPECTEST_JSON not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*", "test.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test.json files under %s", dir)
	}
	sort.Strings(files)
	var valid, validOK, invalid, invalidOK, malformed, malformedOK int
	var failures []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tf struct {
			Commands []specCmd `json:"commands"`
		}
		if err := json.Unmarshal(b, &tf); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		tdir := filepath.Dir(f)
		name := filepath.Base(tdir)
		for _, c := range tf.Commands {
			if c.Filename == "" || !strings.HasSuffix(c.Filename, ".wasm") {
				continue
			}
			if c.ModuleType != "" && c.ModuleType != "binary" {
				continue
			}
			var want int // 1 = valid, 2 = invalid, 3 = malformed
			switch c.Type {
			case "module", "module_definition", "assert_trap", "assert_exhaustion",
				"assert_uninstantiable", "assert_unlinkable":
				want = 1
			case "assert_invalid":
				want = 2
			case "assert_malformed":
				want = 3
			default:
				continue
			}
			wb, err := os.ReadFile(filepath.Join(tdir, c.Filename))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.Decode(wb)
			if err == nil {
				err = Validate(m)
			}
			loc := name + "/" + c.Filename
			switch want {
			case 1:
				valid++
				if err == nil {
					validOK++
				} else {
					failures = append(failures, loc+": valid module rejected: "+err.Error())
				}
			case 2:
				invalid++
				if err != nil {
					invalidOK++
				} else {
					failures = append(failures, loc+": invalid module accepted (want "+c.Text+")")
				}
			case 3:
				malformed++
				if err != nil {
					malformedOK++
				} else {
					failures = append(failures, loc+": malformed module accepted (want "+c.Text+")")
				}
			}
		}
	}
	for _, f := range failures {
		t.Error(f)
	}
	t.Logf("valid: %d/%d, invalid rejected: %d/%d, malformed rejected: %d/%d",
		validOK, valid, invalidOK, invalid, malformedOK, malformed)
}

// TestValidDir checks that every .wasm file in GOSM_VALID_WASM_DIR (for
// example output of wasm-tools smith) decodes and validates.
func TestValidDir(t *testing.T) {
	dir := os.Getenv("GOSM_VALID_WASM_DIR")
	if dir == "" {
		t.Skip("GOSM_VALID_WASM_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.wasm"))
	ok := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := wasm.Decode(b)
		if err == nil {
			err = Validate(m)
		}
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		ok++
	}
	t.Logf("%d/%d valid", ok, len(files))
}
