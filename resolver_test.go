package fuigo

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitModulePath(t *testing.T) {
	cases := []struct {
		in, module, rel string
	}{
		{"github.com/sopranoworks/shoka/cmd/shoka", "github.com/sopranoworks/shoka", "cmd/shoka"},
		{"github.com/sopranoworks/shoka", "github.com/sopranoworks/shoka", ""},
		{"gitlab.com/owner/repo/sub/pkg", "gitlab.com/owner/repo", "sub/pkg"},
		{"example.com/single", "example.com/single", ""},
	}
	for _, c := range cases {
		mod, rel := SplitModulePath(c.in)
		if mod != c.module || rel != c.rel {
			t.Errorf("SplitModulePath(%q) = (%q, %q), want (%q, %q)", c.in, mod, rel, c.module, c.rel)
		}
	}
}

func TestEscapeModulePath(t *testing.T) {
	got, err := escapeModulePath("github.com/Masterminds/Sprig")
	if err != nil {
		t.Fatal(err)
	}
	if want := "github.com/!masterminds/!sprig"; got != want {
		t.Errorf("escapeModulePath = %q, want %q", got, want)
	}
}

func TestProxyBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                          defaultProxy,
		"https://proxy.example.com": "https://proxy.example.com",
		"https://a.com,direct":      "https://a.com",
		"direct":                    "",
		"off":                       "",
		"direct,https://b.com/":     "https://b.com",
	}
	for env, want := range cases {
		t.Setenv("GOPROXY", env)
		if got := proxyBaseURL(); got != want {
			t.Errorf("proxyBaseURL(GOPROXY=%q) = %q, want %q", env, got, want)
		}
	}
}

func TestExtractModuleZipStripsPrefixAndRejectsSlip(t *testing.T) {
	// Valid module zip.
	data := buildModuleZip(t, "github.com/x/y", "v1.0.0", map[string]string{
		"go.mod":       "module github.com/x/y\n",
		"sub/file.txt": "hello",
	})
	dest := t.TempDir()
	if err := extractModuleZip(data, "github.com/x/y", "v1.0.0", dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readFile(t, filepath.Join(dest, "sub", "file.txt")); got != "hello" {
		t.Errorf("extracted content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "go.mod")); err != nil {
		t.Errorf("go.mod not extracted: %v", err)
	}

	// Zip-slip: an entry escaping the destination must be rejected.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("github.com/x/y@v1.0.0/../escape.txt")
	w.Write([]byte("evil"))
	zw.Close()
	if err := extractModuleZip(buf.Bytes(), "github.com/x/y", "v1.0.0", t.TempDir()); err == nil {
		t.Fatal("expected zip-slip to be rejected")
	}
}

// TestResolveModuleViaProxy exercises the full proxy path (resolve @latest,
// download zip, extract) and the cleanup function, all offline.
func TestResolveModuleViaProxy(t *testing.T) {
	zipData := buildModuleZip(t, "github.com/fuigotest/hello", "v1.2.3", map[string]string{
		"go.mod":     "module github.com/fuigotest/hello\n\ngo 1.26\n",
		"fuigo.yaml": "steps:\n  - go version\n",
	})
	srv := newProxyServer(t, "github.com/fuigotest/hello", "v1.2.3", zipData)
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	srcDir, resolved, cleanup, err := ResolveModule("github.com/fuigotest/hello/cmd/hello", "latest")
	if err != nil {
		t.Fatalf("ResolveModule: %v", err)
	}
	if resolved != "v1.2.3" {
		t.Errorf("resolved = %q, want v1.2.3", resolved)
	}
	if !strings.HasPrefix(filepath.Base(srcDir), "fuigo-") {
		t.Errorf("temp dir %q lacks fuigo- prefix", srcDir)
	}
	if got := readFile(t, filepath.Join(srcDir, "fuigo.yaml")); !strings.Contains(got, "go version") {
		t.Errorf("fuigo.yaml not extracted: %q", got)
	}

	// Cleanup must remove the temp directory.
	cleanup()
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Errorf("temp dir still exists after cleanup: %v", err)
	}
}

// --- shared test helpers ---

// buildModuleZip builds an in-memory Go module zip: every entry is prefixed
// with "<module>@<version>/" exactly as the module proxy serves them.
func buildModuleZip(t *testing.T, module, version string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	prefix := module + "@" + version + "/"
	for name, content := range files {
		w, err := zw.Create(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newProxyServer returns an httptest server that answers the module proxy
// endpoints for one module/version.
func newProxyServer(t *testing.T, module, version string, zipData []byte) *httptest.Server {
	t.Helper()
	esc, _ := escapeModulePath(module)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/@latest"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"Version":"` + version + `"}`))
		case strings.HasSuffix(r.URL.Path, "/@v/"+version+".zip"):
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipData)
		case strings.Contains(r.URL.Path, "/@v/"+version+".info"):
			w.Write([]byte(`{"Version":"` + version + `"}`))
		default:
			t.Logf("proxy: unexpected request %s (esc=%s)", r.URL.Path, esc)
			http.NotFound(w, r)
		}
	}))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
