package fuigo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/sopranoworks/npmgo"
)

// Builtins holds the implementations fuigo dispatches built-in steps to. They
// are injectable so tests can substitute fakes. Logf, when set, receives
// progress messages; a nil Logf discards them.
type Builtins struct {
	Npmgo   func(dir string, args []string) error
	Esbuild func(dir string, args []string) error
	Logf    func(format string, args ...any)
}

// DefaultBuiltins returns Builtins wired to the compiled-in npmgo and esbuild
// libraries, logging progress through logf.
func DefaultBuiltins(logf func(format string, args ...any)) Builtins {
	return Builtins{
		Npmgo:   runNpmgo,
		Esbuild: runEsbuild,
		Logf:    logf,
	}
}

func (b Builtins) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

// ExecuteSteps runs each step in dir. A step is dispatched by its first token:
// "go" runs the external go tool; "npmgo" and "esbuild" invoke the built-in
// libraries. Any other command is rejected. Execution stops at the first
// failing step, returning an error that names the step.
func ExecuteSteps(dir string, steps []string, builtins Builtins) error {
	for i, step := range steps {
		args, err := splitArgs(step)
		if err != nil {
			return fmt.Errorf("step %d %q: %w", i+1, step, err)
		}
		if len(args) == 0 {
			return fmt.Errorf("step %d is empty", i+1)
		}
		builtins.logf("running: %s", step)
		switch args[0] {
		case "go":
			err = runGoStep(dir, args[1:])
		case "npmgo":
			if builtins.Npmgo == nil {
				return fmt.Errorf("step %d: npmgo built-in not available", i+1)
			}
			err = builtins.Npmgo(dir, args[1:])
		case "esbuild":
			if builtins.Esbuild == nil {
				return fmt.Errorf("step %d: esbuild built-in not available", i+1)
			}
			err = builtins.Esbuild(dir, args[1:])
		default:
			return fmt.Errorf("step %d: unsupported command %q", i+1, args[0])
		}
		if err != nil {
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step, err)
		}
		builtins.logf("step %d complete", i+1)
	}
	return nil
}

// runGoStep executes the external go tool in dir, streaming output live.
func runGoStep(dir string, args []string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

// runNpmgo invokes the built-in npmgo library. Supported form:
//
//	npmgo install [--cache-only] [--production] [--lockfile PATH] [--target DIR]
func runNpmgo(dir string, args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return fmt.Errorf("npmgo: expected 'install' subcommand")
	}
	opts := npmgo.Options{
		LockfilePath: filepath.Join(dir, "package-lock.json"),
		TargetDir:    filepath.Join(dir, "node_modules"),
		Logf:         func(format string, a ...any) { fmt.Printf("npmgo: "+format+"\n", a...) },
	}
	cacheOnly := false
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--cache-only":
			cacheOnly = true
		case "--production":
			opts.Production = true
		case "--lockfile":
			if i+1 >= len(rest) {
				return fmt.Errorf("npmgo: --lockfile requires a value")
			}
			i++
			opts.LockfilePath = resolveUnder(dir, rest[i])
		case "--target":
			if i+1 >= len(rest) {
				return fmt.Errorf("npmgo: --target requires a value")
			}
			i++
			opts.TargetDir = resolveUnder(dir, rest[i])
		default:
			return fmt.Errorf("npmgo: unknown flag %q", rest[i])
		}
	}
	if cacheOnly {
		_, err := npmgo.CachePackages(opts)
		return err
	}
	return npmgo.Install(opts)
}

// runEsbuild invokes the built-in esbuild Go API. Supported form:
//
//	esbuild --entry FILE [--entry FILE...] [--bundle] [--outdir DIR|--outfile FILE]
//	        [--minify] [--format esm|cjs|iife] [--sourcemap]
func runEsbuild(dir string, args []string) error {
	opts := esbuild.BuildOptions{
		AbsWorkingDir: dir,
		Write:         true,
		LogLevel:      esbuild.LogLevelInfo,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--entry":
			if i+1 >= len(args) {
				return fmt.Errorf("esbuild: --entry requires a value")
			}
			i++
			opts.EntryPoints = append(opts.EntryPoints, args[i])
		case "--bundle":
			opts.Bundle = true
		case "--minify":
			opts.MinifyWhitespace = true
			opts.MinifyIdentifiers = true
			opts.MinifySyntax = true
		case "--sourcemap":
			opts.Sourcemap = esbuild.SourceMapLinked
		case "--outdir":
			if i+1 >= len(args) {
				return fmt.Errorf("esbuild: --outdir requires a value")
			}
			i++
			opts.Outdir = args[i]
		case "--outfile":
			if i+1 >= len(args) {
				return fmt.Errorf("esbuild: --outfile requires a value")
			}
			i++
			opts.Outfile = args[i]
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("esbuild: --format requires a value")
			}
			i++
			f, err := parseEsbuildFormat(args[i])
			if err != nil {
				return err
			}
			opts.Format = f
		default:
			return fmt.Errorf("esbuild: unknown flag %q", args[i])
		}
	}
	if len(opts.EntryPoints) == 0 {
		return fmt.Errorf("esbuild: at least one --entry is required")
	}
	result := esbuild.Build(opts)
	if len(result.Errors) > 0 {
		msgs := esbuild.FormatMessages(result.Errors, esbuild.FormatMessagesOptions{})
		return fmt.Errorf("esbuild: %s", strings.TrimSpace(strings.Join(msgs, "")))
	}
	return nil
}

func parseEsbuildFormat(s string) (esbuild.Format, error) {
	switch s {
	case "esm":
		return esbuild.FormatESModule, nil
	case "cjs":
		return esbuild.FormatCommonJS, nil
	case "iife":
		return esbuild.FormatIIFE, nil
	default:
		return esbuild.FormatDefault, fmt.Errorf("esbuild: unknown format %q", s)
	}
}

// resolveUnder joins rel under dir unless rel is already absolute.
func resolveUnder(dir, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(dir, rel)
}

// splitArgs tokenizes a step into arguments on whitespace, honouring single and
// double quotes so that quoted values containing spaces stay intact.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inToken := false
	var quote rune

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t':
			if inToken {
				args = append(args, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inToken {
		args = append(args, cur.String())
	}
	return args, nil
}
