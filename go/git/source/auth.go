// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	httpauth "github.com/go-git/go-git/v5/plumbing/transport/http"
	gossh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// resolveAuth returns the AuthMethod to attach to clone/fetch options for
// the given URL. Mirrors Rust's `git2::Cred::credential_helper` behaviour:
//
//   - SSH URL (starts with `git@` or `ssh://`): try the ssh-agent.
//   - HTTP(S) URL: shell out to `git credential fill` and pass the
//     username/password as BasicAuth.
//   - Local path or unrecognised scheme: no auth.
//
// Errors during credential discovery are non-fatal — go-git will just
// receive nil auth and the upstream may still accept the request (e.g.
// public repos, local clones).
func resolveAuth(rawURL string) transport.AuthMethod {
	if rawURL == "" {
		return nil
	}
	if isSSHURL(rawURL) {
		user := extractSSHUser(rawURL)
		if user == "" {
			user = "git"
		}
		auth, err := gossh.NewSSHAgentAuth(user)
		if err != nil {
			return nil
		}
		return auth
	}
	if isHTTPURL(rawURL) {
		user, pass, ok := credentialFill(rawURL)
		if !ok {
			return nil
		}
		return &httpauth.BasicAuth{Username: user, Password: pass}
	}
	return nil
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func isSSHURL(u string) bool {
	return strings.HasPrefix(u, "ssh://") || (strings.Contains(u, "@") && !strings.Contains(u, "://"))
}

func extractSSHUser(rawURL string) string {
	if strings.HasPrefix(rawURL, "ssh://") {
		parsed, err := url.Parse(rawURL)
		if err == nil && parsed.User != nil {
			return parsed.User.Username()
		}
		return ""
	}
	if at := strings.Index(rawURL, "@"); at > 0 {
		return rawURL[:at]
	}
	return ""
}

// credentialFill invokes `git credential fill` and parses its protocol
// output. Returns (username, password, true) on success.
func credentialFill(rawURL string) (string, string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}
	input := buildCredentialInput(parsed)
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", false
	}
	user, pass := parseCredentialOutput(stdout.String())
	if user == "" && pass == "" {
		return "", "", false
	}
	return user, pass, true
}

func buildCredentialInput(u *url.URL) string {
	var b strings.Builder
	if u.Scheme != "" {
		fmt.Fprintf(&b, "protocol=%s\n", u.Scheme)
	}
	if u.Host != "" {
		fmt.Fprintf(&b, "host=%s\n", u.Host)
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path != "" {
		fmt.Fprintf(&b, "path=%s\n", path)
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			fmt.Fprintf(&b, "username=%s\n", name)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

func parseCredentialOutput(out string) (string, string) {
	var user, pass string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "username":
			user = value
		case "password":
			pass = value
		}
	}
	return user, pass
}
