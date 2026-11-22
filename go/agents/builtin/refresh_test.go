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

func writeRuleFile(t *testing.T, projectDir, fileName, content string) {
	t.Helper()
	rulePath := filepath.Join(projectDir, ".inat", "rules", fileName)
	if err := os.MkdirAll(filepath.Dir(rulePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRefreshInatUsageMissingFileRestores(t *testing.T) {
	dir := t.TempDir()
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", nil, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered, got nil")
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty", modified)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if err != nil {
		t.Fatalf("rule file missing after restore: %v", err)
	}
	if string(body) != inatUsageRule {
		t.Errorf("rule content does not match embedded inatUsageRule")
	}
	if delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("delivered.Hash = %s, want embedded hash", delivered.Hash)
	}
}

func TestRefreshInatUsageMissingFileRestoresEvenWithRecord(t *testing.T) {
	dir := t.TempDir()
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-usage.md", Hash: "stale-hash"}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered, got nil")
	}
	if delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("delivered.Hash = %s, want embedded hash", delivered.Hash)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (file-missing always restores)", modified)
	}
}

func TestRefreshInatUsageNoRecordOnDiskMatchesEmbeddedSilentBackfill(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", inatUsageRule)
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", nil, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered (silent backfill), got nil")
	}
	if delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("delivered.Hash = %s, want embedded hash", delivered.Hash)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (silent backfill must not warn)", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != inatUsageRule {
		t.Errorf("file content changed during silent backfill")
	}
}

func TestRefreshInatUsageNoRecordOnDiskDiffersNoForce(t *testing.T) {
	dir := t.TempDir()
	stale := "stale v0.3-style content (no frontmatter)"
	writeRuleFile(t, dir, "inat-usage.md", stale)
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", nil, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered != nil {
		t.Errorf("delivered = %+v, want nil (migration friction must not record)", delivered)
	}
	if modified != ".inat/rules/inat-usage.md" {
		t.Errorf("modified = %q, want .inat/rules/inat-usage.md", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != stale {
		t.Errorf("file overwritten without force: %q", body)
	}
}

func TestRefreshInatUsageNoRecordOnDiskDiffersForce(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", "stale content")
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", nil, true, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered after --force, got nil")
	}
	if delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("delivered.Hash = %s, want embedded hash", delivered.Hash)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (force overrides)", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != inatUsageRule {
		t.Errorf("file not overwritten with embedded content")
	}
}

func TestRefreshInatUsageRecordedMatchesEmbedded(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", inatUsageRule)
	recordedHash := hash.HashBytes([]byte(inatUsageRule))
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-usage.md", Hash: recordedHash}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil || delivered.Hash != recordedHash {
		t.Errorf("delivered = %+v, want preserved recorded hash", delivered)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty", modified)
	}
}

func TestRefreshInatUsageRecordedStaleSafeUpgrade(t *testing.T) {
	dir := t.TempDir()
	oldContent := "old v0.3 inat-usage content without frontmatter"
	writeRuleFile(t, dir, "inat-usage.md", oldContent)
	oldHash := hash.HashBytes([]byte(oldContent))
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-usage.md", Hash: oldHash}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered (criterion 7 path), got nil")
	}
	embeddedHash := hash.HashBytes([]byte(inatUsageRule))
	if delivered.Hash != embeddedHash {
		t.Errorf("delivered.Hash = %s, want new embedded hash", delivered.Hash)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty (criterion 7 must not warn)", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != inatUsageRule {
		t.Errorf("file not upgraded to new embedded content")
	}
}

func TestRefreshInatUsageRecordedStaleUserModifiedNoForce(t *testing.T) {
	dir := t.TempDir()
	userEdit := "user-edited content"
	writeRuleFile(t, dir, "inat-usage.md", userEdit)
	recorded := &config.DeliveredFile{
		Path: ".inat/rules/inat-usage.md",
		Hash: "old-hash-not-matching-on-disk",
	}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil || delivered.Hash != recorded.Hash {
		t.Errorf("delivered = %+v, want preserved recorded hash", delivered)
	}
	if modified != ".inat/rules/inat-usage.md" {
		t.Errorf("modified = %q, want path", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != userEdit {
		t.Errorf("file overwritten despite user edit + no force: %q", body)
	}
}

func TestRefreshInatUsageRecordedStaleUserModifiedForce(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", "user edit")
	recorded := &config.DeliveredFile{
		Path: ".inat/rules/inat-usage.md",
		Hash: "old-hash",
	}
	delivered, modified, err := RefreshInatUsage(dir, "claude-code", recorded, true, scope.Project)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	embeddedHash := hash.HashBytes([]byte(inatUsageRule))
	if delivered == nil || delivered.Hash != embeddedHash {
		t.Errorf("delivered = %+v, want embedded hash after force", delivered)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty after force", modified)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if string(body) != inatUsageRule {
		t.Errorf("file not force-overwritten with embedded content")
	}
}

func TestRefreshInatUsageAlwaysEnsuresBullet(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", inatUsageRule)
	// Wipe CLAUDE.md to simulate a managed-section loss while file is intact.
	_ = os.Remove(filepath.Join(dir, "CLAUDE.md"))
	recordedHash := hash.HashBytes([]byte(inatUsageRule))
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-usage.md", Hash: recordedHash}
	if _, _, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") {
		t.Errorf("bullet not restored: %s", md)
	}
}

func TestRefreshInatUsageBulletEnsuredEvenInWarnBranch(t *testing.T) {
	// User edited the file, no force → warn branch. The bullet must still
	// be sync'd because file edits and managed-section state are independent.
	dir := t.TempDir()
	writeRuleFile(t, dir, "inat-usage.md", "user edit")
	_ = os.Remove(filepath.Join(dir, "CLAUDE.md"))
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-usage.md", Hash: "old-hash"}
	if _, _, err := RefreshInatUsage(dir, "claude-code", recorded, false, scope.Project); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") {
		t.Errorf("bullet not restored in warn branch: %s", md)
	}
}

func TestRefreshInatKitsRecordedStaleSafeUpgrade(t *testing.T) {
	dir := t.TempDir()
	oldContent := "old v0.3 inat-kits content"
	writeRuleFile(t, dir, "inat-kits.md", oldContent)
	oldHash := hash.HashBytes([]byte(oldContent))
	recorded := &config.DeliveredFile{Path: ".inat/rules/inat-kits.md", Hash: oldHash}
	delivered, modified, err := RefreshInatKits(dir, "claude-code", recorded, false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	embeddedHash := hash.HashBytes([]byte(inatKitsRule))
	if delivered == nil || delivered.Hash != embeddedHash {
		t.Errorf("delivered = %+v, want new embedded hash", delivered)
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty", modified)
	}
}

func TestRefreshInatKitsMissingFileRestores(t *testing.T) {
	dir := t.TempDir()
	delivered, modified, err := RefreshInatKits(dir, "claude-code", nil, false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if delivered == nil {
		t.Fatal("expected delivered, got nil")
	}
	if modified != "" {
		t.Errorf("modified = %q, want empty", modified)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-kits.md")); err != nil {
		t.Fatalf("inat-kits.md not restored: %v", err)
	}
}
