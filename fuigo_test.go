package fuigo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplitVersion(t *testing.T) {
	cases := []struct{ in, pkg, ver string }{
		{"github.com/x/y/cmd/z@latest", "github.com/x/y/cmd/z", "latest"},
		{"github.com/x/y/cmd/z@v1.2.3", "github.com/x/y/cmd/z", "v1.2.3"},
		{"github.com/x/y/cmd/z", "github.com/x/y/cmd/z", ""},
	}
	for _, c := range cases {
		pkg, ver := splitVersion(c.in)
		if pkg != c.pkg || ver != c.ver {
			t.Errorf("splitVersion(%q) = (%q,%q), want (%q,%q)", c.in, pkg, ver, c.pkg, c.ver)
		}
	}
}

func TestConfirm(t *testing.T) {
	cases := map[string]bool{
		"\n":    true, // default
		"y\n":   true,
		"yes\n": true,
		"n\n":   false,
		"no\n":  false,
		"":      true, // EOF → default yes
	}
	for input, want := range cases {
		ok, err := confirm(Options{In: strings.NewReader(input)})
		if err != nil {
			t.Fatalf("confirm(%q): %v", input, err)
		}
		if ok != want {
			t.Errorf("confirm(%q) = %v, want %v", input, ok, want)
		}
	}
}

// logCapture collects formatted log lines.
type logCapture struct{ lines []string }

func (l *logCapture) logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *logCapture) joined() string { return strings.Join(l.lines, "\n") }

// TestRunListShowsStepsWithoutExecuting verifies --list prints steps and does
// not execute them or install anything.
func TestRunListShowsStepsWithoutExecuting(t *testing.T) {
	zipData := buildModuleZip(t, "github.com/fuigotest/hello", "v1.0.0", map[string]string{
		"go.mod":            "module github.com/fuigotest/hello\n\ngo 1.26\n",
		"cmd/hello/main.go": "package main\n\nfunc main() {}\n",
		// This step would fail if executed (missing entry file), proving --list
		// does not run it.
		"fuigo.yaml": "steps:\n  - esbuild --entry does-not-exist.js --bundle --outfile out.js\n",
	})
	srv := newProxyServer(t, "github.com/fuigotest/hello", "v1.0.0", zipData)
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	bin := t.TempDir()
	setInstallEnv(t, bin)

	log := &logCapture{}
	err := Run(Options{
		Package: "github.com/fuigotest/hello/cmd/hello@latest",
		List:    true,
		Logf:    log.logf,
	})
	if err != nil {
		t.Fatalf("Run --list: %v", err)
	}
	if !strings.Contains(log.joined(), "esbuild --entry does-not-exist.js") {
		t.Errorf("steps not listed:\n%s", log.joined())
	}
	if strings.Contains(log.joined(), "step 1 complete") {
		t.Error("--list executed a step")
	}
	if entries, _ := os.ReadDir(bin); len(entries) != 0 {
		t.Error("--list installed a binary")
	}
}

