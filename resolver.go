package fuigo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// defaultProxy is the public Go module proxy, used unless GOPROXY overrides it.
const defaultProxy = "https://proxy.golang.org"

// knownVCSHosts host modules whose root path is the first three path segments
// (host/owner/repo), matching how the go tool splits them for these hosts.
var knownVCSHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
}

// SplitModulePath splits a full package import path into its module root and
// the package path relative to that root. For the common VCS hosts the module
// root is host/owner/repo; e.g. "github.com/sopranoworks/shoka/cmd/shoka"
// becomes module "github.com/sopranoworks/shoka" and relPkg "cmd/shoka".
func SplitModulePath(pkg string) (module, relPkg string) {
	pkg = strings.Trim(pkg, "/")
	segs := strings.Split(pkg, "/")
	if len(segs) >= 3 && knownVCSHosts[segs[0]] {
		return strings.Join(segs[:3], "/"), strings.Join(segs[3:], "/")
	}
	// Unknown host: treat the whole path as the module root.
	return pkg, ""
}

// ResolveModule fetches the source of the module containing pkg at the given
// version and extracts it to a temporary directory. version may be "", "latest"
// or an explicit "vX.Y.Z". It returns the extracted source directory, a cleanup
// function that removes it, and the resolved concrete version.
//
// The primary path downloads a zip from the Go module proxy (no git). If the
// module is not available on the proxy (e.g. GOPRIVATE repositories) it falls
// back to a shallow git clone via go-git.
func ResolveModule(pkg, version string) (srcDir string, resolved string, cleanup func(), err error) {
	module, _ := SplitModulePath(pkg)

	tmp, err := os.MkdirTemp("", "fuigo-")
	if err != nil {
		return "", "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmp) }

	srcDir, resolved, err = fetchFromProxy(module, version, tmp)
	if err == nil {
		return srcDir, resolved, cleanup, nil
	}

	// Fall back to git clone for modules the proxy cannot serve.
	if isProxyMiss(err) {
		srcDir, resolved, gErr := fetchFromGit(module, version, tmp)
		if gErr != nil {
			cleanup()
			return "", "", nil, fmt.Errorf("proxy fetch failed (%v) and git fallback failed: %w", err, gErr)
		}
		return srcDir, resolved, cleanup, nil
	}

	cleanup()
	return "", "", nil, err
}

// proxyMissError marks a proxy response that means "this module/version is not
// served here" (404/410), which is the trigger for the git fallback.
type proxyMissError struct{ msg string }

func (e *proxyMissError) Error() string { return e.msg }

func isProxyMiss(err error) bool {
	var pm *proxyMissError
	return errors.As(err, &pm)
}

// fetchFromProxy resolves the version (when needed), downloads the module zip
// and extracts it into dest, returning the populated source directory.
func fetchFromProxy(module, version, dest string) (srcDir, resolved string, err error) {
	base := proxyBaseURL()
	if base == "" {
		return "", "", &proxyMissError{msg: "GOPROXY disables proxy access"}
	}
	esc, err := escapeModulePath(module)
	if err != nil {
		return "", "", err
	}

	resolved = version
	if version == "" || version == "latest" {
		resolved, err = resolveLatest(base, esc)
		if err != nil {
			return "", "", err
		}
	}

	zipURL := fmt.Sprintf("%s/%s/@v/%s.zip", base, esc, escapeVersion(resolved))
	data, err := httpGet(zipURL)
	if err != nil {
		return "", "", err
	}

	if err := extractModuleZip(data, module, resolved, dest); err != nil {
		return "", "", err
	}
	return dest, resolved, nil
}

// resolveLatest queries the proxy's @latest endpoint and returns the version.
func resolveLatest(base, escModule string) (string, error) {
	data, err := httpGet(fmt.Sprintf("%s/%s/@latest", base, escModule))
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parsing @latest response: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("@latest returned no version for module")
	}
	return info.Version, nil
}

// extractModuleZip extracts a Go module zip into dest, stripping the
// "<module>@<version>/" prefix that prefixes every entry.
func extractModuleZip(data []byte, module, version, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening module zip: %w", err)
	}
	prefix := module + "@" + version + "/"
	for _, f := range zr.File {
		name, ok := strings.CutPrefix(f.Name, prefix)
		if !ok || name == "" {
			continue
		}
		// Guard against zip-slip: the resolved path must stay within dest.
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry escapes destination: %s", f.Name)
		}
		if strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	return nil
}

// fetchFromGit clones the module repository into dest as a fallback for modules
// the proxy will not serve. version "" / "latest" clones the default branch;
// an explicit version is checked out as a tag.
func fetchFromGit(module, version, dest string) (srcDir, resolved string, err error) {
	repoURL := "https://" + module + ".git"
	opts := &git.CloneOptions{URL: repoURL, Depth: 1, SingleBranch: true}
	if version != "" && version != "latest" {
		opts.ReferenceName = plumbing.NewTagReferenceName(version)
		resolved = version
	} else {
		resolved = "latest"
	}
	if _, err := git.PlainClone(dest, false, opts); err != nil {
		return "", "", fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return dest, resolved, nil
}

// proxyBaseURL returns the first usable proxy URL from GOPROXY, or the default
// public proxy. It returns "" when GOPROXY is "off" or only "direct".
func proxyBaseURL() string {
	v := strings.TrimSpace(os.Getenv("GOPROXY"))
	if v == "" {
		return defaultProxy
	}
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '|' }) {
		part = strings.TrimSpace(part)
		switch part {
		case "", "direct", "off":
			continue
		}
		if strings.HasPrefix(part, "https://") || strings.HasPrefix(part, "http://") {
			return strings.TrimRight(part, "/")
		}
	}
	return ""
}

// escapeModulePath applies the Go module proxy case-encoding: every uppercase
// ASCII letter becomes "!" followed by its lowercase form.
func escapeModulePath(p string) (string, error) {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("empty module path")
	}
	return b.String(), nil
}

// escapeVersion applies the same case-encoding to a version string.
func escapeVersion(v string) string {
	esc, _ := escapeModulePath(v)
	return esc
}

// httpGet fetches url and returns the body, mapping 404/410 to a proxyMissError.
func httpGet(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, &proxyMissError{msg: fmt.Sprintf("not found on proxy: %s (%d)", url, resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

// cleanRelPkg validates that a package-relative path stays within the module.
func cleanRelPkg(rel string) (string, error) {
	rel = path.Clean("/" + rel)
	return strings.TrimPrefix(rel, "/"), nil
}
