// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package sources

import (
	"errors"
	"testing"

	initcmd "github.com/schubergphilis/inat/cli/init"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

func TestDeriveSourceNameHTTPS(t *testing.T) {
	got, ok := deriveSourceName("https://github.com/company/rules.git")
	if !ok || got != "rules" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestDeriveSourceNameNoGitSuffix(t *testing.T) {
	got, _ := deriveSourceName("https://github.com/company/rules")
	if got != "rules" {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveSourceNameSSH(t *testing.T) {
	got, _ := deriveSourceName("git@github.com:company/rules.git")
	if got != "rules" {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveSourceNameTrailingSlash(t *testing.T) {
	got, _ := deriveSourceName("https://github.com/company/rules/")
	if got != "rules" {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveSourceNameBare(t *testing.T) {
	got, _ := deriveSourceName("my-source")
	if got != "my-source" {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveSourceNameEmpty(t *testing.T) {
	if _, ok := deriveSourceName(""); ok {
		t.Fatalf("expected ok=false")
	}
}

func TestResolveExplicitName(t *testing.T) {
	got, err := resolveSourceName("https://example.com/repo.git", "my-source")
	if err != nil || got != "my-source" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveDerivedName(t *testing.T) {
	got, err := resolveSourceName("https://example.com/my-rules.git", "")
	if err != nil || got != "my-rules" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveInvalidExplicitName(t *testing.T) {
	_, err := resolveSourceName("https://example.com/repo.git", "INVALID")
	if !errors.Is(err, errs.ErrInvalidName) {
		t.Fatalf("want InvalidName, got %v", err)
	}
}

func TestResolveInvalidDerivedName(t *testing.T) {
	_, err := resolveSourceName("https://example.com/MyRules.git", "")
	if !errors.Is(err, errs.ErrInvalidName) {
		t.Fatalf("want InvalidName, got %v", err)
	}
}

func TestValidateUnique(t *testing.T) {
	cfg := &config.Config{
		Sources: []config.Source{{Name: "existing", URL: "https://e.com"}},
	}
	if err := validateSourceUnique(cfg, "new-source"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateDuplicate(t *testing.T) {
	cfg := &config.Config{
		Sources: []config.Source{{Name: "company", URL: "https://e.com"}},
	}
	err := validateSourceUnique(cfg, "company")
	if !errors.Is(err, errs.ErrSourceAlreadyExists) {
		t.Fatalf("want SourceAlreadyExists, got %v", err)
	}
}

func TestRemoveSourceSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := initcmd.Init(dir, "", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadConfig(dir)
	cfg.Sources = append(cfg.Sources, config.Source{Name: "test-src", URL: "https://e.com/test.git"})
	if err := config.SaveConfig(dir, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := SourcesRemove(dir, "test-src"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ = config.LoadConfig(dir)
	if len(cfg.Sources) != 0 {
		t.Fatalf("not empty: %+v", cfg.Sources)
	}
}

func TestRemoveSourceNotFound(t *testing.T) {
	dir := t.TempDir()
	_ = initcmd.Init(dir, "", false)
	err := SourcesRemove(dir, "nonexistent")
	if !errors.Is(err, errs.ErrSourceNotFound) {
		t.Fatalf("want SourceNotFound, got %v", err)
	}
}

func TestRemoveSourceInUse(t *testing.T) {
	dir := t.TempDir()
	_ = initcmd.Init(dir, "", false)
	cfg, _ := config.LoadConfig(dir)
	cfg.Sources = append(cfg.Sources, config.Source{Name: "company", URL: "https://e.com/r.git"})
	cfg.Installed["company/coding-standards"] = config.InstalledKit{
		Tree:      "abc123",
		Names:     []string{"review"},
		Delivered: []config.DeliveredFile{},
	}
	_ = config.SaveConfig(dir, &cfg)
	err := SourcesRemove(dir, "company")
	if !errors.Is(err, errs.ErrSourceInUse) {
		t.Fatalf("want SourceInUse, got %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	dir := t.TempDir()
	_ = initcmd.Init(dir, "", false)
	if err := SourcesList(dir); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestListWithoutInit(t *testing.T) {
	dir := t.TempDir()
	err := SourcesList(dir)
	if !errors.Is(err, errs.ErrConfigNotFound) {
		t.Fatalf("want ConfigNotFound, got %v", err)
	}
}

// Path-normalisation tests (port of the Rust implementation's src/cli/sources/path.rs tests).

func TestNormalizeNone(t *testing.T) {
	got, err := NormalizeSourcePath("")
	if err != nil || got != "" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestNormalizeSimple(t *testing.T) {
	got, _ := NormalizeSourcePath("ai-resources")
	if got != "ai-resources" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeStripsSlashes(t *testing.T) {
	got, _ := NormalizeSourcePath("ai-resources/")
	if got != "ai-resources" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeNested(t *testing.T) {
	got, _ := NormalizeSourcePath("deep/nested/dir")
	if got != "deep/nested/dir" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeRejectsAbsolute(t *testing.T) {
	_, err := NormalizeSourcePath("/absolute")
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
}

func TestNormalizeRejectsParent(t *testing.T) {
	_, err := NormalizeSourcePath("foo/../bar")
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
}

func TestNormalizeRejectsCurrent(t *testing.T) {
	_, err := NormalizeSourcePath("foo/./bar")
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
}

func TestNormalizeRejectsBackslash(t *testing.T) {
	_, err := NormalizeSourcePath("foo\\bar")
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
}

func TestNormalizeRejectsOnlySlashes(t *testing.T) {
	_, err := NormalizeSourcePath("///")
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
}
