// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package errs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel values, one per Kind. Use with `errors.Is` for kind-only
// matching.
var (
	ErrConfigNotFound          = &Error{Kind: ConfigNotFound}
	ErrConfigAlreadyExists     = &Error{Kind: ConfigAlreadyExists}
	ErrKitNotFound             = &Error{Kind: KitNotFound}
	ErrKitAlreadyInstalled     = &Error{Kind: KitAlreadyInstalled}
	ErrKitNotInstalled         = &Error{Kind: KitNotInstalled}
	ErrInvalidName             = &Error{Kind: InvalidName}
	ErrDuplicateName           = &Error{Kind: DuplicateName}
	ErrReservedName            = &Error{Kind: ReservedName}
	ErrSourceNotFound          = &Error{Kind: SourceNotFound}
	ErrSourceAlreadyExists     = &Error{Kind: SourceAlreadyExists}
	ErrSourceInUse             = &Error{Kind: SourceInUse}
	ErrAgentNotFound           = &Error{Kind: AgentNotFound}
	ErrAgentAlreadyActive      = &Error{Kind: AgentAlreadyActive}
	ErrAgentNotActive          = &Error{Kind: AgentNotActive}
	ErrManagedSectionCorrupted = &Error{Kind: ManagedSectionCorrupted}
	ErrKitValidation           = &Error{Kind: KitValidation}
	ErrRegistryValidation      = &Error{Kind: RegistryValidation}
	ErrGit                     = &Error{Kind: Git}
	ErrIO                      = &Error{Kind: IO}
	ErrJSON                    = &Error{Kind: JSON}
	ErrUnsupportedPlatform     = &Error{Kind: UnsupportedPlatform}
	ErrRunningViaGoRun         = &Error{Kind: RunningViaGoRun}
	ErrNoReleaseFound          = &Error{Kind: NoReleaseFound}
	ErrVersionNotPublished     = &Error{Kind: VersionNotPublished}
	ErrDownloadFailed          = &Error{Kind: DownloadFailed}
	ErrChecksumMismatch        = &Error{Kind: ChecksumMismatch}
	ErrArchiveLayout           = &Error{Kind: ArchiveLayout}
	ErrUnrecoverableSwap       = &Error{Kind: UnrecoverableSwapFailure}
	ErrSyncDivergence          = &Error{Kind: SyncDivergence}
	ErrKitsNeedAttention       = &Error{Kind: KitsNeedAttention}
	ErrRenameTreeMismatch      = &Error{Kind: RenameTreeMismatch}
	ErrRenameCrossSource       = &Error{Kind: RenameCrossSource}
	ErrReplaceInstallFailed    = &Error{Kind: ReplaceInstallFailed}
	ErrRenameAmbiguous         = &Error{Kind: RenameAmbiguous}
	ErrSnapshotCaptureFailed   = &Error{Kind: SnapshotCaptureFailed}
	ErrApplyRestoreFailed      = &Error{Kind: ApplyRestoreFailed}
	ErrHomeAsProject           = &Error{Kind: HomeAsProject}
	ErrConfigInvalid           = &Error{Kind: ConfigInvalid}
)

// Constructors. One per variant; mirror the Rust struct-initialization
// patterns used at error-creation sites.

func NewConfigNotFound(path string) *Error      { return &Error{Kind: ConfigNotFound, Path: path} }
func NewConfigAlreadyExists(path string) *Error { return &Error{Kind: ConfigAlreadyExists, Path: path} }
func NewHomeAsProject(home string) *Error       { return &Error{Kind: HomeAsProject, Path: home} }
func NewConfigInvalid(path, reason string) *Error {
	return &Error{Kind: ConfigInvalid, Path: path, Reason: reason}
}

func NewKitNotFound(name, sourceName string) *Error {
	return &Error{Kind: KitNotFound, Name: name, SourceName: sourceName}
}
func NewKitAlreadyInstalled(name string) *Error {
	return &Error{Kind: KitAlreadyInstalled, Name: name}
}
func NewKitNotInstalled(name string) *Error { return &Error{Kind: KitNotInstalled, Name: name} }

func NewInvalidName(name, reason string) *Error {
	return &Error{Kind: InvalidName, Name: name, Reason: reason}
}
func NewDuplicateName(name, existingKit string) *Error {
	return &Error{Kind: DuplicateName, Name: name, ExistingKit: existingKit}
}
func NewReservedName(name string) *Error { return &Error{Kind: ReservedName, Name: name} }

func NewSourceNotFound(name string) *Error      { return &Error{Kind: SourceNotFound, Name: name} }
func NewSourceAlreadyExists(name string) *Error { return &Error{Kind: SourceAlreadyExists, Name: name} }
func NewSourceInUse(name string) *Error         { return &Error{Kind: SourceInUse, Name: name} }

