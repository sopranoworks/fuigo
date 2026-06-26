package fuigo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"go generate ./...", []string{"go", "generate", "./..."}},
		{`npmgo install --lockfile "web/package lock.json"`, []string{"npmgo", "install", "--lockfile", "web/package lock.json"}},
		{"esbuild --entry 'a b.tsx'", []string{"esbuild", "--entry", "a b.tsx"}},
	}
	for _, c := range cases {
		got, err := splitArgs(c.in)
		if err != nil {
			t.Fatalf("splitArgs(%q): %v", c.in, err)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitArgs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if _, err := splitArgs(`go "unterminated`); err == nil {
		t.Error("expected error for unterminated quote")
	}
}

// TestExecuteStepsDispatchNpmgo verifies an "npmgo ..." step is routed to the
// npmgo built-in with the trailing arguments.
func TestExecuteStepsDispatchNpmgo(t *testing.T) {
	dir := t.TempDir()
	var gotArgs []string
	builtins := Builtins{
		Npmgo: func(d string, args []string) error {
			gotArgs = args
			return nil
		},
	}
	err := ExecuteSteps(dir, []Step{{Command: "npmgo install --cache-only --lockfile web/package-lock.json"}}, builtins)
	if err != nil {
		t.Fatalf("ExecuteSteps: %v", err)
	}
	want := "install|--cache-only|--lockfile|web/package-lock.json"
	if strings.Join(gotArgs, "|") != want {
		t.Errorf("npmgo args = %v, want %s", gotArgs, want)
	}
}

// TestExecuteStepsGoStep runs a real go step and confirms it executes in dir.
func TestExecuteStepsGoStep(t *testing.T) {
	dir := t.TempDir()
	// "go env GOOS" is harmless and offline.
	if err := ExecuteSteps(dir, []Step{{Command: "go env GOOS"}}, DefaultBuiltins(nil)); err != nil {
		t.Fatalf("ExecuteSteps go step: %v", err)
	}
}

// TestExecuteStepsStopsOnFailure verifies a failing step halts execution and
// later steps do not run.
func TestExecuteStepsStopsOnFailure(t *testing.T) {
	dir := t.TempDir()
	ran := false
	builtins := Builtins{
		Esbuild: func(d string, args []string) error { ran = true; return nil },
	}
	// "go nonexistent-subcommand" fails; the esbuild step must never run.
	err := ExecuteSteps(dir, []Step{{Command: "go nonexistent-subcommand-xyz"}, {Command: "esbuild --entry x"}}, builtins)
	if err == nil {
		t.Fatal("expected failure")
	}
	if ran {
		t.Error("subsequent step ran despite earlier failure")
	}
}

func TestExecuteStepsUnsupportedCommand(t *testing.T) {
	if err := ExecuteSteps(t.TempDir(), []Step{{Command: "rm -rf /"}}, DefaultBuiltins(nil)); err == nil {
		t.Fatal("expected unsupported command error")
	}
}

// TestRunEsbuildBuildsBundle exercises the built-in esbuild library end to end,
// fully offline: it bundles a tiny module to an output file.
func TestRunEsbuildBuildsBundle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/dep.js", "export const greet = () => 'hi';\n")
	write(t, dir, "src/main.js", "import {greet} from './dep.js';\nconsole.log(greet());\n")

	step := "esbuild --entry src/main.js --bundle --outfile dist/out.js --format esm"
	if err := ExecuteSteps(dir, []Step{{Command: step}}, DefaultBuiltins(nil)); err != nil {
		t.Fatalf("esbuild step: %v", err)
	}
	out := readFile(t, filepath.Join(dir, "dist", "out.js"))
	if !strings.Contains(out, "hi") {
		t.Errorf("bundle missing inlined dependency: %q", out)
	}
}

// TestExecuteStepsWorkdirDispatch verifies a step's workdir is resolved against
// the module root and passed to the dispatched command.
func TestExecuteStepsWorkdirDispatch(t *testing.T) {
	root := t.TempDir()
	var gotDir string
	builtins := Builtins{
		Npmgo: func(d string, args []string) error { gotDir = d; return nil },
	}
	steps := []Step{{Command: "npmgo install", Workdir: "web/app"}}
	if err := ExecuteSteps(root, steps, builtins); err != nil {
		t.Fatalf("ExecuteSteps: %v", err)
	}
	if want := filepath.Join(root, "web", "app"); gotDir != want {
		t.Errorf("workdir = %q, want %q", gotDir, want)
	}
}

// TestExecuteStepsWorkdirGoRun proves a go step actually runs in the workdir:
// the program writes a marker into its current directory.
func TestExecuteStepsWorkdirGoRun(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/wd\n\ngo 1.26\n")
	write(t, root, "tool/main.go", "package main\n\nimport \"os\"\n\nfunc main() { os.WriteFile(\"marker\", []byte(\"ok\"), 0o644) }\n")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")

	steps := []Step{{Command: "go run .", Workdir: "tool"}}
	if err := ExecuteSteps(root, steps, DefaultBuiltins(nil)); err != nil {
		t.Fatalf("ExecuteSteps: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "tool", "marker")); got != "ok" {
		t.Errorf("marker not written in workdir: %q", got)
	}
}

func TestExecuteStepsWorkdirEscapeRejected(t *testing.T) {
	root := t.TempDir()
	ran := false
	builtins := Builtins{Npmgo: func(d string, args []string) error { ran = true; return nil }}
	steps := []Step{{Command: "npmgo install", Workdir: "../escape"}}
	if err := ExecuteSteps(root, steps, builtins); err == nil {
		t.Fatal("expected workdir escape to be rejected")
	}
	if ran {
		t.Error("step ran despite escaping workdir")
	}
}

func TestResolveWorkdir(t *testing.T) {
	root := t.TempDir()
	if got, err := resolveWorkdir(root, ""); err != nil || got != root {
		t.Errorf("empty workdir = (%q, %v), want (%q, nil)", got, err, root)
	}
	if got, err := resolveWorkdir(root, "a/b"); err != nil || got != filepath.Join(root, "a/b") {
		t.Errorf("sub workdir = (%q, %v)", got, err)
	}
	for _, bad := range []string{"../x", "a/../../x", string(os.PathSeparator) + "abs"} {
		if _, err := resolveWorkdir(root, bad); err == nil {
			t.Errorf("workdir %q should be rejected", bad)
		}
	}
}

func TestRunEsbuildRequiresEntry(t *testing.T) {
	if err := runEsbuild(t.TempDir(), []string{"--bundle"}); err == nil {
		t.Fatal("expected error when no entry given")
	}
}

func TestRunNpmgoRejectsNonInstall(t *testing.T) {
	if err := runNpmgo(t.TempDir(), []string{"frobnicate"}); err == nil {
		t.Fatal("expected error for unknown npmgo subcommand")
	}
}

func TestResolveUnder(t *testing.T) {
	dir := t.TempDir()
	if got := resolveUnder(dir, "a/b"); got != filepath.Join(dir, "a/b") {
		t.Errorf("resolveUnder relative = %q", got)
	}
	abs := string(os.PathSeparator) + "abs" + string(os.PathSeparator) + "p"
	if got := resolveUnder(dir, abs); got != abs {
		t.Errorf("resolveUnder absolute = %q, want %q", got, abs)
	}
}
