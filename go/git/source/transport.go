// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// userAgent is the User-Agent header injected onto every git smart-HTTP
// request issued by go-git. Azure DevOps's HTTP frontend gates the
// `info/refs?service=git-upload-pack` handshake on a `git/<version>`
// prefix and rejects non-matching UAs with HTTP 400 before auth is
// evaluated. The version need not match a real git release — the prefix
// is what the gate checks — but we keep it stable across builds so
// reproductions can identify the inat binary that emitted it.
const userAgent = "git/2.40.0"

// multiAckCapability is the protocol-v0 capability flag Azure DevOps
// requires the client to advertise on its first `want` pkt-line. Without
// it, Azure DevOps's git server rejects the upload-pack POST with
//
//	HTTP 400 / X-Tfs-Exception: GitProtocolException
//	body: TF401041: The Git protocol sent is not as expected
//	      (Clients must support multi-ack.).
//
// go-git v5.13.2 advertises only `side-band-64k no-progress` and does
// not implement multi_ack handling, but advertising the capability is
// sufficient for `git clone` against Azure DevOps because the initial
// fetch has no `have` lines and the multi_ack response branch is never
// taken in practice. See go-git/go-git#64.
const multiAckCapability = "multi_ack"

// uaTransport is an http.RoundTripper that wraps an inner transport and
// applies the per-host workarounds inat needs to successfully clone /
// fetch from Azure DevOps:
//
//  1. Injects User-Agent: git/<version> on every request (User-Agent
//     gate; rejects all non-matching clients at the smart-HTTP
//     handshake).
//  2. Rewrites the POST body to `/git-upload-pack` so the first `want`
//     pkt-line advertises the `multi_ack` capability (multi-ack gate;
//     rejects clients that do not advertise it).
//
// The inbound request is cloned before mutation per the
// http.RoundTripper contract. Both rewrites are no-ops for hosts that
// don't gate on them (GitLab, GitHub, generic HTTPS), so the transport
// is safe to install globally.
type uaTransport struct {
	inner http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("User-Agent", userAgent)
	if isUploadPackPost(cloned) && cloned.Body != nil {
		body, err := io.ReadAll(cloned.Body)
		if err != nil {
			return nil, err
		}
		_ = cloned.Body.Close()
		patched := patchWantCapabilities(body, multiAckCapability)
		cloned.Body = io.NopCloser(bytes.NewReader(patched))
		cloned.ContentLength = int64(len(patched))
	}
	debugRequest(cloned)
	resp, err := t.inner.RoundTrip(cloned)
	debugResponse(resp, err)
	return resp, err
}

// isUploadPackPost reports whether req is the smart-HTTP POST that
// negotiates the packfile (the second leg of a clone/fetch).
func isUploadPackPost(req *http.Request) bool {
	if req.Method != http.MethodPost || req.URL == nil {
		return false
	}
	return strings.HasSuffix(req.URL.Path, "/git-upload-pack")
}

// patchWantCapabilities adds cap to the capability list on the first
// `want` pkt-line in body, recomputing the line's 4-hex-char length
// prefix. Returns body unchanged if the first pkt-line is malformed,
// not a `want` line, or already advertises cap.
//
// Pkt-line format: 4 hex chars (total length including the prefix) +
// payload. The first `want` line of an upload-pack request looks like
//
//	0067want <40-char-sha> side-band-64k no-progress\n
//
// where capabilities follow the SHA on a single space-separated list
// terminated by LF. Subsequent `want` lines carry only a SHA and no
// capabilities, so we only ever rewrite the first one.
func patchWantCapabilities(body []byte, cap string) []byte {
	if len(body) < 4 {
		return body
	}
	n, err := strconv.ParseUint(string(body[:4]), 16, 32)
	if err != nil || n < 4 || int(n) > len(body) {
		return body
	}
	line := body[4:n]
	if !bytes.HasPrefix(line, []byte("want ")) {
		return body
	}
	// Need at least: "want " + 40-char sha + " " + 1-char cap + "\n"
	if len(line) < len("want ")+40+1+1+1 {
		return body
	}
	if bytes.Contains(line, []byte(cap)) {
		return body
	}
	insert := []byte(" " + cap)
	var newLine []byte
	if line[len(line)-1] == '\n' {
		newLine = make([]byte, 0, len(line)+len(insert))
		newLine = append(newLine, line[:len(line)-1]...)
		newLine = append(newLine, insert...)
		newLine = append(newLine, '\n')
	} else {
		newLine = make([]byte, 0, len(line)+len(insert))
		newLine = append(newLine, line...)
		newLine = append(newLine, insert...)
	}
	newTotal := 4 + len(newLine)
	prefix := []byte(fmt.Sprintf("%04x", newTotal))
	out := make([]byte, 0, len(prefix)+len(newLine)+(len(body)-int(n)))
	out = append(out, prefix...)
	out = append(out, newLine...)
	out = append(out, body[n:]...)
	return out
}

func debugRequest(req *http.Request) {
	if os.Getenv("INAT_DEBUG_HTTP") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP] %s %s\n", req.Method, req.URL)
	for k, v := range req.Header {
		fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP]   %s: %v\n", k, v)
	}
	if req.Body == nil {
		return
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return
	}
	_ = req.Body.Close()
	fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP]   body (%d bytes): %q\n", len(b), string(b))
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.ContentLength = int64(len(b))
}

func debugResponse(resp *http.Response, err error) {
	if os.Getenv("INAT_DEBUG_HTTP") == "" {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP] err: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP] <- %s\n", resp.Status)
	if resp.StatusCode >= 400 && resp.Body != nil {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		fmt.Fprintf(os.Stderr, "[INAT_DEBUG_HTTP]   body (%d bytes): %s\n", len(body), string(body))
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
}

var registerOnce sync.Once

// RegisterHTTPTransport installs the UA-injecting HTTP transport into
// go-git's protocol registry for both `https` and `http` schemes. Safe
// to call repeatedly: the first call wins via sync.Once.
//
// The override is process-global. Called automatically from this
// package's init() so any code path that imports `git/source` gets it
// for free; main also calls it explicitly to keep the dependency
// visible in the binary entry point.
func RegisterHTTPTransport() {
	registerOnce.Do(func() {
		c := githttp.NewClient(&http.Client{
			Transport: &uaTransport{inner: http.DefaultTransport},
		})
		client.InstallProtocol("https", c)
		client.InstallProtocol("http", c)
	})
}

func init() {
	RegisterHTTPTransport()
}
