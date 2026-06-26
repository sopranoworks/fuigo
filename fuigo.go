// Package fuigo wraps "go install" with optional pre-build steps. It reads a
// fuigo.yaml from the target module (fetched from the Go module proxy), runs the
// declared steps, then delegates to go install. Without a fuigo.yaml it behaves
// exactly like go install.
package fuigo

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"strings"
)

// Options configures a Run.
type Options struct {
	// Package is the target package path, optionally with an "@version" suffix
	// (e.g. "github.com/sopranoworks/shoka/cmd/shoka@latest").
	Package string
	// Yes skips the confirmation prompt before executing steps.
	Yes bool
	// List prints the pre-build steps without executing anything.
	List bool

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

// Run resolves the target module, runs any pre-build steps and installs the
// package. It is the entry point used by the CLI.
func Run(opts Options) error {
	pkgPath, version := splitVersion(opts.Package)
	if pkgPath == "" {
		return fmt.Errorf("no package specified")
	}
	module, relPkg := SplitModulePath(pkgPath)

	displayVersion := version
	if displayVersion == "" {
		displayVersion = "latest"
	}

	opts.logf("resolving %s@%s...", module, displayVersion)
	opts.logf("downloading module zip...")
	srcDir, resolved, cleanup, err := ResolveModule(pkgPath, version)
	if err != nil {
		return err
	}
	defer func() {
		cleanup()
		opts.logf("cleaned up %s", srcDir)
	}()
	if resolved != "" {
		displayVersion = resolved
	}

	cfg, err := LoadConfig(srcDir)
	if err != nil {
		return err
	}

	if cfg == nil {
		opts.logf("no fuigo.yaml found, running plain go install")
		if opts.List {
			return nil
		}
		return install(opts, srcDir, relPkg, pkgPath)
	}

	opts.logf("fuigo.yaml found, pre-build steps:")
	for i, step := range cfg.Steps {
		opts.logf("  %d. %s", i+1, step)
	}

	if opts.List {
		return nil
	}

	if !opts.Yes {
		ok, err := confirm(opts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("aborted by user")
		}
	}

	builtins := DefaultBuiltins(opts.Logf)
	if err := ExecuteSteps(srcDir, cfg.Steps, builtins); err != nil {
		return err
	}

	return install(opts, srcDir, relPkg, pkgPath)
}

// install delegates to go install and reports where the binary landed.
func install(opts Options, srcDir, relPkg, pkgPath string) error {
	rel := relPkg
	if rel == "" {
		rel = "."
	}
	opts.logf("running: go install ./%s", strings.TrimPrefix(rel, "./"))
	if err := Install(srcDir, relPkg); err != nil {
		return err
	}
	bin := path.Base(pkgPath)
	if dir := InstallDir(); dir != "" {
		opts.logf("installed to %s/%s", dir, bin)
	} else {
		opts.logf("installed %s", bin)
	}
	return nil
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
