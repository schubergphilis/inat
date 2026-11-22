// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/schubergphilis/inat/errs"
)

// setupProject lays out a temp "project" + "home" pair and returns both
// absolute paths. The project contains the given files (rel→content); the
// home is empty (snapshot dir will be created under it).
func setupProject(t *testing.T, files map[string]string) (project, home string) {
	t.Helper()
	project = t.TempDir()
	home = t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(project, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir parent for %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return project, home
}

func TestCaptureAndDiscard(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "hello",
	})

	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "company/coding-standards",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if _, err := os.Stat(snap.Dir); err != nil {
		t.Fatalf("snapshot dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap.Dir, "manifest.json")); err != nil {
		t.Errorf("manifest.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap.Dir, "files", ".inat/rules/foo.md")); err != nil {
		t.Errorf("captured file missing: %v", err)
	}

	// Dir name format: <ts>-<sanitized-key>
	name := filepath.Base(snap.Dir)
	re := regexp.MustCompile(`^\d{8}T\d{6}Z-company__coding-standards(-\d+)?$`)
	if !re.MatchString(name) {
		t.Errorf("dir name %q does not match expected pattern", name)
	}

	if err := snap.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(snap.Dir); !os.IsNotExist(err) {
		t.Errorf("snapshot dir still present after Discard: err=%v", err)
	}
}

func TestCaptureAndRestoreByteIdentical(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "original-content",
	})
	target := filepath.Join(project, ".inat/rules/foo.md")

	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if err := os.WriteFile(target, []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := snap.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "original-content" {
		t.Errorf("Restore did not bring back original bytes: got %q", got)
	}
}

func TestCaptureAndRestoreAbsentFile(t *testing.T) {
	project, home := setupProject(t, map[string]string{})

	// Snapshot a path that does not exist yet (apply will create it then fail).
	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/new.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// Simulate apply creating the file mid-flight, then failing.
	if err := os.MkdirAll(filepath.Join(project, ".inat/rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(project, ".inat/rules/new.md")
	if err := os.WriteFile(created, []byte("apply-created"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := snap.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("Restore did not delete apply-created file: err=%v", err)
	}
}

func TestMultiPathCapture(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "rule-one",
		"CLAUDE.md":          "managed-section-content",
	})
	rulePath := filepath.Join(project, ".inat/rules/foo.md")
	claudePath := filepath.Join(project, "CLAUDE.md")

	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md", "CLAUDE.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if err := os.WriteFile(rulePath, []byte("rule-mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte("claude-mutated"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := snap.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, _ := os.ReadFile(rulePath); string(got) != "rule-one" {
		t.Errorf("rule.md not restored: got %q", got)
	}
	if got, _ := os.ReadFile(claudePath); string(got) != "managed-section-content" {
		t.Errorf("CLAUDE.md not restored: got %q", got)
	}
}

func TestCaptureFailureCleanup(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "data",
	})
	// Pre-create a regular file at the snapshots root path so MkdirAll
	// fails predictably across platforms.
	if err := os.MkdirAll(filepath.Join(home, ".inat"), 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(home, ".inat", "snapshots")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err == nil {
		t.Fatalf("Capture should have failed with snapshots root as a file")
	}
	if !errors.Is(err, errs.ErrSnapshotCaptureFailed) {
		t.Errorf("err = %v, want SnapshotCaptureFailed", err)
	}
	if snap != nil {
		t.Errorf("Capture returned non-nil Snapshot on failure: %+v", snap)
	}
	// The blocker file is still a regular file (untouched).
	info, err := os.Stat(blocker)
	if err != nil {
		t.Fatalf("blocker stat: %v", err)
	}
	if info.IsDir() {
		t.Errorf("blocker became a directory; expected untouched regular file")
	}
}

func TestLeftoverDirCollisionRetries(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "data",
	})

	// Freeze the clock so both captures choose the same base directory
	// name, forcing the collision retry to engage.
	frozen := nowFunc
	t.Cleanup(func() { nowFunc = frozen })
	fixedTime, err := time.Parse(timestampFormat, "20260519T085400Z")
	if err != nil {
		t.Fatalf("parse fixed time: %v", err)
	}
	nowFunc = func() time.Time { return fixedTime }

	first, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("first Capture: %v", err)
	}
	marker := filepath.Join(first.Dir, "marker.txt")
	if err := os.WriteFile(marker, []byte("preserve-me"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("second Capture: %v", err)
	}
	if second.Dir == first.Dir {
		t.Fatalf("second snapshot overwrote first: both at %s", first.Dir)
	}
	if !strings.HasSuffix(second.Dir, "-2") {
		t.Errorf("second snapshot dir = %s, want suffix -2", second.Dir)
	}
	if got, _ := os.ReadFile(marker); string(got) != "preserve-me" {
		t.Errorf("first snapshot's marker overwritten: got %q", got)
	}
}

func TestRestoreRejectsSchemaVersionMismatch(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "data",
	})
	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// Overwrite the manifest with a future schema version.
	manifestPath := filepath.Join(snap.Dir, "manifest.json")
	// Marshal the path: pasted raw, a Windows path has backslashes that are invalid JSON escapes.
	projectJSON, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	bad := `{"schemaVersion":2,"installKey":"co/kit","createdAt":"2026-05-19T00:00:00Z","projectDir":` + string(projectJSON) + `,"files":[{"relPath":".inat/rules/foo.md"}]}`
	if err := os.WriteFile(manifestPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	err = snap.Restore()
	if err == nil {
		t.Fatalf("Restore should have rejected schemaVersion=2")
	}
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Errorf("err = %v, want KitValidation", err)
	}
}

func TestDiscardIdempotent(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "data",
	})
	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if err := snap.Discard(); err != nil {
		t.Fatalf("first Discard: %v", err)
	}
	if err := snap.Discard(); err != nil {
		t.Errorf("second Discard should be idempotent, got %v", err)
	}
}

func TestRestoreFailsWhenFilesSubdirMissing(t *testing.T) {
	project, home := setupProject(t, map[string]string{
		".inat/rules/foo.md": "original",
	})
	snap, err := Capture(Request{
		ProjectDir: project, InstallKey: "co/kit",
		RelPaths: []string{".inat/rules/foo.md"}, HomeOverride: home,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	// Simulate snapshot corruption (e.g., operator/process accidentally
	// removed the files/ subdir). Restore must fail with an IO error so
	// the apply layer can surface ApplyRestoreFailed.
	if err := os.RemoveAll(filepath.Join(snap.Dir, filesSubdir)); err != nil {
		t.Fatal(err)
	}
	if err := snap.Restore(); err == nil {
		t.Fatalf("Restore should have failed with files/ missing")
	}
}

func TestSanitizeInstallKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"company/foo", "company__foo"},
		{"co/bar-baz", "co__bar-baz"},
		{"a/b/c", "a__b__c"}, // defensive: multiple slashes
	}
	for _, c := range cases {
		if got := sanitizeInstallKey(c.in); got != c.want {
			t.Errorf("sanitizeInstallKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
