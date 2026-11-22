// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
)

// captureRoundTripper records every request it sees and returns a
// minimal 200 response. Used by uaTransport unit tests to inspect the
// outbound headers.
type captureRoundTripper struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestUATransportSetsUserAgent(t *testing.T) {
	cap := &captureRoundTripper{}
	tr := &uaTransport{inner: cap}
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if len(cap.requests) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(cap.requests))
	}
	got := cap.requests[0].Header.Get("User-Agent")
	if got != userAgent {
		t.Fatalf("User-Agent: got %q, want %q", got, userAgent)
	}
}

func TestUATransportClonesRequest(t *testing.T) {
	cap := &captureRoundTripper{}
	tr := &uaTransport{inner: cap}
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	const original = "original-ua"
	req.Header.Set("User-Agent", original)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != original {
		t.Fatalf("inbound request UA mutated: got %q, want %q", got, original)
	}
	if got := cap.requests[0].Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("outbound UA: got %q, want %q", got, userAgent)
	}
}

func TestRegisterHTTPTransportIsIdempotent(t *testing.T) {
	// Package init() ran RegisterHTTPTransport once before the test.
	// A second call must not re-register: the registered transport
	// instance has to stay identical.
	first := client.Protocols["https"]
	if first == nil {
		t.Fatalf("https protocol not registered after package init")
	}
	if client.Protocols["http"] == nil {
		t.Fatalf("http protocol not registered after package init")
	}
	RegisterHTTPTransport()
	if client.Protocols["https"] != first {
		t.Fatalf("RegisterHTTPTransport re-registered https: %p != %p", client.Protocols["https"], first)
	}
}

// productionWantBody mirrors the exact pkt-line shape go-git v5.13.2
// sends as the first POST body to /git-upload-pack: a `want <sha>` line
// with `side-band-64k no-progress` capabilities, followed by additional
// `want` lines, terminated by a flush + `done`. Captured 2026-04-30
// against `dev.azure.com` with INAT_DEBUG_HTTP=1.
const productionWantBody = "004cwant 04a669591649fa9a33193bf27525871ea06d079f side-band-64k no-progress\n" +
	"0032want 5df64250b8cf7ee770635b77e00ee0c4ec393402\n" +
	"00000009done\n"

func TestPatchWantCapabilitiesAddsMultiAckToProductionBody(t *testing.T) {
	in := []byte(productionWantBody)
	out := patchWantCapabilities(in, multiAckCapability)
	// The first pkt-line must now contain multi_ack.
	if !bytes.Contains(out[:128], []byte("multi_ack")) {
		t.Fatalf("multi_ack not injected into first pkt-line: %q", string(out))
	}
	// The new prefix must encode the new total length.
	want := "0056want 04a669591649fa9a33193bf27525871ea06d079f side-band-64k no-progress multi_ack\n"
	if !strings.HasPrefix(string(out), want) {
		t.Fatalf("first pkt-line wrong:\n got: %q\nwant prefix: %q", string(out), want)
	}
	// Subsequent pkt-lines untouched.
	tailIn := in[0x4c:]
	tailOut := out[0x56:]
	if !bytes.Equal(tailIn, tailOut) {
		t.Fatalf("tail of body mutated:\n in:  %q\n out: %q", string(tailIn), string(tailOut))
	}
}

func TestPatchWantCapabilitiesNoOpIfAlreadyPresent(t *testing.T) {
	body := []byte("0056want 04a669591649fa9a33193bf27525871ea06d079f side-band-64k no-progress multi_ack\n0009done\n")
	out := patchWantCapabilities(body, multiAckCapability)
	if !bytes.Equal(body, out) {
		t.Fatalf("body mutated despite multi_ack already present")
	}
}

func TestPatchWantCapabilitiesNoOpForMalformedBody(t *testing.T) {
	cases := map[string][]byte{
		"empty":           {},
		"too short":       []byte("00"),
		"non-hex prefix":  []byte("zzzzwant 04a669591649fa9a33193bf27525871ea06d079f side-band-64k\n"),
		"prefix too long": []byte("ffffwant short\n"),
		"non-want first":  []byte("000ehave abcdef\n"),
		"want no caps":    []byte("002fwant 04a669591649fa9a33193bf27525871ea06d079f\n"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out := patchWantCapabilities(body, multiAckCapability)
			if !bytes.Equal(body, out) {
				t.Fatalf("body mutated:\n in:  %q\n out: %q", string(body), string(out))
			}
		})
	}
}

func TestRoundTripPatchesUploadPackPost(t *testing.T) {
	cap := &captureRoundTripper{}
	tr := &uaTransport{inner: cap}
	body := bytes.NewReader([]byte(productionWantBody))
	req, err := http.NewRequest(http.MethodPost, "https://dev.azure.com/org/proj/_git/repo/git-upload-pack", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	captured, err := io.ReadAll(cap.requests[0].Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Contains(captured, []byte("multi_ack")) {
		t.Fatalf("captured POST body lacks multi_ack: %q", string(captured))
	}
	if cap.requests[0].ContentLength != int64(len(captured)) {
		t.Fatalf("Content-Length mismatch: header=%d body=%d", cap.requests[0].ContentLength, len(captured))
	}
}

func TestRoundTripDoesNotPatchNonUploadPackPost(t *testing.T) {
	cap := &captureRoundTripper{}
	tr := &uaTransport{inner: cap}
	body := bytes.NewReader([]byte(productionWantBody))
	req, err := http.NewRequest(http.MethodPost, "https://dev.azure.com/org/proj/_git/repo/git-receive-pack", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	captured, err := io.ReadAll(cap.requests[0].Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if bytes.Contains(captured, []byte("multi_ack")) {
		t.Fatalf("non-upload-pack POST was rewritten: %q", string(captured))
	}
}

func TestGoGitCloneSendsGitUserAgent(t *testing.T) {
	// httptest server records the User-Agent of every incoming
	// request. We respond with 401 to abort the smart-HTTP handshake
	// on the first packet — the captured headers are all we need.
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	// Expected to fail (401, not a real git endpoint). We only care
	// that the captured request advertised a `git/...` UA.
	_, _ = git.PlainClone(tmp, false, &git.CloneOptions{URL: srv.URL})

	mu.Lock()
	defer mu.Unlock()
	if len(captured) == 0 {
		t.Fatalf("no requests captured — go-git did not call the test server")
	}
	for i, ua := range captured {
		if !strings.HasPrefix(ua, "git/") {
			t.Fatalf("request %d User-Agent %q does not start with %q", i, ua, "git/")
		}
		if ua != userAgent {
			t.Fatalf("request %d User-Agent %q != constant %q", i, ua, userAgent)
		}
	}
}
