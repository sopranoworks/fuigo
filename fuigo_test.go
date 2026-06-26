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
		"fuigo.yaml":      "steps:\n  - go env GOOS\n",
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

func assertBinary(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("binary %q not installed in %s: %v", name, dir, err)
	}
}
