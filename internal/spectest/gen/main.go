// Command gen compiles the spec test suite with gosm and runs it.
//
//	go run ./internal/spectest/gen -wast <json-from-wast output root> [-run regexp] [-v]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"gosm/internal/compiler"
	"gosm/internal/validate"
	"gosm/internal/wasm"
)

var (
	wastDir = flag.String("wast", "", "directory of converted spec tests (one subdirectory per .wast)")
	outDir  = flag.String("out", "internal/spectest/_out", "output directory (inside the gosm module)")
	runRe   = flag.String("run", "", "only run tests matching this regexp")
	verbose = flag.Bool("v", false, "print failures")
	jobs    = flag.Int("j", runtime.NumCPU()/2, "parallel tests")
	multi   = flag.Bool("multi", false, "compile all modules of a script into one package (shared GC types)")
)

type cmd struct {
	Type       string `json:"type"`
	Filename   string `json:"filename"`
	ModuleType string `json:"module_type"`
	Commands   []cmd  `json:"commands"`
}

// flatten includes commands nested in thread blocks.
func flatten(cs []cmd) []cmd {
	var out []cmd
	for _, c := range cs {
		out = append(out, c)
		out = append(out, flatten(c.Commands)...)
	}
	return out
}

type summary struct {
	Name                    string
	Passed, Failed, Skipped int
	Failures                []string
	Err                     string
}

