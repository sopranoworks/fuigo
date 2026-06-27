package fuigo

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadConfigMixedStringAndMapSteps(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", ""+
		"steps:\n"+
		"  - command: go run .\n"+
		"    workdir: build/frontend\n"+
		"  - go generate ./server/...\n")
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Steps))
	}
	if cfg.Steps[0].Command != "go run ." || cfg.Steps[0].Workdir != "build/frontend" {
		t.Errorf("map step parsed wrong: %+v", cfg.Steps[0])
	}
	if cfg.Steps[1].Command != "go generate ./server/..." || cfg.Steps[1].Workdir != "" {
		t.Errorf("string step parsed wrong: %+v", cfg.Steps[1])
	}
}

func TestLoadConfigMapStepDisallowedCommand(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - command: cd build && go run .\n    workdir: x\n")
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("expected error for disallowed command in map step")
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

func TestValidateConfigOK(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - go generate ./...\n  - command: go run .\n    workdir: build/frontend\n")
	found, steps, problems := ValidateConfig(dir)
	if !found || steps != 2 || len(problems) != 0 {
		t.Fatalf("ValidateConfig = (found=%v, steps=%d, problems=%v)", found, steps, problems)
	}
}

func TestValidateConfigNoFile(t *testing.T) {
	found, steps, problems := ValidateConfig(t.TempDir())
	if found || steps != 0 || len(problems) != 0 {
		t.Fatalf("expected (false,0,nil), got (%v,%d,%v)", found, steps, problems)
	}
}

func TestValidateConfigCollectsAllErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps:\n  - npm install\n  - command: go run .\n    workdir: ../escape\n")
	found, _, problems := ValidateConfig(dir)
	if !found || len(problems) != 2 {
		t.Fatalf("expected 2 problems, got %v", problems)
	}
	if !strings.Contains(problems[0], "step 1") || !strings.Contains(problems[0], "not allowed") {
		t.Errorf("problem 0 = %q", problems[0])
	}
	if !strings.Contains(problems[1], "step 2") || !strings.Contains(problems[1], "escapes") {
		t.Errorf("problem 1 = %q", problems[1])
	}
}

func TestValidateConfigEmptySteps(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fuigo.yaml", "steps: []\n")
	_, _, problems := ValidateConfig(dir)
	if len(problems) == 0 {
		t.Fatal("expected a problem for empty steps")
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
