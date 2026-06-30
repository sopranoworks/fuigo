// Package fuigo wraps "go install" with optional pre-build steps. It reads a
// fuigo.yaml from the target module (fetched from the Go module proxy), runs the
// declared steps, then delegates to go install. Without a fuigo.yaml it behaves
// exactly like go install.
package fuigo

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Options configures a Run or Check.
type Options struct {
	// Package is the target: a remote module path (optionally with an
	// "@version" suffix, e.g. "github.com/sopranoworks/shoka/cmd/shoka@latest")
	// or a local directory (".", "./path", "/abs/path").
	Package string
	// Subpkg optionally names the package to install for a local target, e.g.
	// "./cmd/shoka". When empty, local installs auto-detect ./cmd/*.
	Subpkg string
	// Yes skips the confirmation prompt before executing steps.
	Yes bool
	// List prints the pre-build steps without executing anything.
	List bool
	// DryRun executes all pre-build steps but skips the final go install.
	DryRun bool

	// Logf receives progress messages (without a trailing newline). If nil,
	// messages are discarded.
	Logf func(format string, args ...any)
	// In is the source of the confirmation answer; defaults handled by caller.
	In io.Reader
	// Out is where the confirmation prompt is written.
	Out io.Writer
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Run runs any pre-build steps for the target and installs it. The target is
// either a remote module (fetched from the proxy) or a local directory. It is
// the entry point used by the CLI.
func Run(opts Options) error {
	if opts.Package == "" {
		return fmt.Errorf("no package specified")
	}
	if opts.DryRun {
		opts.logf("dry run mode (build but do not install)")
	}
	if isLocalPath(opts.Package) {
		return runLocal(opts)
	}
	return runRemote(opts)
}

// dryRunBuild compiles each package (without installing) to verify it builds,
// then logs the dry-run completion messages. It is used in place of install when
// opts.DryRun is set: --dry-run runs go build but skips go install.
func dryRunBuild(opts Options, srcDir string, relPkgs []string) error {
	for _, rel := range relPkgs {
		target := "."
		if rel != "" {
			target = "./" + rel
		}
		opts.logf("running: go build %s", target)
		if err := Build(srcDir, rel); err != nil {
			return err
		}
	}
	opts.logf("skipping go install (dry run)")
	opts.logf("dry run OK")
	return nil
}

// runRemote resolves the module from the proxy, runs steps and installs it.
func runRemote(opts Options) error {
	pkgPath, version := splitVersion(opts.Package)
	module, relPkg := SplitModulePath(pkgPath)

	displayVersion := version
	if displayVersion == "" {
		displayVersion = "latest"
	}
	opts.logf("resolving %s@%s...", module, displayVersion)
	opts.logf("downloading module zip...")
	srcDir, _, cleanup, err := ResolveModule(pkgPath, version)
	if err != nil {
		return err
	}
	defer func() {
		cleanup()
		opts.logf("cleaned up %s", srcDir)
	}()

	cfg, err := LoadConfig(srcDir)
	if err != nil {
		return err
	}
	proceed, err := runSteps(opts, srcDir, cfg)
	if err != nil || !proceed {
		return err
	}
	if opts.DryRun {
		return dryRunBuild(opts, srcDir, []string{relPkg})
	}
	return install(opts, srcDir, relPkg, path.Base(pkgPath))
}

// runLocal runs steps and installs from a local directory, with no download.
func runLocal(opts Options) error {
	dir, err := filepath.Abs(opts.Package)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s: not a directory", opts.Package)
	}
	opts.logf("installing from local directory %s", dir)

	relPkgs, err := localPackages(opts, dir)
	if err != nil {
		return err
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		return err
	}
	proceed, err := runSteps(opts, dir, cfg)
	if err != nil || !proceed {
		return err
	}
	if opts.DryRun {
		return dryRunBuild(opts, dir, relPkgs)
	}
	for _, rel := range relPkgs {
		name := path.Base(rel)
		if rel == "" {
			name = path.Base(dir)
		}
		if err := install(opts, dir, rel, name); err != nil {
			return err
		}
	}
	return nil
}