func NewAgentNotFound(name string) *Error      { return &Error{Kind: AgentNotFound, Name: name} }
func NewAgentAlreadyActive(name string) *Error { return &Error{Kind: AgentAlreadyActive, Name: name} }
func NewAgentNotActive(name string) *Error     { return &Error{Kind: AgentNotActive, Name: name} }

func NewManagedSectionCorrupted(path string) *Error {
	return &Error{Kind: ManagedSectionCorrupted, Path: path}
}

func NewKitValidation(reason string) *Error { return &Error{Kind: KitValidation, Reason: reason} }
func NewRegistryValidation(reason string) *Error {
	return &Error{Kind: RegistryValidation, Reason: reason}
}

func WrapGit(cause error) *Error  { return &Error{Kind: Git, Cause: cause} }
func WrapIO(cause error) *Error   { return &Error{Kind: IO, Cause: cause} }
func WrapJSON(cause error) *Error { return &Error{Kind: JSON, Cause: cause} }

// Self-update error constructors. See spec 0015 §4 for the failure modes.

func NewUnsupportedPlatform(goosArch string) *Error {
	return &Error{Kind: UnsupportedPlatform, Reason: goosArch}
}
func NewRunningViaGoRun(path string) *Error {
	return &Error{Kind: RunningViaGoRun, Path: path}
}
func NewNoReleaseFound() *Error { return &Error{Kind: NoReleaseFound} }
func NewVersionNotPublished(version, target string) *Error {
	return &Error{Kind: VersionNotPublished, Name: version, Reason: target}
}
func NewDownloadFailed(url string, cause error) *Error {
	return &Error{Kind: DownloadFailed, Path: url, Cause: cause}
}
func NewChecksumMismatch(archive, detail string) *Error {
	return &Error{Kind: ChecksumMismatch, Name: archive, Reason: detail}
}
func NewArchiveLayout(reason string) *Error { return &Error{Kind: ArchiveLayout, Reason: reason} }
func NewUnrecoverableSwap(reason string, cause error) *Error {
	return &Error{Kind: UnrecoverableSwapFailure, Reason: reason, Cause: cause}
}

// Kit-reorganisation error constructors. See spec 0016 §4.6 for the
// failure modes.

func NewSyncDivergence(count int) *Error {
	return &Error{Kind: SyncDivergence, Count: count}
}
func NewKitsNeedAttention(count int) *Error {
	return &Error{Kind: KitsNeedAttention, Count: count}
}
func NewRenameTreeMismatch(oldKey, newKey, oldSHA, newSHA string) *Error {
	return &Error{
		Kind:        RenameTreeMismatch,
		Name:        oldKey,
		ExistingKit: newKey,
		Reason:      fmt.Sprintf("recorded tree %s != new tree %s", oldSHA, newSHA),
	}
}
func NewRenameCrossSource(oldSrc, newSrc string) *Error {
	return &Error{Kind: RenameCrossSource, Name: oldSrc, SourceName: newSrc}
}
func NewReplaceInstallFailed(oldKey, newKey string, cause error) *Error {
	return &Error{
		Kind:        ReplaceInstallFailed,
		Name:        oldKey,
		ExistingKit: newKey,
		Cause:       cause,
	}
}

// NewRenameAmbiguous reports that auto-rename detection found the
// recorded tree SHA at two or more different names in the same source's
// registry. Candidates are sorted alphabetically and packed into the
// existing Reason field as a comma-separated list — no new struct
// fields. See spec 0018 §4.8.
func NewRenameAmbiguous(installKey string, candidates []string) *Error {
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	return &Error{
		Kind:   RenameAmbiguous,
		Name:   installKey,
		Reason: strings.Join(sorted, ", "),
	}
}

// NewSnapshotCaptureFailed wraps an I/O failure encountered while
// capturing the apply-phase snapshot. The caller has guaranteed no
// project mutation has occurred. See spec 0021 §4.6.
func NewSnapshotCaptureFailed(installKey string, cause error) *Error {
	return &Error{Kind: SnapshotCaptureFailed, Name: installKey, Cause: cause}
}

// NewApplyRestoreFailed reports that an apply failure was followed by a
// failure restoring from the snapshot. Both errors are joined into the
// Cause chain so errors.Is / errors.As walk both sides. The snapshot
// directory is preserved on disk and named via the Path field for
// operator recovery. See spec 0021 §4.6.
func NewApplyRestoreFailed(installKey, snapshotDir string, applyErr, restoreErr error) *Error {
	return &Error{
		Kind:  ApplyRestoreFailed,
		Name:  installKey,
		Path:  snapshotDir,
		Cause: errors.Join(applyErr, restoreErr),
	}
}
