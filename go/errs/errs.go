// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package errs is the Go equivalent of the Rust implementation's `src/error.rs`'s `InatError`
// enum. The directory is named `errs` (rather than the spec's nominal
// `error/`) to avoid shadowing Go's built-in `error` interface inside the
// package.
//
// Two ways to inspect an inat error:
//
//   - Kind-only check (sentinel pattern):
//     if errors.Is(err, errs.ErrAgentNotFound) { ... }
//
//   - Kind + parameter check:
//     var e *errs.Error
//     if errors.As(err, &e) && e.Kind == errs.AgentNotFound && e.Name == "claude-code" { ... }
package errs

import (
	"errors"
	"fmt"
)

// Kind identifies a specific InatError variant.
type Kind int

const (
	_ Kind = iota
	ConfigNotFound
	ConfigAlreadyExists
	KitNotFound
	KitAlreadyInstalled
	KitNotInstalled
	InvalidName
	DuplicateName
	ReservedName
	SourceNotFound
	SourceAlreadyExists
	SourceInUse
	AgentNotFound
	AgentAlreadyActive
	AgentNotActive
	ManagedSectionCorrupted
	KitValidation
	RegistryValidation
	Git
	IO
	JSON
	UnsupportedPlatform
	RunningViaGoRun
	NoReleaseFound
	VersionNotPublished
	DownloadFailed
	ChecksumMismatch
	ArchiveLayout
	UnrecoverableSwapFailure
	SyncDivergence
	KitsNeedAttention
	RenameTreeMismatch
	RenameCrossSource
	ReplaceInstallFailed
	RenameAmbiguous
	SnapshotCaptureFailed
	ApplyRestoreFailed
	HomeAsProject
	ConfigInvalid
)

// Error is the typed inat error. Mirrors `enum InatError` in Rust.
//
// Different variants populate different fields; `Error()` knows which
// fields belong to which `Kind`.
type Error struct {
	Kind        Kind
	Path        string // ConfigNotFound, ConfigAlreadyExists, ManagedSectionCorrupted
	Name        string // KitNotFound, KitAlreadyInstalled, KitNotInstalled, InvalidName, DuplicateName, ReservedName, SourceXxx, AgentXxx, RenameXxx (oldKey/oldSrc), ReplaceInstallFailed (oldKey)
	SourceName  string // KitNotFound, RenameCrossSource (newSrc)
	ExistingKit string // DuplicateName, RenameTreeMismatch (newKey), ReplaceInstallFailed (newKey)
	Reason      string // InvalidName, KitValidation, RegistryValidation, RenameTreeMismatch (recorded != new)
	Count       int    // SyncDivergence, KitsNeedAttention
	Cause       error  // Git, IO, JSON, ReplaceInstallFailed (wrapped underlying error)
}

