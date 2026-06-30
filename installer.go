package fuigo

import (
	"fmt"
	"os"
	"os/exec"
)

// Install delegates to "go install" for the package at relPkg within the module
// source rooted at dir. relPkg is the package path relative to the module root
// (e.g. "cmd/shoka"); an empty relPkg installs the module root package. Output
// is streamed live.
func Install(dir, relPkg string) error {
	rel, err := cleanRelPkg(relPkg)
	if err != nil {
		return err
	}
	target := "./" + rel
	if rel == "" {
		target = "."
	}
	cmd := exec.Command("go", "install", target)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install %s: %w", target, err)
	}
	return nil
}

// Build compiles the package at relPkg within the module source rooted at dir
// without producing a binary (output is discarded via os.DevNull), to verify the
// code compiles. It is used by dry-run mode in place of go install. relPkg
// follows the same convention as Install: an empty relPkg builds the module root
// package. Output is streamed live.
func Build(dir, relPkg string) error {
	rel, err := cleanRelPkg(relPkg)
	if err != nil {
		return err
	}
	target := "./" + rel
	if rel == "" {
		target = "."
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, target)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s: %w", target, err)
	}
	return nil
}

// InstallDir reports the directory go install writes binaries to: GOBIN if set,
// otherwise GOPATH/bin.
func InstallDir() string {
	if out, err := exec.Command("go", "env", "GOBIN").Output(); err == nil {
		if dir := trimLine(string(out)); dir != "" {
			return dir
		}
	}
	if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
		if gopath := trimLine(string(out)); gopath != "" {
			return gopath + "/bin"
		}
	}
	return ""
}

func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
