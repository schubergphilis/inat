// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package snapshot captures on-disk apply-phase state for inat update
// and inat replace, so a failed apply can be rolled back to byte-identity
// with the pre-apply state. See spec 0021.
//
// The primitive is per-kit, on-disk, single-shot: one Capture per
// applyOne, one Restore on failure (or Discard on success). Snapshots
// live under ~/.inat/snapshots/<UTC-timestamp>-<sanitized-install-key>/
// and survive process crash. Successful applies clean up; failed applies
// leave the directory for forensics.
package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

const (
	manifestSchemaVersion = 1
	manifestFileName      = "manifest.json"
	filesSubdir           = "files"
	timestampFormat       = "20060102T150405Z"
	maxCollisionRetries   = 100
)

// nowFunc is the package's clock seam. Tests in export_test.go swap it
// to freeze time for the collision-retry assertion.
var nowFunc = time.Now

// Snapshot is the handle to a captured on-disk snapshot. Dir is the
// absolute path of the snapshot directory under ~/.inat/snapshots/.
type Snapshot struct {
	Dir string
}

// Request describes one Capture call. ProjectDir is the absolute project
// root. RelPaths are project-relative file paths to capture (caller
// dedupes). HomeOverride honours config.HomeDir's override semantics for
// hermetic tests.
type Request struct {
	ProjectDir   string
	InstallKey   string
	RelPaths     []string
	HomeOverride string
}

// manifest is the JSON-serialised metadata in <snap>/manifest.json.
type manifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	InstallKey    string         `json:"installKey"`
	CreatedAt     string         `json:"createdAt"`
	ProjectDir    string         `json:"projectDir"`
	Files         []manifestFile `json:"files"`
}

type manifestFile struct {
	RelPath string `json:"relPath"`
	// Absent records whether the path existed on disk at capture time.
	// Emitted explicitly (no omitempty) so the manifest is
	// self-describing — operators reading a snapshot's manifest can tell
	// "file existed at capture" from "field missing because schema
	// changed."
	Absent bool `json:"absent"`
}

// Capture creates a new snapshot directory, copies each file's current
// on-disk bytes into it (or records "absent: true"), writes the
// manifest, and returns the *Snapshot. On any failure mid-capture, the
// partially-created dir is removed and the failure wrapped as
// errs.NewSnapshotCaptureFailed. The caller can assume no on-disk
// mutation if an error is returned.
func Capture(req Request) (*Snapshot, error) {
	root, err := snapshotsRoot(req.HomeOverride)
	if err != nil {
		return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, errs.WrapIO(err))
	}
	dir, err := chooseSnapshotDir(root, req.InstallKey)
	if err != nil {
		return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, filesSubdir), 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, errs.WrapIO(err))
	}
	man := manifest{
		SchemaVersion: manifestSchemaVersion,
		InstallKey:    req.InstallKey,
		CreatedAt:     nowFunc().UTC().Format(time.RFC3339),
		ProjectDir:    req.ProjectDir,
	}
	for _, rel := range req.RelPaths {
		absent, err := copyFileIntoSnapshot(dir, req.ProjectDir, rel)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, err)
		}
		man.Files = append(man.Files, manifestFile{RelPath: rel, Absent: absent})
	}
	if err := writeManifest(dir, &man); err != nil {
		_ = os.RemoveAll(dir)
		return nil, errs.NewSnapshotCaptureFailed(req.InstallKey, err)
	}
	return &Snapshot{Dir: dir}, nil
}

// Restore re-reads the manifest from disk, validates its schema version,
// and restores every captured file. Files marked absent are deleted from
// the project; present files are written back from <snap>/files/<rel>.
// Returns the first failure encountered; the snapshot directory is NOT
// removed by Restore.
func (s *Snapshot) Restore() error {
	man, err := readManifest(s.Dir)
	if err != nil {
		return err
	}
	if man.SchemaVersion != manifestSchemaVersion {
		return errs.NewKitValidation(fmt.Sprintf(
			"snapshot manifest schemaVersion %d unsupported (this binary expects %d)",
			man.SchemaVersion, manifestSchemaVersion))
	}
	for _, f := range man.Files {
		if err := restoreFileFromSnapshot(s.Dir, man.ProjectDir, f); err != nil {
			return err
		}
	}
	return nil
}

// Discard removes the snapshot directory. Idempotent: a missing dir is
// not an error.
func (s *Snapshot) Discard() error {
	if err := os.RemoveAll(s.Dir); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

// snapshotsRoot resolves ~/.inat/snapshots, honouring homeOverride if
// non-empty.
func snapshotsRoot(homeOverride string) (string, error) {
	home := homeOverride
	if home == "" {
		h, err := config.HomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.Join(home, ".inat", "snapshots"), nil
}

// chooseSnapshotDir picks an unused directory under root for this
// capture. Base name is "<ts>-<sanitized-key>"; collision retries append
// "-2", "-3", … up to maxCollisionRetries attempts total.
func chooseSnapshotDir(root, installKey string) (string, error) {
	base := nowFunc().UTC().Format(timestampFormat) + "-" + sanitizeInstallKey(installKey)
	candidate := filepath.Join(root, base)
	for attempt := 1; attempt <= maxCollisionRetries; attempt++ {
		if err := os.Mkdir(candidate, 0o755); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", errs.WrapIO(err)
		}
		candidate = filepath.Join(root, fmt.Sprintf("%s-%d", base, attempt+1))
	}
	return "", errs.NewKitValidation(fmt.Sprintf(
		"snapshot collision retry exhausted after %d attempts under %s", maxCollisionRetries, root))
}

// copyFileIntoSnapshot copies projectDir/relPath into <snapDir>/files/relPath,
// preserving the project-relative directory structure. Returns absent=true
// if the source file does not exist (and writes nothing under files/).
func copyFileIntoSnapshot(snapDir, projectDir, relPath string) (absent bool, err error) {
	src := filepath.Join(projectDir, relPath)
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, errs.WrapIO(err)
	}
	dst := filepath.Join(snapDir, filesSubdir, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, errs.WrapIO(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return false, errs.WrapIO(err)
	}
	return false, nil
}

// restoreFileFromSnapshot writes <snapDir>/files/<rel> back to
// projectDir/rel, or removes projectDir/rel if the manifest recorded it
// as absent at capture time.
func restoreFileFromSnapshot(snapDir, projectDir string, f manifestFile) error {
	target := filepath.Join(projectDir, f.RelPath)
	if f.Absent {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errs.WrapIO(err)
		}
		return nil
	}
	src := filepath.Join(snapDir, filesSubdir, f.RelPath)
	data, err := os.ReadFile(src)
	if err != nil {
		return errs.WrapIO(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return errs.WrapIO(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

func writeManifest(snapDir string, m *manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return errs.WrapJSON(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, manifestFileName), data, 0o644); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

func readManifest(snapDir string) (*manifest, error) {
	data, err := os.ReadFile(filepath.Join(snapDir, manifestFileName))
	if err != nil {
		return nil, errs.WrapIO(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errs.WrapJSON(err)
	}
	return &m, nil
}

// sanitizeInstallKey makes an install key safe for use as a single
// directory name. The install-key alphabet is [a-z0-9-/] (slash being
// the source/kit separator); replacing "/" with "__" is unambiguous.
func sanitizeInstallKey(key string) string {
	return strings.ReplaceAll(key, "/", "__")
}