// Check validates the target's fuigo.yaml without executing anything. For a
// remote target it fetches the module zip, validates, and cleans up. It returns
// a non-nil error when validation fails (problems are reported via Logf).
func Check(opts Options) error {
	if opts.Package == "" {
		return fmt.Errorf("no target specified")
	}
	var dir string
	if isLocalPath(opts.Package) {
		abs, err := filepath.Abs(opts.Package)
		if err != nil {
			opts.logf("error: %v", err)
			return err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			opts.logf("error: %s is not a directory", opts.Package)
			return fmt.Errorf("%s: not a directory", opts.Package)
		}
		dir = abs
	} else {
		pkgPath, version := splitVersion(opts.Package)
		srcDir, _, cleanup, err := ResolveModule(pkgPath, version)
		if err != nil {
			opts.logf("error: %v", err)
			return err
		}
		defer cleanup()
		dir = srcDir
	}

	found, steps, problems := ValidateConfig(dir)
	if !found {
		opts.logf("no fuigo.yaml found (plain go install, no pre-build steps)")
		return nil
	}
	if len(problems) > 0 {
		for _, p := range problems {
			opts.logf("fuigo.yaml error: %s", p)
		}
		return fmt.Errorf("%d validation error(s)", len(problems))
	}
	opts.logf("fuigo.yaml syntax OK (%d steps)", steps)
	return nil
}

// runSteps lists the steps and, unless --list, confirms and executes them in
// root. It returns proceed=false when --list short-circuits before install.
func runSteps(opts Options, root string, cfg *Config) (proceed bool, err error) {
	if cfg == nil {
		opts.logf("no fuigo.yaml found, running plain go install")
		return !opts.List, nil
	}
	opts.logf("fuigo.yaml found, pre-build steps:")
	for i, step := range cfg.Steps {
		opts.logf("  %d. %s", i+1, step)
	}
	if opts.List {
		return false, nil
	}
	if !opts.Yes {
		ok, err := confirm(opts)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, fmt.Errorf("aborted by user")
		}
	}
	builtins := DefaultBuiltins(opts.Logf)
	if err := ExecuteSteps(root, cfg.Steps, builtins); err != nil {
		return false, err
	}
	return true, nil
}

// install delegates to go install and reports where the binary landed. binName
// is the expected binary name, used only for the progress message.
func install(opts Options, srcDir, relPkg, binName string) error {
	target := "."
	if relPkg != "" {
		target = "./" + relPkg
	}
	opts.logf("running: go install %s", target)
	if err := Install(srcDir, relPkg); err != nil {
		return err
	}
	if dir := InstallDir(); dir != "" {
		opts.logf("installed to %s/%s", dir, binName)
	} else {
		opts.logf("installed %s", binName)
	}
	return nil
}

// localPackages determines which package(s) to install from a local directory:
// the explicit Subpkg if given, otherwise auto-detected ./cmd/* packages, or
// the module root as a last resort.
func localPackages(opts Options, dir string) ([]string, error) {
	if opts.Subpkg != "" {
		rel, err := cleanRelPkg(opts.Subpkg)
		if err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}
	if detected := detectCmdPackages(dir); len(detected) > 0 {
		return detected, nil
	}
	return []string{""}, nil
}

// detectCmdPackages returns the "cmd/<name>" packages under dir that contain a
// main.go, sorted for deterministic order.
func detectCmdPackages(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return nil
	}
	var pkgs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "cmd", e.Name(), "main.go")); err == nil {
			pkgs = append(pkgs, "cmd/"+e.Name())
		}
	}
	return pkgs
}

// isLocalPath reports whether target names a local directory rather than a
// remote module path. Local: ".", "..", a "./"/"../"/"/"-prefixed path, or a
// path whose first segment has no dot (module paths start with a dotted host
// like github.com).
func isLocalPath(target string) bool {
	switch {
	case target == "", target == ".", target == "..":
		return target != ""
	case strings.HasPrefix(target, "./"), strings.HasPrefix(target, "../"), strings.HasPrefix(target, "/"):
		return true
	}
	first := target
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	if i := strings.IndexByte(first, '@'); i >= 0 {
		first = first[:i]
	}
	return !strings.Contains(first, ".")
}

// confirm prompts on opts.Out and reads a yes/no answer from opts.In. The
// default (empty line) is yes.
func confirm(opts Options) (bool, error) {
	if opts.Out != nil {
		fmt.Fprint(opts.Out, "fuigo: proceed? [Y/n] ")
	}
	if opts.In == nil {
		return true, nil
	}
	reader := bufio.NewReader(opts.In)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// splitVersion separates a package path from its optional "@version" suffix.
func splitVersion(spec string) (pkg, version string) {
	if i := strings.LastIndex(spec, "@"); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}
