// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

func ptrString(s string) *string { return &s }

func TestRoundTripConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{
			{Name: "company", URL: "https://github.com/company/rules.git"},
		},
		Installed: map[string]InstalledKit{
			"company/coding-standards": {
				Tree:  "abc123",
				Names: []string{"review"},
				Delivered: []DeliveredFile{
					{Path: "review/SKILL.md", Hash: "def456"},
				},
			},
		},
		Agents: []string{"claude-code"},
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !equalConfig(cfg, loaded) {
		t.Errorf("round-trip mismatch:\nsaved: %+v\nloaded: %+v", cfg, loaded)
	}
}

func TestLoadMissingConfigReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrConfigNotFound) {
		t.Errorf("err kind = %v, want ConfigNotFound", err)
	}
}

func TestLoadInvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	inatDir := filepath.Join(dir, ".inat")
	if err := os.MkdirAll(inatDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inatDir, "config.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrJSON) {
		t.Errorf("err kind = %v, want JSON", err)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if _, err := os.Stat(ConfigPath(dir)); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestEnsureCreatesNewConfig(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureConfig(dir); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded.Sources) != 0 || len(loaded.Installed) != 0 || len(loaded.Agents) != 0 {
		t.Errorf("expected empty defaults, got %+v", loaded)
	}
	if loaded.KitsRepo {
		t.Errorf("KitsRepo = true, want false")
	}
}

func TestEnsureExistingConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureConfig(dir); err != nil {
		t.Fatalf("first EnsureConfig: %v", err)
	}
	err := EnsureConfig(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrConfigAlreadyExists) {
		t.Errorf("err kind = %v, want ConfigAlreadyExists", err)
	}
}

func TestLoadConfigDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	inatDir := filepath.Join(dir, ".inat")
	if err := os.MkdirAll(inatDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inatDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Sources == nil || len(loaded.Sources) != 0 {
		t.Errorf("Sources = %v, want empty []", loaded.Sources)
	}
	if loaded.Installed == nil || len(loaded.Installed) != 0 {
		t.Errorf("Installed = %v, want empty map", loaded.Installed)
	}
	if loaded.Agents == nil || len(loaded.Agents) != 0 {
		t.Errorf("Agents = %v, want empty []", loaded.Agents)
	}
	if loaded.KitsRepo {
		t.Errorf("KitsRepo = true, want false")
	}
	if loaded.BuiltinDelivered == nil || len(loaded.BuiltinDelivered) != 0 {
		t.Errorf("BuiltinDelivered = %v, want empty map", loaded.BuiltinDelivered)
	}
}

func TestGlobalConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".inat", "config.json")
	cfg := GlobalConfig{Agents: []string{"claude-code"}}
	if err := SaveGlobalConfigTo(path, &cfg); err != nil {
		t.Fatalf("SaveGlobalConfigTo: %v", err)
	}
	loaded, err := LoadGlobalConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfigFrom: %v", err)
	}
	if len(loaded.Agents) != 1 || loaded.Agents[0] != "claude-code" {
		t.Errorf("Agents = %v, want [claude-code]", loaded.Agents)
	}
}

func TestGlobalConfigMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".inat", "config.json")
	loaded, err := LoadGlobalConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfigFrom: %v", err)
	}
	if len(loaded.Agents) != 0 {
		t.Errorf("Agents = %v, want empty", loaded.Agents)
	}
}

func TestCheckNameUniquenessRejectsReservedName(t *testing.T) {
	cfg := Config{}
	err := CheckNameUniqueness(&cfg, []string{"inat-usage"}, "source/kit")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrReservedName) {
		t.Errorf("err kind = %v, want ReservedName", err)
	}
}

func TestCheckNameUniquenessRejectsInatKitsReservedName(t *testing.T) {
	cfg := Config{}
	err := CheckNameUniqueness(&cfg, []string{"inat-kits"}, "source/kit")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrReservedName) {
		t.Errorf("err kind = %v, want ReservedName", err)
	}
}