// Error returns the human-readable error string. Texts mirror Rust's
// `#[error(...)]` attributes verbatim so error-message tests stay parity.
func (e *Error) Error() string {
	switch e.Kind {
	case ConfigNotFound:
		return "config not found at " + e.Path
	case ConfigAlreadyExists:
		return "config already exists at " + e.Path
	case HomeAsProject:
		return fmt.Sprintf("home directory %s cannot be a project; use --user for the user scope", e.Path)
	case ConfigInvalid:
		return fmt.Sprintf("invalid config at %s: %s", e.Path, e.Reason)
	case KitNotFound:
		return fmt.Sprintf("kit '%s' not found in source '%s'", e.Name, e.SourceName)
	case KitAlreadyInstalled:
		return fmt.Sprintf("kit '%s' already installed", e.Name)
	case KitNotInstalled:
		return fmt.Sprintf("kit '%s' is not installed", e.Name)
	case InvalidName:
		return fmt.Sprintf("name '%s' is invalid: %s", e.Name, e.Reason)
	case DuplicateName:
		return fmt.Sprintf("duplicate name '%s' already delivered by kit '%s'", e.Name, e.ExistingKit)
	case ReservedName:
		return fmt.Sprintf("name '%s' is reserved by inat and cannot be used by a kit", e.Name)
	case SourceNotFound:
		return fmt.Sprintf("source '%s' not found", e.Name)
	case SourceAlreadyExists:
		return fmt.Sprintf("source '%s' already exists", e.Name)
	case SourceInUse:
		return fmt.Sprintf("source '%s' still has installed kits", e.Name)
	case AgentNotFound:
		return fmt.Sprintf("agent profile '%s' not found", e.Name)
	case AgentAlreadyActive:
		return fmt.Sprintf("agent '%s' is already active in this project", e.Name)
	case AgentNotActive:
		return fmt.Sprintf("agent '%s' is not active in this project", e.Name)
	case ManagedSectionCorrupted:
		return "managed section markers missing or corrupted in " + e.Path
	case KitValidation:
		return "kit validation failed: " + e.Reason
	case RegistryValidation:
		return "registry validation failed: " + e.Reason
	case Git:
		return "git operation failed: " + causeString(e.Cause)
	case IO:
		return "file I/O error: " + causeString(e.Cause)
	case JSON:
		return "JSON parse error: " + causeString(e.Cause)
	case UnsupportedPlatform:
		return fmt.Sprintf("no release artifact for %s; install from source", e.Reason)
	case RunningViaGoRun:
		return fmt.Sprintf("self update refused: running via 'go run' (%s); install a release binary first", e.Path)
	case NoReleaseFound:
		return "no release found in the package registry"
	case VersionNotPublished:
		return fmt.Sprintf("release '%s' not published for target '%s'", e.Name, e.Reason)
	case DownloadFailed:
		return fmt.Sprintf("download failed for %s: %s", e.Path, causeString(e.Cause))
	case ChecksumMismatch:
		return fmt.Sprintf("checksum mismatch for %s: %s", e.Name, e.Reason)
	case ArchiveLayout:
		return "release archive layout unexpected: " + e.Reason
	case UnrecoverableSwapFailure:
		return "self update left the binary in an inconsistent state: " + e.Reason +
			"; cause: " + causeString(e.Cause)
	case SyncDivergence:
		return fmt.Sprintf("%d %s need%s attention", e.Count, kitWord(e.Count), thirdPersonS(e.Count))
	case KitsNeedAttention:
		return fmt.Sprintf("%d %s need%s attention; run `inat sync` for guidance",
			e.Count, kitWord(e.Count), thirdPersonS(e.Count))
	case RenameTreeMismatch:
		return fmt.Sprintf("cannot rename '%s' to '%s': %s; use `inat replace` or `inat update` first",
			e.Name, e.ExistingKit, e.Reason)
	case RenameCrossSource:
		return fmt.Sprintf("rename is single-source only: '%s' and '%s' are different sources; "+
			"use `inat replace` for cross-source moves", e.Name, e.SourceName)
	case ReplaceInstallFailed:
		return fmt.Sprintf("replace '%s' with '%s' failed during install half: %s; the old kit was restored",
			e.Name, e.ExistingKit, causeString(e.Cause))
	case SnapshotCaptureFailed:
		return fmt.Sprintf("snapshot capture failed for kit '%s': %s", e.Name, causeString(e.Cause))
	case ApplyRestoreFailed:
		return fmt.Sprintf("apply failed for kit '%s' and snapshot restore also failed; snapshot kept at %s: %s",
			e.Name, e.Path, causeString(e.Cause))
	case RenameAmbiguous:
		return fmt.Sprintf("auto-rename for '%s' is ambiguous: tree SHA matches %s; "+
			"run `inat sync` for guidance and `inat rename` for explicit resolution",
			e.Name, e.Reason)
	default:
		return "unknown inat error"
	}
}

// kitWord returns "kit" or "kits" depending on count. Used for display
// pluralisation of the spec-0016 count-bearing errors.
func kitWord(n int) string {
	if n == 1 {
		return "kit"
	}
	return "kits"
}

// thirdPersonS returns "s" for n == 1 (singular: "kit needs"), empty
// otherwise (plural: "kits need"). Pairs with kitWord.
func thirdPersonS(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

// Unwrap exposes the wrapped underlying error for `errors.Is`/`errors.As`
// chains. Variants without a wrapped cause return nil.
func (e *Error) Unwrap() error { return e.Cause }

// Is implements `errors.Is` semantics: an `*Error` matches another
// `*Error` if they share the same `Kind`. Parameter fields are ignored —
// callers who need parameter checks use `errors.As` and inspect fields
// directly.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind
}

func causeString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
