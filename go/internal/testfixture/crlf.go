// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package testfixture

import (
	"bytes"
	"os"
	"testing"
)

// ConvertToCRLF rewrites the file at path with CRLF line endings, the way
// a git checkout under `core.autocrlf=true` does on Windows. Fails the
// test if the file has no LF to convert, so a fixture change can't turn
// a CRLF test into a no-op.
func ConvertToCRLF(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Contains(content, []byte("\n")) {
		t.Fatalf("%s has no line ending to convert", path)
	}
	lf := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(path, crlf, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
