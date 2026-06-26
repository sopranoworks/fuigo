package fuigo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for missing fuigo.yaml, got %+v", cfg)
	}
}

func TestLoadConfigValid(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - npmgo install --cache-only\n  - esbuild --entry a.tsx --bundle\n  - go generate ./...\n")
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(cfg.Steps))
	}
}

func TestLoadConfigEmptySteps(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps: []\n")
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestLoadConfigDisallowedCommand(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - rm -rf /\n")
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("expected error for disallowed command")
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
