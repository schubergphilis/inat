// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package errs

import (
	"errors"
	"io"
	"testing"
)

// TestDisplay covers the six display-format cases from the Rust implementation's `src/error.rs`'s
// test module. Texts MUST byte-equal the Rust originals (modulo trivia)
// to preserve user-visible behaviour across the port.
func TestDisplay(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "config_not_found_display",
			err:  NewConfigNotFound(".inat/config.json"),
			want: "config not found at .inat/config.json",
		},
		{
			name: "invalid_name_display",
			err:  NewInvalidName("My-Kit", "uppercase characters not allowed"),
			want: "name 'My-Kit' is invalid: uppercase characters not allowed",
		},
		{
			name: "duplicate_name_display",
			err:  NewDuplicateName("review", "coding-standards"),
			want: "duplicate name 'review' already delivered by kit 'coding-standards'",
		},
		{
			name: "agent_already_active_display",
			err:  NewAgentAlreadyActive("claude-code"),
			want: "agent 'claude-code' is already active in this project",
		},
		{
			name: "agent_not_active_display",
			err:  NewAgentNotActive("claude-code"),
			want: "agent 'claude-code' is not active in this project",
		},
		{
			name: "source_in_use_display",
			err:  NewSourceInUse("company"),
			want: "source 'company' still has installed kits",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestIs covers the kind-only sentinel matching path. `errors.Is(err, ErrXxx)`
// returns true iff `err` is an `*Error` with the matching Kind, regardless
// of parameter fields.
func TestIs(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		sentinel error
		want     bool
	}{
		{"matching kind", NewAgentNotFound("foo"), ErrAgentNotFound, true},
		{"different kind", NewAgentNotFound("foo"), ErrAgentNotActive, false},
		{"same kind regardless of name", NewInvalidName("a", "x"), ErrInvalidName, true},
		{"plain error not match", io.EOF, ErrAgentNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.Is(c.err, c.sentinel); got != c.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", c.err, c.sentinel, got, c.want)
			}
		})
	}
}

// TestAs verifies `errors.As` extracts the `*Error` for parameter inspection,
// the equivalent of Rust's `matches!(err, InatError::AgentNotFound { ref name } if name == "X")`.
func TestAs(t *testing.T) {
	err := NewInvalidName("Bad-Name", "uppercase")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("errors.As failed to extract *Error from %v", err)
	}
	if e.Kind != InvalidName {
		t.Errorf("e.Kind = %v, want %v", e.Kind, InvalidName)
	}
	if e.Name != "Bad-Name" {
		t.Errorf("e.Name = %q, want %q", e.Name, "Bad-Name")
	}
	if e.Reason != "uppercase" {
		t.Errorf("e.Reason = %q, want %q", e.Reason, "uppercase")
	}
}

// TestUnwrap verifies the wrapped error chain works for IO/JSON/Git wrappers.
func TestUnwrap(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := WrapIO(cause)
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(WrapIO(io.ErrUnexpectedEOF), io.ErrUnexpectedEOF) = false, want true")
	}
	if got := err.Error(); got != "file I/O error: unexpected EOF" {
		t.Errorf("Error() = %q, want %q", got, "file I/O error: unexpected EOF")
	}
}

