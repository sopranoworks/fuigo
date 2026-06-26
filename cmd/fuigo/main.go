// Command fuigo is a drop-in "go install" replacement that runs a module's
// declared pre-build steps (from fuigo.yaml) before installing.
package main

import (
	"fmt"
	"os"

	"github.com/sopranoworks/fuigo"
)

// version is the fuigo release, overridable at build time with
// -ldflags "-X main.version=vX.Y.Z".
var version = "v0.1.0"

const usage = `fuigo — go install with pre-build steps

Usage:
  fuigo [flags] <package>[@version]

Flags:
  --yes        Skip the confirmation prompt before running steps
  --list       Show the pre-build steps without executing them
  --version    Print the fuigo version and exit
  -h, --help   Show this help

Examples:
  fuigo github.com/sopranoworks/shoka/cmd/shoka@latest
  fuigo --list github.com/sopranoworks/shoka/cmd/shoka@latest
`

func main() {
	opts := fuigo.Options{
		Logf: func(format string, a ...any) { fmt.Fprintf(os.Stderr, "fuigo: "+format+"\n", a...) },
		In:   os.Stdin,
		Out:  os.Stderr,
	}

	var pkg string
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--yes", "-y":
			opts.Yes = true
		case "--list", "-l":
			opts.List = true
		case "--version", "-v":
			fmt.Printf("fuigo %s\n", version)
			return
		case "-h", "--help":
			fmt.Print(usage)
			return
		default:
			if len(arg) > 0 && arg[0] == '-' {
				fmt.Fprintf(os.Stderr, "fuigo: unknown flag %q\n", arg)
				os.Exit(2)
			}
			if pkg != "" {
				fmt.Fprintf(os.Stderr, "fuigo: multiple packages given (%q and %q)\n", pkg, arg)
				os.Exit(2)
			}
			pkg = arg
		}
	}

	if pkg == "" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	opts.Package = pkg

	if err := fuigo.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "fuigo: error: %v\n", err)
		os.Exit(1)
	}
}
