// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package self

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

// --- parseSHA256Sums --------------------------------------------------

func TestParseSHA256SumsGNUFormats(t *testing.T) {
	const archive = "inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz"
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "two_spaces",
			body: "deadbeef  inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz\n" +
				"cafef00d  other.zip\n",
			want: "deadbeef",
		},
		{
			name: "star_marker",
			body: "deadbeef *inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz\n",
			want: "deadbeef",
		},
		{
			name: "single_space",
			body: "deadbeef inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz\n",
			want: "deadbeef",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, found, err := parseSHA256Sums(strings.NewReader(c.body), archive)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if !found {
				t.Fatalf("found = false, want true")
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseSHA256SumsMissingEntry(t *testing.T) {
	body := "deadbeef  some-other-archive.zip\n"
	_, found, err := parseSHA256Sums(strings.NewReader(body), "inat-0.4.1-linux.tar.gz")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("found = true, want false")
	}
}

// --- LatestVersion ----------------------------------------------------

func TestLatestVersionStable(t *testing.T) {
	body := `[
        {"tag_name": "v0.4.1", "prerelease": false},
        {"tag_name": "v0.4.0", "prerelease": false},
        {"tag_name": "v0.3.0", "prerelease": false}
    ]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/schubergphilis/inat/releases" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		assertAnonymous(t, r)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	got, err := LatestVersion(srv.Client(), testEndpoints(srv.URL))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "0.4.1" {
		t.Fatalf("got %q, want 0.4.1", got)
	}
}

func TestLatestVersionSkipsPrereleaseFlag(t *testing.T) {
	body := `[
        {"tag_name": "v0.5.0", "prerelease": true},
        {"tag_name": "v0.4.1", "prerelease": false}
    ]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	got, err := LatestVersion(srv.Client(), testEndpoints(srv.URL))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "0.4.1" {
		t.Fatalf("got %q, want 0.4.1", got)
	}
}

func TestLatestVersionSkipsPreReleases(t *testing.T) {
	body := `[
        {"tag_name": "v0.5.0-rc.1"},
        {"tag_name": "v0.4.1"}
    ]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	got, err := LatestVersion(srv.Client(),
		testEndpoints(srv.URL))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "0.4.1" {
		t.Fatalf("got %q, want 0.4.1", got)
	}
}

func TestLatestVersionEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	_, err := LatestVersion(srv.Client(),
		testEndpoints(srv.URL))
	if !errors.Is(err, errs.ErrNoReleaseFound) {
		t.Fatalf("want NoReleaseFound, got %v", err)
	}
}

func TestLatestVersionAllPreReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"tag_name":"v0.5.0-rc.1"}]`)
	}))
	defer srv.Close()
	_, err := LatestVersion(srv.Client(),
		testEndpoints(srv.URL))
	if !errors.Is(err, errs.ErrNoReleaseFound) {
		t.Fatalf("want NoReleaseFound, got %v", err)
	}
}

// --- DownloadFile + IsNotFound ---------------------------------------

func TestDownloadFile404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "file")
	err := DownloadFile(srv.Client(),
		testEndpoints(srv.URL),
		srv.URL+"/missing", dest)
	if !errors.Is(err, errs.ErrDownloadFailed) {
		t.Fatalf("want DownloadFailed, got %v", err)
	}
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound(err) = false, want true; err=%v", err)
	}
}

// --- DefaultEndpoints -------------------------------------------------

func TestDefaultEndpointsGitHubURLs(t *testing.T) {
	ep := DefaultEndpoints("0.9.0")
	cases := map[string]string{
		"archive": ArchiveURL(ep, "0.9.0", "x86_64-unknown-linux-gnu", ".tar.gz"),
		"sums":    SumsURL(ep, "0.9.0"),
		"api":     ep.API,
	}
	want := map[string]string{
		"archive": "https://github.com/schubergphilis/inat/releases/download/v0.9.0/inat-0.9.0-x86_64-unknown-linux-gnu.tar.gz",
		"sums":    "https://github.com/schubergphilis/inat/releases/download/v0.9.0/SHA256SUMS",
		"api":     "https://api.github.com/repos/schubergphilis/inat",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s = %q, want %q", k, got, want[k])
		}
	}
	if ep.UserAgent != "inat/0.9.0" {
		t.Errorf("UserAgent = %q, want inat/0.9.0", ep.UserAgent)
	}
}