// TestKitNotFoundDisplay covers the two-parameter KitNotFound variant
// (not in the Rust display tests but a sanity check against the format string).
func TestKitNotFoundDisplay(t *testing.T) {
	err := NewKitNotFound("review", "company")
	want := "kit 'review' not found in source 'company'"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestReservedNameDisplay covers ReservedName format string.
func TestReservedNameDisplay(t *testing.T) {
	err := NewReservedName("inat-usage")
	want := "name 'inat-usage' is reserved by inat and cannot be used by a kit"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestSelfUpdateDisplay covers the format strings of the spec-0015 errors.
func TestSelfUpdateDisplay(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "unsupported_platform",
			err:  NewUnsupportedPlatform("freebsd/amd64"),
			want: "no release artifact for freebsd/amd64; install from source",
		},
		{
			name: "running_via_go_run",
			err:  NewRunningViaGoRun("/tmp/go-build1234/exe/inat"),
			want: "self update refused: running via 'go run' (/tmp/go-build1234/exe/inat); install a release binary first",
		},
		{
			name: "no_release_found",
			err:  NewNoReleaseFound(),
			want: "no release found in the package registry",
		},
		{
			name: "version_not_published",
			err:  NewVersionNotPublished("0.4.1", "aarch64-unknown-linux-gnu"),
			want: "release '0.4.1' not published for target 'aarch64-unknown-linux-gnu'",
		},
		{
			name: "checksum_mismatch",
			err:  NewChecksumMismatch("inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz", "expected aaa, got bbb"),
			want: "checksum mismatch for inat-0.4.1-x86_64-unknown-linux-gnu.tar.gz: expected aaa, got bbb",
		},
		{
			name: "archive_layout",
			err:  NewArchiveLayout("no entry matching 'inat' in tarball"),
			want: "release archive layout unexpected: no entry matching 'inat' in tarball",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestDownloadFailedWraps verifies cause-wrapping for DownloadFailed.
func TestDownloadFailedWraps(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := NewDownloadFailed("https://example.test/inat-0.0.0.tar.gz", cause)
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(NewDownloadFailed(..., io.ErrUnexpectedEOF), io.ErrUnexpectedEOF) = false, want true")
	}
	want := "download failed for https://example.test/inat-0.0.0.tar.gz: unexpected EOF"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestUnrecoverableSwapWraps verifies cause-wrapping for the swap-rollback error.
func TestUnrecoverableSwapWraps(t *testing.T) {
	cause := io.ErrClosedPipe
	err := NewUnrecoverableSwap("rollback rename failed", cause)
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is should match wrapped cause")
	}
	want := "self update left the binary in an inconsistent state: rollback rename failed; cause: io: read/write on closed pipe"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestSelfUpdateSentinels verifies kind-only matching for the new sentinels.
func TestSelfUpdateSentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"unsupported_platform", NewUnsupportedPlatform("x/y"), ErrUnsupportedPlatform},
		{"running_via_go_run", NewRunningViaGoRun("/tmp/foo"), ErrRunningViaGoRun},
		{"no_release_found", NewNoReleaseFound(), ErrNoReleaseFound},
		{"version_not_published", NewVersionNotPublished("v", "t"), ErrVersionNotPublished},
		{"download_failed", NewDownloadFailed("u", io.EOF), ErrDownloadFailed},
		{"checksum_mismatch", NewChecksumMismatch("a", "d"), ErrChecksumMismatch},
		{"archive_layout", NewArchiveLayout("x"), ErrArchiveLayout},
		{"unrecoverable_swap", NewUnrecoverableSwap("x", io.EOF), ErrUnrecoverableSwap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errors.Is(c.err, c.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false, want true", c.err, c.sentinel)
			}
		})
	}
}

// TestKitReorgDisplay covers the format strings of the spec-0016 errors.
func TestKitReorgDisplay(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "sync_divergence_zero",
			err:  NewSyncDivergence(0),
			want: "0 kits need attention",
		},
		{
			name: "sync_divergence_one",
			err:  NewSyncDivergence(1),
			want: "1 kit needs attention",
		},
		{
			name: "sync_divergence_many",
			err:  NewSyncDivergence(3),
			want: "3 kits need attention",
		},
		{
			name: "kits_need_attention_one",
			err:  NewKitsNeedAttention(1),
			want: "1 kit needs attention; run `inat sync` for guidance",
		},
		{
			name: "kits_need_attention_many",
			err:  NewKitsNeedAttention(2),
			want: "2 kits need attention; run `inat sync` for guidance",
		},
		{
			name: "rename_tree_mismatch",
			err:  NewRenameTreeMismatch("company/old", "company/new", "abc123", "def456"),
			want: "cannot rename 'company/old' to 'company/new': recorded tree abc123 != new tree def456; use `inat replace` or `inat update` first",
		},
		{
			name: "rename_cross_source",
			err:  NewRenameCrossSource("company", "vendor"),
			want: "rename is single-source only: 'company' and 'vendor' are different sources; use `inat replace` for cross-source moves",
		},
		{
			name: "replace_install_failed",
			err:  NewReplaceInstallFailed("company/old", "vendor/new", io.EOF),
			want: "replace 'company/old' with 'vendor/new' failed during install half: EOF; the old kit was restored",
		},
		{
			name: "snapshot_capture_failed",
			err:  NewSnapshotCaptureFailed("company/foo", io.EOF),
			want: "snapshot capture failed for kit 'company/foo': EOF",
		},
		{
			name: "apply_restore_failed",
			err:  NewApplyRestoreFailed("company/foo", "/tmp/snap", io.EOF, io.ErrUnexpectedEOF),
			want: "apply failed for kit 'company/foo' and snapshot restore also failed; snapshot kept at /tmp/snap: EOF\nunexpected EOF",
		},
		{
			name: "rename_ambiguous_two",
			err:  NewRenameAmbiguous("company/foo", []string{"foo-renamed", "foo-copy"}),
			want: "auto-rename for 'company/foo' is ambiguous: tree SHA matches foo-copy, foo-renamed; run `inat sync` for guidance and `inat rename` for explicit resolution",
		},
		{
			name: "rename_ambiguous_three",
			err:  NewRenameAmbiguous("company/foo", []string{"c", "a", "b"}),
			want: "auto-rename for 'company/foo' is ambiguous: tree SHA matches a, b, c; run `inat sync` for guidance and `inat rename` for explicit resolution",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRenameAmbiguousDoesNotMutateInput verifies the constructor copies
// the candidates slice before sorting — callers' slices must not be
// reordered as a side effect.
func TestRenameAmbiguousDoesNotMutateInput(t *testing.T) {
	in := []string{"c", "a", "b"}
	_ = NewRenameAmbiguous("k", in)
	if in[0] != "c" || in[1] != "a" || in[2] != "b" {
		t.Errorf("constructor mutated caller's slice: got %v, want [c a b]", in)
	}
}

// TestReplaceInstallFailedWraps verifies cause-wrapping for ReplaceInstallFailed.
func TestReplaceInstallFailedWraps(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := NewReplaceInstallFailed("company/old", "vendor/new", cause)
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(NewReplaceInstallFailed(..., io.ErrUnexpectedEOF), io.ErrUnexpectedEOF) = false, want true")
	}
}