// TestRunPlainInstallNoConfig verifies a module without fuigo.yaml is installed
// directly, end to end and offline.
func TestRunPlainInstallNoConfig(t *testing.T) {
	zipData := buildModuleZip(t, "github.com/fuigotest/plain", "v0.5.0", map[string]string{
		"go.mod":            "module github.com/fuigotest/plain\n\ngo 1.26\n",
		"cmd/plain/main.go": "package main\n\nfunc main() {}\n",
	})
	srv := newProxyServer(t, "github.com/fuigotest/plain", "v0.5.0", zipData)
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	bin := t.TempDir()
	setInstallEnv(t, bin)

	log := &logCapture{}
	err := Run(Options{
		Package: "github.com/fuigotest/plain/cmd/plain@latest",
		Yes:     true,
		Logf:    log.logf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(log.joined(), "no fuigo.yaml found") {
		t.Errorf("expected plain-install message:\n%s", log.joined())
	}
	assertBinary(t, bin, "plain")
}

// TestRunYesExecutesSteps verifies --yes runs steps then installs, without
// prompting.
func TestRunYesExecutesSteps(t *testing.T) {
	zipData := buildModuleZip(t, "github.com/fuigotest/withsteps", "v1.0.0", map[string]string{
		"go.mod":          "module github.com/fuigotest/withsteps\n\ngo 1.26\n",
		"cmd/app/main.go": "package main\n\nfunc main() {}\n",
		// Mixed string + map(workdir) steps exercised through the full Run path.
		"fuigo.yaml": "steps:\n  - go env GOOS\n  - command: go env GOARCH\n    workdir: cmd/app\n",
	})
	srv := newProxyServer(t, "github.com/fuigotest/withsteps", "v1.0.0", zipData)
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	bin := t.TempDir()
	setInstallEnv(t, bin)

	log := &logCapture{}
	err := Run(Options{
		Package: "github.com/fuigotest/withsteps/cmd/app@latest",
		Yes:     true,
		Logf:    log.logf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(log.joined(), "step 1 complete") {
		t.Errorf("step did not run:\n%s", log.joined())
	}
	assertBinary(t, bin, "app")
}

func TestIsLocalPath(t *testing.T) {
	cases := map[string]bool{
		".":                    true,
		"..":                   true,
		"./x":                  true,
		"../x":                 true,
		"/abs/path":            true,
		"mymod":                true,
		"cmd/foo":              true,
		"github.com/x/y":       false,
		"example.com/m@v1.0.0": false,
		"":                     false,
	}
	for in, want := range cases {
		if got := isLocalPath(in); got != want {
			t.Errorf("isLocalPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDetectCmdPackages(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "cmd/a/main.go", "package main\nfunc main() {}\n")
	write(t, dir, "cmd/b/main.go", "package main\nfunc main() {}\n")
	write(t, dir, "cmd/lib/helper.go", "package lib\n") // no main.go → skipped
	got := detectCmdPackages(dir)
	if strings.Join(got, ",") != "cmd/a,cmd/b" {
		t.Errorf("detectCmdPackages = %v, want [cmd/a cmd/b]", got)
	}
}

// TestRunLocalAutoDetectAndWorkdir installs from a local dir, auto-detecting
// cmd/* and running a workdir step resolved against the local path.
func TestRunLocalAutoDetectAndWorkdir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/local\n\ngo 1.26\n")
	write(t, dir, "cmd/tool/main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "build/gen/main.go", "package main\n\nimport \"os\"\n\nfunc main() { os.WriteFile(\"marker\", []byte(\"ok\"), 0o644) }\n")
	write(t, dir, "fuigo.yaml", "steps:\n  - command: go run .\n    workdir: build/gen\n")

	bin := t.TempDir()
	setInstallEnv(t, bin)

	log := &logCapture{}
	if err := Run(Options{Package: dir, Yes: true, Logf: log.logf}); err != nil {
		t.Fatalf("Run local: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "build", "gen", "marker")); got != "ok" {
		t.Errorf("workdir step did not run in build/gen: %q", got)
	}
	assertBinary(t, bin, "tool")
}

// TestRunLocalExplicitPackage installs a single explicitly-named local package
// (and only that one) from a module with two commands, no fuigo.yaml.
func TestRunLocalExplicitPackage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/multi\n\ngo 1.26\n")
	write(t, dir, "cmd/a/main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "cmd/b/main.go", "package main\n\nfunc main() {}\n")

	bin := t.TempDir()
	setInstallEnv(t, bin)

	if err := Run(Options{Package: dir, Subpkg: "./cmd/b", Yes: true}); err != nil {
		t.Fatalf("Run local explicit: %v", err)
	}
	assertBinary(t, bin, "b")
	if _, err := os.Stat(filepath.Join(bin, "a")); err == nil {
		t.Error("cmd/a was installed but only ./cmd/b was requested")
	}
}

func TestCheckLocalValid(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - go generate ./...\n  - npmgo install\n")
	log := &logCapture{}
	if err := Check(Options{Package: dir, Logf: log.logf}); err != nil {
		t.Fatalf("Check valid: %v", err)
	}
	if !strings.Contains(log.joined(), "syntax OK (2 steps)") {
		t.Errorf("unexpected output: %s", log.joined())
	}
}

func TestCheckLocalInvalid(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - npm install\n")
	log := &logCapture{}
	err := Check(Options{Package: dir, Logf: log.logf})
	if err == nil {
		t.Fatal("expected Check to fail on invalid config")
	}
	if !strings.Contains(log.joined(), "fuigo.yaml error:") {
		t.Errorf("error not reported: %s", log.joined())
	}
}

func TestCheckLocalNoConfig(t *testing.T) {
	log := &logCapture{}
	if err := Check(Options{Package: t.TempDir(), Logf: log.logf}); err != nil {
		t.Fatalf("Check no-config should not error: %v", err)
	}
	if !strings.Contains(log.joined(), "no fuigo.yaml found") {
		t.Errorf("unexpected output: %s", log.joined())
	}
}

// TestCheckRemote fetches a module from the (offline) proxy, validates its
// fuigo.yaml, and cleans up — without executing or installing.
func TestCheckRemote(t *testing.T) {
	zipData := buildModuleZip(t, "github.com/fuigotest/chk", "v1.0.0", map[string]string{
		"go.mod":     "module github.com/fuigotest/chk\n\ngo 1.26\n",
		"fuigo.yaml": "steps:\n  - go generate ./...\n",
	})
	srv := newProxyServer(t, "github.com/fuigotest/chk", "v1.0.0", zipData)
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	log := &logCapture{}
	if err := Check(Options{Package: "github.com/fuigotest/chk/cmd/chk@latest", Logf: log.logf}); err != nil {
		t.Fatalf("Check remote: %v", err)
	}
	if !strings.Contains(log.joined(), "syntax OK (1 steps)") {
		t.Errorf("unexpected output: %s", log.joined())
	}
}

func assertBinary(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("binary %q not installed in %s: %v", name, dir, err)
	}
}
