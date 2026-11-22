// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package self implements `inat self update`. See spec/plan 0015 for the
// contract; this file owns release discovery, download, and SHA256
// verification against GitHub Releases. All requests are anonymous.
package self

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/schubergphilis/inat/errs"
)

// Production GitHub coordinates. Mirror the URLs used by install.sh and
// install.ps1. Not exported / not env-overridable: a runtime knob would
// be a footgun (a malicious mirror could divert update traffic). Tests
// construct an Endpoints value directly with the httptest server URL.
const (
	defaultAPIBase      = "https://api.github.com/repos/schubergphilis/inat"
	defaultDownloadBase = "https://github.com/schubergphilis/inat"
	httpTimeout         = 5 * time.Minute
)

// Endpoints captures the API surface the command calls. API is the
// GitHub REST base for the repo (release list); Download is the repo's
// web URL, under which release assets live at /releases/download/.
type Endpoints struct {
	API       string
	Download  string
	UserAgent string
}

// DefaultEndpoints returns the production GitHub endpoints, stamped
// with the running binary's version in the User-Agent.
func DefaultEndpoints(version string) Endpoints {
	return Endpoints{
		API:       defaultAPIBase,
		Download:  defaultDownloadBase,
		UserAgent: "inat/" + version,
	}
}

// NewHTTPClient returns the http.Client used by all release calls.
// Plain stdlib client with a generous timeout — archives are ≤10 MB.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: httpTimeout}
}

// LatestVersion returns the bare version (no leading "v") of the most
// recent stable release, mirroring install.sh's resolve_version. Pre-
// releases (flagged `prerelease`, or a tag containing `-`) are skipped.
func LatestVersion(client *http.Client, ep Endpoints) (string, error) {
	url := ep.API + "/releases?per_page=50"
	body, err := doGet(client, ep, url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(body).Decode(&releases); err != nil {
		return "", errs.WrapJSON(err)
	}
	for _, r := range releases {
		if !r.Prerelease && !strings.Contains(r.TagName, "-") {
			return strings.TrimPrefix(r.TagName, "v"), nil
		}
	}
	return "", errs.NewNoReleaseFound()
}

// ArchiveURL composes the release-asset URL for the binary archive
// matching the given target triple.
func ArchiveURL(ep Endpoints, version, triple, ext string) string {
	return fmt.Sprintf("%s/releases/download/v%s/inat-%s-%s%s",
		ep.Download, version, version, triple, ext)
}

// SumsURL composes the release-asset URL for the sibling SHA256SUMS file.
func SumsURL(ep Endpoints, version string) string {
	return fmt.Sprintf("%s/releases/download/v%s/SHA256SUMS", ep.Download, version)
}

// DownloadFile streams url into destPath. Returns DownloadFailed on
// network or transport failure; the caller maps a 404 to a richer typed
// error if it has the version/target context.
func DownloadFile(client *http.Client, ep Endpoints, url, destPath string) error {
	body, err := doGet(client, ep, url)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.Create(destPath)
	if err != nil {
		return errs.WrapIO(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, body); err != nil {
		return errs.NewDownloadFailed(url, err)
	}
	return nil
}

// IsNotFound reports whether err is a DownloadFailed with HTTP 404. The
// caller upgrades it to a typed VersionNotPublished error when the
// version+target context is in scope.
func IsNotFound(err error) bool {
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Kind != errs.DownloadFailed || e.Cause == nil {
		return false
	}
	return strings.Contains(e.Cause.Error(), "HTTP 404")
}

// VerifySHA256 reads sumsPath, finds the entry for archiveName, hashes
// archivePath, and compares. archiveName missing from sums →
// VersionNotPublished (release exists but the target's archive does
// not); digest mismatch → ChecksumMismatch.
func VerifySHA256(archivePath, sumsPath, archiveName, version, target string) error {
	sumsFile, err := os.Open(sumsPath)
	if err != nil {
		return errs.WrapIO(err)
	}
	defer sumsFile.Close()
	expected, found, err := parseSHA256Sums(sumsFile, archiveName)
	if err != nil {
		return err
	}
	if !found {
		return errs.NewVersionNotPublished(version, target)
	}
	actual, err := hashFile(archivePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return errs.NewChecksumMismatch(archiveName,
			fmt.Sprintf("expected %s, got %s", expected, actual))
	}
	return nil
}

// hashFile streams path through SHA-256 and returns the hex digest.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errs.WrapIO(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errs.WrapIO(err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseSHA256Sums returns the hex digest for archiveName, plus a found
// bool. Format: GNU coreutils `<digest><sp>[*]<name>`, two spaces or
// space-then-star both supported. Lines without two fields are skipped.
func parseSHA256Sums(r io.Reader, archiveName string) (string, bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		digest := fields[0]
		name := strings.TrimPrefix(fields[1], "*")
		if name == archiveName {
			return digest, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, errs.WrapIO(err)
	}
	return "", false, nil
}

// doGet issues an anonymous GET; redirects (GitHub serves assets from a
// CDN) are followed by the client. Non-200 responses surface as
// DownloadFailed with an HTTP <code> cause string (used by IsNotFound).
func doGet(client *http.Client, ep Endpoints, url string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, errs.NewDownloadFailed(url, err)
	}
	if ep.UserAgent != "" {
		req.Header.Set("User-Agent", ep.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errs.NewDownloadFailed(url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errs.NewDownloadFailed(url, fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	return resp.Body, nil
}
