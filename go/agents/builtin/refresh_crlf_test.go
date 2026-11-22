// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/hash"

	"github.com/schubergphilis/inat/core/scope"
)

// crlf converts s to CRLF line endings. The embedded rule may already be
// CRLF when built from a Windows checkout, so normalise to LF first.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// TestRefreshInatUsageCRLFCopyOfEmbeddedBackfills: issue #2. With no
// record, a CRLF checkout of the embedded rule matches it and is
// backfilled silently, not reported as modified.
func TestRefreshInatUsageCRLFCopyOfEmbeddedBackfills(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", crlf(inatUsageRule))
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", nil, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected silent backfill, got nil")
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (CRLF copy is not an edit)", modified)
	}
}

// TestRefreshInatUsageCRLFStaleCopyUpgrades: issue #2. A CRLF checkout
// of an older delivered version still counts as unmodified, so the rule
// upgrades to the embedded content without a warning.
func TestRefreshInatUsageCRLFStaleCopyUpgrades(t *testing.T) {
	dir := t.TempDir()
	oldContent := "old inat-usage content\nsecond line\n"
	writeRuleFile(t, dir, "inat-usage.md", crlf(oldContent))
	recorded := &config.DeliveredFile{
		Path: ".inat/rules/inat-usage.md",
		Hash: hash.HashBytes([]byte(oldContent)),
	}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (CRLF copy is not an edit)", modified)
	}
	if delivered == nil || delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("delivered = %+v, want embedded hash", delivered)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != inatUsageRule {
		t.Errorf("rule not upgraded to embedded content")
	}
}