func TestCheckNameUniquenessRejectsDuplicate(t *testing.T) {
	cfg := Config{
		Installed: map[string]InstalledKit{
			"source/kit-a": {Names: []string{"review"}},
		},
	}
	err := CheckNameUniqueness(&cfg, []string{"review"}, "source/kit-b")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrDuplicateName) {
		t.Errorf("err kind = %v, want DuplicateName", err)
	}
}

func TestCheckNameUniquenessAllowsSameKit(t *testing.T) {
	cfg := Config{
		Installed: map[string]InstalledKit{
			"source/kit-a": {Names: []string{"review"}},
		},
	}
	if err := CheckNameUniqueness(&cfg, []string{"review"}, "source/kit-a"); err != nil {
		t.Errorf("CheckNameUniqueness allowing same-kit: %v", err)
	}
}

func TestConfigWithKitsRepoRoundTrips(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{}, Installed: map[string]InstalledKit{}, Agents: []string{},
		KitsRepo: true,
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !loaded.KitsRepo {
		t.Errorf("KitsRepo = false, want true")
	}
}

func TestConfigOmitsKitsRepoFieldWhenFalse(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{}, Installed: map[string]InstalledKit{}, Agents: []string{},
		KitsRepo: false,
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "kits_repo") {
		t.Errorf("false kits_repo should be omitted, got: %s", raw)
	}
}

func TestConfigBuiltinDeliveredRoundTrips(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{}, Installed: map[string]InstalledKit{}, Agents: []string{"claude-code"},
		BuiltinDelivered: map[string][]DeliveredFile{
			"claude-code": {
				{Path: ".inat/rules/inat-usage.md", Hash: "abc123"},
				{Path: ".inat/rules/inat-kits.md", Hash: "def456"},
			},
		},
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got, ok := loaded.BuiltinDelivered["claude-code"]
	if !ok {
		t.Fatalf("BuiltinDelivered[claude-code] missing, got %v", loaded.BuiltinDelivered)
	}
	if len(got) != 2 {
		t.Fatalf("BuiltinDelivered[claude-code] = %v, want 2 entries", got)
	}
	if got[0].Path != ".inat/rules/inat-usage.md" || got[0].Hash != "abc123" {
		t.Errorf("entry 0 = %+v, want inat-usage.md/abc123", got[0])
	}
	if got[1].Path != ".inat/rules/inat-kits.md" || got[1].Hash != "def456" {
		t.Errorf("entry 1 = %+v, want inat-kits.md/def456", got[1])
	}
}

func TestConfigOmitsBuiltinDeliveredFieldWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{}, Installed: map[string]InstalledKit{}, Agents: []string{},
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "builtinDelivered") {
		t.Errorf("empty BuiltinDelivered should be omitted, got: %s", raw)
	}
}

func TestConfigSourcePathOmittedWhenNil(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources:   []Source{{Name: "company", URL: "https://example.com"}},
		Installed: map[string]InstalledKit{},
		Agents:    []string{},
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, _ := os.ReadFile(ConfigPath(dir))
	if strings.Contains(string(raw), `"path"`) {
		t.Errorf("nil Source.Path should be omitted, got: %s", raw)
	}
}

func TestConfigSourcePathPreservedWhenSet(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{{
			Name: "company", URL: "https://example.com",
			Path: ptrString("ai-resources"),
		}},
		Installed: map[string]InstalledKit{},
		Agents:    []string{},
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, _ := LoadConfig(dir)
	if loaded.Sources[0].Path == nil || *loaded.Sources[0].Path != "ai-resources" {
		t.Errorf("Path = %v, want pointer to 'ai-resources'", loaded.Sources[0].Path)
	}
}