// TestApplyRestoreFailedWrapsBothCauses verifies that both the apply
// error and the restore error are recoverable from an ApplyRestoreFailed
// via errors.Is. Cause is errors.Join(applyErr, restoreErr) so both
// chains are walked.
func TestApplyRestoreFailedWrapsBothCauses(t *testing.T) {
	err := NewApplyRestoreFailed("company/foo", "/tmp/snap", io.EOF, io.ErrUnexpectedEOF)
	if !errors.Is(err, io.EOF) {
		t.Errorf("errors.Is(ApplyRestoreFailed, io.EOF) = false, want true")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(ApplyRestoreFailed, io.ErrUnexpectedEOF) = false, want true")
	}
}

// TestSnapshotCaptureFailedWraps verifies cause-wrapping.
func TestSnapshotCaptureFailedWraps(t *testing.T) {
	err := NewSnapshotCaptureFailed("company/foo", io.ErrUnexpectedEOF)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(SnapshotCaptureFailed, io.ErrUnexpectedEOF) = false, want true")
	}
}

// TestKitReorgSentinels verifies kind-only matching for the spec-0016 sentinels.
func TestKitReorgSentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"sync_divergence", NewSyncDivergence(1), ErrSyncDivergence},
		{"kits_need_attention", NewKitsNeedAttention(2), ErrKitsNeedAttention},
		{"rename_tree_mismatch", NewRenameTreeMismatch("a", "b", "c", "d"), ErrRenameTreeMismatch},
		{"rename_cross_source", NewRenameCrossSource("a", "b"), ErrRenameCrossSource},
		{"replace_install_failed", NewReplaceInstallFailed("a", "b", io.EOF), ErrReplaceInstallFailed},
		{"rename_ambiguous", NewRenameAmbiguous("a/b", []string{"x", "y"}), ErrRenameAmbiguous},
		{"snapshot_capture_failed", NewSnapshotCaptureFailed("k", io.EOF), ErrSnapshotCaptureFailed},
		{"apply_restore_failed", NewApplyRestoreFailed("k", "/tmp/s", io.EOF, io.ErrUnexpectedEOF), ErrApplyRestoreFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errors.Is(c.err, c.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false, want true", c.err, c.sentinel)
			}
		})
	}
}

// TestKitReorgFieldExtraction verifies errors.As surfaces parameter fields
// for the spec-0016 errors so callers can branch on (oldKey, newKey, etc.).
func TestKitReorgFieldExtraction(t *testing.T) {
	err := NewRenameTreeMismatch("company/old", "company/new", "abc", "def")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("errors.As failed to extract *Error from %v", err)
	}
	if e.Kind != RenameTreeMismatch {
		t.Errorf("e.Kind = %v, want RenameTreeMismatch", e.Kind)
	}
	if e.Name != "company/old" {
		t.Errorf("e.Name = %q, want %q", e.Name, "company/old")
	}
	if e.ExistingKit != "company/new" {
		t.Errorf("e.ExistingKit = %q, want %q", e.ExistingKit, "company/new")
	}

	div := NewSyncDivergence(7)
	var de *Error
	if !errors.As(div, &de) {
		t.Fatalf("errors.As failed for SyncDivergence")
	}
	if de.Count != 7 {
		t.Errorf("de.Count = %d, want 7", de.Count)
	}
}
