package fuigo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestInstallProducesBinary builds a trivial module and confirms Install writes
// the binary to GOBIN. Fully offline (no dependencies).
func TestInstallProducesBinary(t *testing.T) {
	src := t.TempDir()
	write(t, src, "go.mod", "module github.com/fuigotest/hello\n\ngo 1.26\n")
	write(t, src, "cmd/hello/main.go", "package main\n\nfunc main() {}\n")

	bin := t.TempDir()
	setInstallEnv(t, bin)

	if err := Install(src, "cmd/hello"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	name := "hello"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err := os.Stat(filepath.Join(bin, name)); err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	if got := InstallDir(); got != bin {
		t.Errorf("InstallDir = %q, want %q", got, bin)
	}
}

// setInstallEnv points the go tool at an isolated, offline-friendly environment.
func setInstallEnv(t *testing.T, gobin string) {
	t.Helper()
	t.Setenv("GOBIN", gobin)
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
}