// TestRustInitOutputLoadsCleanly verifies that the actual JSON written
// by the Rust binary's `inat init --kits-repo --agent claude-code`
// (captured 2026-04-25, commit 6627c58) loads cleanly through Go's
// LoadConfig with all fields preserved. M1 gate per spec 0009 §10
// acceptance test 2.
func TestRustInitOutputLoadsCleanly(t *testing.T) {
	rustInitOutput := `{
  "sources": [],
  "installed": {},
  "agents": [
    "claude-code"
  ],
  "kits_repo": true
}`
	dir := t.TempDir()
	inatDir := filepath.Join(dir, ".inat")
	if err := os.MkdirAll(inatDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inatDir, "config.json"), []byte(rustInitOutput), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded.Sources) != 0 {
		t.Errorf("Sources = %+v, want empty", loaded.Sources)
	}
	if len(loaded.Installed) != 0 {
		t.Errorf("Installed = %+v, want empty", loaded.Installed)
	}
	if len(loaded.Agents) != 1 || loaded.Agents[0] != "claude-code" {
		t.Errorf("Agents = %v, want [claude-code]", loaded.Agents)
	}
	if !loaded.KitsRepo {
		t.Errorf("KitsRepo = false, want true")
	}
}

// TestRustOutputCompatibility verifies that JSON written by the Rust
// binary (handcrafted to mimic its serde output) loads cleanly into Go's
// LoadConfig with all fields preserved. This is the M1 gate per spec 0009.
func TestRustOutputCompatibility(t *testing.T) {
	rustOutput := `{
  "sources": [
    {
      "name": "company",
      "url": "https://github.com/company/rules.git",
      "path": "ai-resources"
    }
  ],
  "installed": {
    "company/coding-standards": {
      "tree": "abc123",
      "names": [
        "review"
      ],
      "delivered": [
        {
          "path": "review/SKILL.md",
          "hash": "def456"
        }
      ]
    }
  },
  "agents": [
    "claude-code"
  ],
  "kits_repo": true
}`
	dir := t.TempDir()
	inatDir := filepath.Join(dir, ".inat")
	if err := os.MkdirAll(inatDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inatDir, "config.json"), []byte(rustOutput), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Name != "company" {
		t.Errorf("Sources = %+v, want one company entry", loaded.Sources)
	}
	if loaded.Sources[0].Path == nil || *loaded.Sources[0].Path != "ai-resources" {
		t.Errorf("Source.Path = %v, want 'ai-resources'", loaded.Sources[0].Path)
	}
	ik, ok := loaded.Installed["company/coding-standards"]
	if !ok {
		t.Fatalf("Installed[company/coding-standards] missing")
	}
	if ik.Tree != "abc123" {
		t.Errorf("Tree = %q, want abc123", ik.Tree)
	}
	if len(ik.Names) != 1 || ik.Names[0] != "review" {
		t.Errorf("Names = %v, want [review]", ik.Names)
	}
	if len(ik.Delivered) != 1 || ik.Delivered[0].Path != "review/SKILL.md" {
		t.Errorf("Delivered = %v, want one entry", ik.Delivered)
	}
	if !loaded.KitsRepo {
		t.Errorf("KitsRepo = false, want true")
	}
}

// TestGoOutputCompatibleWithRust verifies that JSON written by Go's
// SaveConfig parses cleanly back through LoadConfig (semantic round-trip)
// and contains all the field names Rust expects.
func TestGoOutputCompatibleWithRust(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Sources: []Source{
			{Name: "company", URL: "https://example.com", Path: ptrString("pkg")},
		},
		Installed: map[string]InstalledKit{
			"company/coding-standards": {
				Tree:      "abc",
				Names:     []string{"review"},
				Delivered: []DeliveredFile{{Path: "review/SKILL.md", Hash: "def"}},
			},
		},
		Agents:   []string{"claude-code"},
		KitsRepo: true,
	}
	if err := SaveConfig(dir, &cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, _ := os.ReadFile(ConfigPath(dir))
	// Verify all the field names Rust uses appear in the output.
	for _, field := range []string{
		`"sources"`, `"installed"`, `"agents"`, `"kits_repo"`,
		`"name"`, `"url"`, `"path"`, `"tree"`, `"names"`, `"delivered"`, `"hash"`,
	} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("expected field %s in output, got:\n%s", field, raw)
		}
	}
	// Re-parse via the same loader and assert deep equality.
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !equalConfig(cfg, loaded) {
		t.Errorf("round-trip mismatch:\nsaved %+v\nloaded %+v", cfg, loaded)
	}
}

func equalConfig(a, b Config) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
