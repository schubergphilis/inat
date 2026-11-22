// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

// installAndPushReviewV2 installs test-kit and pushes a new upstream
// version of its review skill. Returns project, home and the skill path.
func installAndPushReviewV2(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated review skill.",
	})
	return project, home, filepath.Join(project, ".claude/skills/review/SKILL.md")
}

// TestUpdateCRLFCheckoutIsNotDrift is the regression test for issue #2:
// a delivered file whose only change is CRLF line endings (a Windows
// checkout under autocrlf) must not abort the update as locally modified.
func TestUpdateCRLFCheckoutIsNotDrift(t *testing.T) {
	project, home, skill := installAndPushReviewV2(t)
	testfixture.ConvertToCRLF(t, skill)

	if err := Update(project, "test-source/test-kit", false, false, home); err != nil {
		t.Fatalf("update after CRLF checkout: %v", err)
	}

	got, _ := os.ReadFile(skill)
	if !strings.Contains(string(got), "Review v2") {
		t.Fatalf("new content not delivered: %q", got)
	}
}

// TestUpdateCRLFWithRealEditIsDrift: CRLF tolerance must not hide a real
// edit made on a CRLF checkout.
func TestUpdateCRLFWithRealEditIsDrift(t *testing.T) {
	project, home, skill := installAndPushReviewV2(t)
	if err := os.WriteFile(skill, []byte("# Review\r\nLocal edit.\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Update(project, "test-source/test-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}
	got, _ := os.ReadFile(skill)
	if string(got) != "# Review\r\nLocal edit.\r\n" {
		t.Fatalf("local edit overwritten: %q", got)
	}
}