func main() {
	flag.Parse()
	dirs, _ := filepath.Glob(filepath.Join(*wastDir, "*"))
	re := regexp.MustCompile(*runRe)
	var names []string
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "test.json")); err == nil && re.MatchString(filepath.Base(d)) {
			names = append(names, filepath.Base(d))
		}
	}
	sort.Strings(names)
	results := make([]summary, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(*jobs, 1))
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runTest(n)
		}(i, n)
	}
	wg.Wait()
	var tp, tf, ts int
	for _, r := range results {
		tp += r.Passed
		tf += r.Failed
		ts += r.Skipped
		status := "ok  "
		if r.Failed > 0 || r.Err != "" {
			status = "FAIL"
		}
		fmt.Printf("%s %-45s pass %5d fail %5d skip %5d %s\n", status, r.Name, r.Passed, r.Failed, r.Skipped, firstLine(r.Err))
		if *verbose {
			for i, f := range r.Failures {
				if i >= 15 {
					fmt.Printf("      ... %d more\n", len(r.Failures)-i)
					break
				}
				fmt.Printf("      %s\n", f)
			}
			if r.Err != "" {
				fmt.Printf("      %s\n", truncate(r.Err, 3000))
			}
		}
	}
	fmt.Printf("TOTAL pass %d fail %d skip %d\n", tp, tf, ts)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncate(s, 120)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func runTest(name string) summary {
	sum := summary{Name: name}
	src := filepath.Join(*wastDir, name)
	b, err := os.ReadFile(filepath.Join(src, "test.json"))
	if err != nil {
		sum.Err = err.Error()
		return sum
	}
	var s struct{ Commands []cmd }
	json.Unmarshal(b, &s)
	s.Commands = flatten(s.Commands)
	dir := filepath.Join(*outDir, name)
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	type mod struct{ file, pkg, ctor string }
	var multiSrcs []compiler.Source
	var mods []mod
	seen := map[string]bool{}
	var compileErrs []string
	// validation: invalid and malformed binary modules must be rejected
	var vpass, vfail int
	var vfailures []string
	for _, c := range s.Commands {
		if (c.Type != "assert_invalid" && c.Type != "assert_malformed") || c.ModuleType == "text" || c.Filename == "" {
			continue
		}
		wb, err := os.ReadFile(filepath.Join(src, c.Filename))
		if err != nil {
			continue
		}
		m, err := wasm.Decode(wb)
		if err == nil {
			err = validate.Validate(m)
		}
		if err != nil {
			vpass++
		} else {
			vfail++
			vfailures = append(vfailures, fmt.Sprintf("%s (%s): accepted an invalid module", c.Filename, c.Type))
		}
	}
	for _, c := range s.Commands {
		if c.Filename == "" || seen[c.Filename] {
			continue
		}
		switch c.Type {
		case "module", "module_definition", "assert_trap", "assert_exhaustion", "assert_uninstantiable", "assert_unlinkable":
		default:
			continue
		}
		seen[c.Filename] = true
		pkg := fmt.Sprintf("m%d", len(mods))
		var wb []byte
		if c.ModuleType == "text" {
			if c.Type != "module" && c.Type != "module_definition" {
				continue
			}
			out, err := exec.Command("wasm-tools", "parse", filepath.Join(src, c.Filename)).Output()
			if err != nil {
				compileErrs = append(compileErrs, c.Filename+": wasm-tools parse failed")
				continue
			}
			wb = out
		} else {
			var err error
			wb, err = os.ReadFile(filepath.Join(src, c.Filename))
			if err != nil {
				continue
			}
		}
		m, err := wasm.Decode(wb)
		if err == nil {
			err = validate.Validate(m)
		}
		if err != nil {
			compileErrs = append(compileErrs, c.Filename+": "+err.Error())
			continue
		}
		if *multi {
			name := fmt.Sprintf("M%d", len(multiSrcs))
			multiSrcs = append(multiSrcs, compiler.Source{Name: name, Module: m})
			mods = append(mods, mod{c.Filename, "mods", "New" + name + "Dynamic"})
			continue
		}
		code, err := compiler.Compile(m, compiler.Options{Package: pkg, Source: c.Filename})
		if err != nil {
			compileErrs = append(compileErrs, c.Filename+": "+err.Error())
			continue
		}
		os.MkdirAll(filepath.Join(dir, pkg), 0o755)
		os.WriteFile(filepath.Join(dir, pkg, pkg+".go"), code, 0o644)
		mods = append(mods, mod{c.Filename, pkg, "NewDynamic"})
	}
	if *multi && len(multiSrcs) > 0 {
		code, err := compiler.CompileModules(multiSrcs, compiler.Options{Package: "mods", Source: name})
		if err != nil {
			sum.Err = "compile: " + err.Error()
			return sum
		}
		os.MkdirAll(filepath.Join(dir, "mods"), 0o755)
		os.WriteFile(filepath.Join(dir, "mods", "mods.go"), code, 0o644)
		if len(multiSrcs) == 1 {
			mods[0].ctor = "NewDynamic"
		}
	}
	abs, _ := filepath.Abs(filepath.Join(src, "test.json"))
	modPath := "gosm/" + filepath.ToSlash(dir)
	for attempt := 0; ; attempt++ {
		var mb bytes.Buffer
		mb.WriteString("package main\n\nimport (\n\t\"encoding/json\"\n\t\"os\"\n\t\"runtime/debug\"\n\n\t\"gosm/internal/spectest/runner\"\n")
		imported := map[string]bool{}
		for _, m := range mods {
			if !imported[m.pkg] {
				imported[m.pkg] = true
				fmt.Fprintf(&mb, "\t%s %q\n", m.pkg, modPath+"/"+m.pkg)
			}
		}
		mb.WriteString(")\n\nfunc main() {\n\tdebug.SetMaxStack(4 << 30)\n\tctors := map[string]runner.Ctor{\n")
		for _, m := range mods {
			fmt.Fprintf(&mb, "\t\t%q: func(r func(string, string) any) (map[string]any, error) {\n\t\t\ti, err := %s.%s(r)\n\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n\t\t\treturn i.Exports(), nil\n\t\t},\n", m.file, m.pkg, m.ctor)
		}
		fmt.Fprintf(&mb, "\t}\n\tres := runner.Run(%q, ctors)\n\tjson.NewEncoder(os.Stdout).Encode(res)\n}\n", abs)
		os.WriteFile(filepath.Join(dir, "main.go"), mb.Bytes(), 0o644)
		exe := filepath.Join(dir, "run.exe")
		out, err := exec.Command("go", "build", "-o", exe, "./"+filepath.ToSlash(dir)).CombinedOutput()
		if err != nil {
			// drop packages that failed to compile and retry
			bad := map[string]bool{}
			for _, m := range mods {
				if bytes.Contains(out, []byte(filepath.Join(dir, m.pkg)+"/")) || bytes.Contains(out, []byte("/"+m.pkg+"/"+m.pkg+".go")) {
					bad[m.pkg] = true
				}
			}
			if len(bad) == 0 || attempt > 3 {
				sum.Err = "build: " + string(out)
				return sum
			}
			var keep []mod
			for _, m := range mods {
				if bad[m.pkg] {
					compileErrs = append(compileErrs, m.file+": go build failed")
				} else {
					keep = append(keep, m)
				}
			}
			sum.Err += string(out)
			mods = keep
			continue
		}
		run := exec.Command(exe)
		var stdout, stderr bytes.Buffer
		run.Stdout = &stdout
		run.Stderr = &stderr
		err = run.Run()
		var res struct {
			Passed, Failed, Skipped int
			Failures                []string
		}
		if jerr := json.Unmarshal(stdout.Bytes(), &res); jerr != nil {
			sum.Err += fmt.Sprintf("run: %v\n%s", err, truncate(stderr.String(), 2000))
			return sum
		}
		sum.Passed, sum.Failed, sum.Skipped = res.Passed+vpass, res.Failed+vfail, res.Skipped-vpass-vfail
		sum.Failures = append(append(compileErrs, vfailures...), res.Failures...)
		if len(compileErrs) > 0 && sum.Err == "" {
			sum.Err = fmt.Sprintf("%d modules failed to compile", len(compileErrs))
		}
		return sum
	}
}