// --- VerifySHA256 -----------------------------------------------------

func TestVerifySHA256Match(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	const payload = "hello world"
	if err := os.WriteFile(archive, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256OfBytes([]byte(payload))
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums, []byte(sum+"  a.tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(archive, sums, "a.tar.gz", "0.0.0", "tri"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestVerifySHA256Mismatch(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(archive, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums,
		[]byte("0000000000000000000000000000000000000000000000000000000000000000  a.tar.gz\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifySHA256(archive, sums, "a.tar.gz", "0.0.0", "tri")
	if !errors.Is(err, errs.ErrChecksumMismatch) {
		t.Fatalf("want ChecksumMismatch, got %v", err)
	}
}

func TestVerifySHA256MissingEntryBecomesVersionNotPublished(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(archive, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums, []byte("deadbeef  other.tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifySHA256(archive, sums, "a.tar.gz", "0.0.0", "tri")
	if !errors.Is(err, errs.ErrVersionNotPublished) {
		t.Fatalf("want VersionNotPublished, got %v", err)
	}
}

// --- ResolveTriple ----------------------------------------------------

func TestResolveTripleSupported(t *testing.T) {
	cases := []struct {
		goos, goarch, target, ext, bin string
	}{
		{"linux", "amd64", "x86_64-unknown-linux-gnu", ".tar.gz", "inat"},
		{"linux", "arm64", "aarch64-unknown-linux-gnu", ".tar.gz", "inat"},
		{"windows", "amd64", "x86_64-pc-windows-msvc", ".zip", "inat.exe"},
		{"darwin", "amd64", "x86_64-apple-darwin", ".tar.gz", "inat"},
		{"darwin", "arm64", "aarch64-apple-darwin", ".tar.gz", "inat"},
	}
	for _, c := range cases {
		t.Run(c.goos+"-"+c.goarch, func(t *testing.T) {
			got, err := ResolveTriple(c.goos, c.goarch)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got.Target != c.target || got.Ext != c.ext || got.Bin != c.bin {
				t.Fatalf("got %+v, want target=%s ext=%s bin=%s", got, c.target, c.ext, c.bin)
			}
		})
	}
}

func TestResolveTripleUnsupported(t *testing.T) {
	_, err := ResolveTriple("freebsd", "amd64")
	if !errors.Is(err, errs.ErrUnsupportedPlatform) {
		t.Fatalf("want UnsupportedPlatform, got %v", err)
	}
}

// --- IsRunningViaGoRun ------------------------------------------------

func TestIsRunningViaGoRun(t *testing.T) {
	cases := []struct {
		name      string
		exec, tmp string
		want      bool
	}{
		{"under_go_build", "/tmp/go-build1234/exe/inat", "/tmp", true},
		{"under_tmp_no_go_build", "/tmp/random/inat", "/tmp", false},
		{"installed_path", "/home/u/.local/bin/inat", "/tmp", false},
		{"empty_tmp", "/tmp/go-build/x", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRunningViaGoRun(c.exec, c.tmp); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// --- ExtractBinary ----------------------------------------------------

func TestExtractTarGz(t *testing.T) {
	const want = "binary content"
	archive := buildTarGz(t, "inat-0.0.0-tri/inat", want)
	dest := filepath.Join(t.TempDir(), "out")
	if err := ExtractBinary(archive, ".tar.gz", "inat", dest); err != nil {
		t.Fatalf("err: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractZip(t *testing.T) {
	const want = "windows binary"
	archive := buildZip(t, "inat-0.0.0-tri/inat.exe", want)
	dest := filepath.Join(t.TempDir(), "out.exe")
	if err := ExtractBinary(archive, ".zip", "inat.exe", dest); err != nil {
		t.Fatalf("err: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractTarGzMissingEntry(t *testing.T) {
	archive := buildTarGz(t, "inat-0.0.0-tri/README", "not the binary")
	dest := filepath.Join(t.TempDir(), "out")
	err := ExtractBinary(archive, ".tar.gz", "inat", dest)
	if !errors.Is(err, errs.ErrArchiveLayout) {
		t.Fatalf("want ArchiveLayout, got %v", err)
	}
}

// --- SwapBinary -------------------------------------------------------

func TestSwapBinaryHappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "inat-bin")
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SwapBinary(target, replacement); err != nil {
		t.Fatalf("err: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("post-swap content = %q, want NEW", got)
	}
	// On non-Windows the .old leftover is cleaned up immediately.
	if runtime.GOOS != "windows" {
		if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
			t.Fatalf("expected target.old to be removed on %s; stat err=%v", runtime.GOOS, err)
		}
	}
}

// --- RunSelfUpdate end-to-end ----------------------------------------

func TestRunSelfUpdateAlreadyOnLatest(t *testing.T) {
	srv := releaseServer(t, "0.4.1", "x86_64-unknown-linux-gnu", []byte("ignored"), nil)
	defer srv.Close()
	withFakeBinary(t, func(_ string) {
		err := RunSelfUpdate(Options{
			Endpoints: testEndpoints(srv.URL),
			GOOS:      "linux", GOARCH: "amd64",
			Current: "0.4.1",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
	})
}

func TestRunSelfUpdateCheckUpdateAvailable(t *testing.T) {
	srv := releaseServer(t, "0.4.1", "x86_64-unknown-linux-gnu", []byte("ignored"), nil)
	defer srv.Close()
	withFakeBinary(t, func(_ string) {
		err := RunSelfUpdate(Options{
			Check:     true,
			Endpoints: testEndpoints(srv.URL),
			GOOS:      "linux", GOARCH: "amd64",
			Current: "0.4.0",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
	})
}

func TestRunSelfUpdateHappyPathLinux(t *testing.T) {
	const newPayload = "fresh inat binary"
	tarBytes := tarGzBytes(t, "inat-0.4.1-x86_64-unknown-linux-gnu/inat", newPayload)
	srv := releaseServer(t, "0.4.1", "x86_64-unknown-linux-gnu", tarBytes, nil)
	defer srv.Close()
	withFakeBinary(t, func(binPath string) {
		err := RunSelfUpdate(Options{
			Endpoints: testEndpoints(srv.URL),
			GOOS:      "linux", GOARCH: "amd64",
			Current: "0.4.0",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := os.ReadFile(binPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != newPayload {
			t.Fatalf("binary content = %q, want %q", got, newPayload)
		}
	})
}

func TestRunSelfUpdateChecksumMismatchLeavesBinary(t *testing.T) {
	tarBytes := tarGzBytes(t, "inat-0.4.1-x86_64-unknown-linux-gnu/inat", "fresh")
	// Override the SHA256SUMS to a wrong digest so verification fails.
	srv := releaseServer(t, "0.4.1", "x86_64-unknown-linux-gnu", tarBytes,
		[]byte("0000000000000000000000000000000000000000000000000000000000000000  inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz\n"))
	defer srv.Close()
	withFakeBinary(t, func(binPath string) {
		err := RunSelfUpdate(Options{
			Endpoints: testEndpoints(srv.URL),
			GOOS:      "linux", GOARCH: "amd64",
			Current: "0.4.0",
		})
		if !errors.Is(err, errs.ErrChecksumMismatch) {
			t.Fatalf("want ChecksumMismatch, got %v", err)
		}
		got, err := os.ReadFile(binPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "ORIGINAL" {
			t.Fatalf("binary should be untouched on failure; got %q", got)
		}
		if _, err := os.Stat(binPath + ".old"); !os.IsNotExist(err) {
			t.Fatalf("no .old should remain on a pre-swap failure; stat err=%v", err)
		}
	})
}

func TestRunSelfUpdateForceReDownloads(t *testing.T) {
	tarBytes := tarGzBytes(t, "inat-0.4.1-x86_64-unknown-linux-gnu/inat", "fresh")
	var archiveHits atomic.Int32
	srv := releaseServerCounted(t, "0.4.1", "x86_64-unknown-linux-gnu", tarBytes, nil, &archiveHits)
	defer srv.Close()
	withFakeBinary(t, func(_ string) {
		err := RunSelfUpdate(Options{
			Force:     true,
			Endpoints: testEndpoints(srv.URL),
			GOOS:      "linux", GOARCH: "amd64",
			Current: "0.4.1",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if archiveHits.Load() == 0 {
			t.Fatalf("--force did not trigger archive download")
		}
	})
}

func TestRunSelfUpdateUnsupportedPlatform(t *testing.T) {
	withFakeBinary(t, func(_ string) {
		err := RunSelfUpdate(Options{
			Endpoints: testEndpoints("http://unused"),
			GOOS:      "freebsd", GOARCH: "amd64",
			Current: "0.4.0",
		})
		if !errors.Is(err, errs.ErrUnsupportedPlatform) {
			t.Fatalf("want UnsupportedPlatform, got %v", err)
		}
	})
}

// --- CleanupOldBinary -------------------------------------------------

func TestCleanupOldBinary(t *testing.T) {
	withFakeBinary(t, func(binPath string) {
		old := binPath + ".old"
		if err := os.WriteFile(old, []byte("leftover"), 0o644); err != nil {
			t.Fatal(err)
		}
		CleanupOldBinary()
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatalf("old not removed; stat err=%v", err)
		}
	})
}

// --- helpers ----------------------------------------------------------

// withFakeBinary stages a fake "running binary" in t.TempDir(), points
// executableFn at it, and runs fn with the path. Restores executableFn
// on cleanup.
func withFakeBinary(t *testing.T, fn func(path string)) {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "inat-test")
	if err := os.WriteFile(binPath, []byte("ORIGINAL"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := executableFn
	executableFn = func() (string, error) { return binPath, nil }
	t.Cleanup(func() { executableFn = prev })
	fn(binPath)
}

// releaseServer returns a httptest.Server that mimics the GitHub
// releases API and release-asset download layout for one version. tarBytes is served
// at the archive URL; if sumsOverride is nil, a correct SHA256SUMS is
// served, else the override is served verbatim (used to inject a
// mismatch).
func releaseServer(t *testing.T, version, target string, tarBytes, sumsOverride []byte) *httptest.Server {
	t.Helper()
	return releaseServerCounted(t, version, target, tarBytes, sumsOverride, nil)
}

func releaseServerCounted(t *testing.T, version, target string, tarBytes, sumsOverride []byte, archiveHits *atomic.Int32) *httptest.Server {
	t.Helper()
	archiveName := fmt.Sprintf("inat-%s-%s.tar.gz", version, target)
	correctSums := []byte(sha256OfBytes(tarBytes) + "  " + archiveName + "\n")
	sums := correctSums
	if sumsOverride != nil {
		sums = sumsOverride
	}
	mux := http.NewServeMux()
	assets := "/schubergphilis/inat/releases/download/v" + version + "/"
	mux.HandleFunc("/repos/schubergphilis/inat/releases", func(w http.ResponseWriter, r *http.Request) {
		assertAnonymous(t, r)
		_, _ = fmt.Fprintf(w, `[{"tag_name":"v%s","prerelease":false}]`, version)
	})
	mux.HandleFunc(assets+"SHA256SUMS",
		func(w http.ResponseWriter, r *http.Request) {
			assertAnonymous(t, r)
			_, _ = w.Write(sums)
		})
	mux.HandleFunc(assets+archiveName,
		func(w http.ResponseWriter, r *http.Request) {
			assertAnonymous(t, r)
			if archiveHits != nil {
				archiveHits.Add(1)
			}
			_, _ = w.Write(tarBytes)
		})
	return httptest.NewServer(mux)
}

// testEndpoints points both the API and the download base at one
// httptest server, on the same paths the production hosts use.
func testEndpoints(base string) Endpoints {
	return Endpoints{
		API:       base + "/repos/schubergphilis/inat",
		Download:  base + "/schubergphilis/inat",
		UserAgent: "inat/test",
	}
}

// assertAnonymous fails the test if a request carries any credential.
func assertAnonymous(t *testing.T, r *http.Request) {
	t.Helper()
	for _, h := range []string{"Authorization", "PRIVATE-TOKEN", "Deploy-Token"} {
		if got := r.Header.Get(h); got != "" {
			t.Errorf("%s header sent on %s; requests must be anonymous", h, r.URL.Path)
		}
	}
}

func sha256OfBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func buildTarGz(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(path, tarGzBytes(t, name, content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func tarGzBytes(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
