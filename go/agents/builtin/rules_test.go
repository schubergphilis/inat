// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/errs"

	"github.com/schubergphilis/inat/core/scope"
)

func TestEmbeddedInatUsageHasFrontmatter(t *testing.T) {
	fm, _, found, err := frontmatter.Parse(inatUsageRule)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatalf("inat_usage.md missing frontmatter")
	}
	if fm.Name != reserved.BuiltinRuleName {
		t.Errorf("name = %q, want %q", fm.Name, reserved.BuiltinRuleName)
	}
	if fm.Description == "" {
		t.Errorf("description must be non-empty")
	}
}

func TestEmbeddedInatKitsHasFrontmatter(t *testing.T) {
	fm, _, found, err := frontmatter.Parse(inatKitsRule)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatalf("inat_kits.md missing frontmatter")
	}
	if fm.Name != reserved.BuiltinKitsRuleName {
		t.Errorf("name = %q, want %q", fm.Name, reserved.BuiltinKitsRuleName)
	}
	if fm.Description == "" {
		t.Errorf("description must be non-empty")
	}
}

func TestDeliverInatUsageRendersDescriptionInReference(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	s := string(md)
	if !strings.Contains(s, "- .inat/rules/inat-usage.md — ") {
		t.Fatalf("reference bullet missing em-dash hint: %s", s)
	}
}

func TestDeliverInatUsageWritesRuleAndReference(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	rule, err := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if err != nil {
		t.Fatalf("rule file missing: %v", err)
	}
	if !strings.Contains(string(rule), "Using `inat` in this project") {
		t.Fatalf("rule body mismatch: %s", rule)
	}
	md, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") {
		t.Fatalf("ref missing: %s", md)
	}
}

func TestDeliverInatUsageIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatal(err)
	}
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Count(string(md), ".inat/rules/inat-usage.md") != 1 {
		t.Fatalf("dedup failed: %s", md)
	}
}

func TestRemoveInatUsageDeletesRuleAndReference(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatal(err)
	}
	if err := RemoveInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); !os.IsNotExist(err) {
		t.Fatalf("rule should be gone")
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Contains(string(md), "inat-usage.md") {
		t.Fatalf("ref should be gone: %s", md)
	}
}

func TestRemoveInatUsageNoop(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatalf("noop: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); !os.IsNotExist(err) {
		t.Fatalf("rule should not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("CLAUDE.md should not exist")
	}
}

func TestDeliverInatUsageUnknownAgent(t *testing.T) {
	dir := t.TempDir()
	_, err := DeliverInatUsage(dir, "nope", scope.Project)
	if err == nil || !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
}

func TestDeliverInatKitsAlongsideUsage(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatUsage(dir, "claude-code", scope.Project); err != nil {
		t.Fatal(err)
	}
	if _, err := DeliverInatKits(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); err != nil {
		t.Fatalf("usage missing")
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-kits.md")); err != nil {
		t.Fatalf("kits missing")
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	s := string(md)
	if !strings.Contains(s, "- .inat/rules/inat-usage.md") || !strings.Contains(s, "- .inat/rules/inat-kits.md") {
		t.Fatalf("refs missing: %s", s)
	}
}

func TestDeliverInatKitsIdempotent(t *testing.T) {
	dir := t.TempDir()
	_, _ = DeliverInatKits(dir, "claude-code")
	_, _ = DeliverInatKits(dir, "claude-code")
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Count(string(md), ".inat/rules/inat-kits.md") != 1 {
		t.Fatalf("dedup failed: %s", md)
	}
}

func TestRemoveInatKitsDeletes(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatKits(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveInatKits(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-kits.md")); !os.IsNotExist(err) {
		t.Fatalf("kits rule should be gone")
	}
}

func TestDeliverInatKitsBodyContains(t *testing.T) {
	dir := t.TempDir()
	if _, err := DeliverInatKits(dir, "claude-code"); err != nil {
		t.Fatal(err)
	}
	rule, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-kits.md"))
	if !strings.Contains(string(rule), "Authoring `inat` kits in this repo") {
		t.Fatalf("body mismatch: %s", rule)
	}
}

func TestDeliverInatKitsUnknownAgent(t *testing.T) {
	dir := t.TempDir()
	_, err := DeliverInatKits(dir, "nope")
	if err == nil || !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
}

func TestDeliverInatUsageReturnsHashOfEmbeddedContent(t *testing.T) {
	dir := t.TempDir()
	delivered, err := DeliverInatUsage(dir, "claude-code", scope.Project)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered.Hash != hash.HashBytes([]byte(inatUsageRule)) {
		t.Errorf("hash = %s, want hash of embedded inat_usage.md", delivered.Hash)
	}
	if delivered.Path != ".inat/rules/inat-usage.md" {
		t.Errorf("path = %s, want .inat/rules/inat-usage.md", delivered.Path)
	}
}

func TestDeliverInatKitsReturnsHashOfEmbeddedContent(t *testing.T) {
	dir := t.TempDir()
	delivered, err := DeliverInatKits(dir, "claude-code")
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered.Hash != hash.HashBytes([]byte(inatKitsRule)) {
		t.Errorf("hash = %s, want hash of embedded inat_kits.md", delivered.Hash)
	}
	if delivered.Path != ".inat/rules/inat-kits.md" {
		t.Errorf("path = %s, want .inat/rules/inat-kits.md", delivered.Path)
	}
}

func TestEmbeddedRulesLinkToGitHub(t *testing.T) {
	const want = "(https://github.com/schubergphilis/inat)"
	for name, body := range map[string]string{"inat-usage": inatUsageRule, "inat-kits": inatKitsRule} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing link %s", name, want)
		}
		if n := strings.Count(body, "](https://"); n != 1 {
			t.Errorf("%s: %d links, want only the project link", name, n)
		}
	}
}
