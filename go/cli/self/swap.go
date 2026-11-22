// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// File ownership: binary-path resolution, target-triple mapping,
// archive extraction, and the rename-self-aside binary swap with
// rollback. See spec 0015 §4.2 for the pipeline shape and §4.6 for the
// swap design.
package self

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/schubergphilis/inat/errs"
)

// triple captures the per-platform release artifact metadata.
type triple struct {
	Target string // Rust-style triple, e.g. "x86_64-unknown-linux-gnu"
	Ext    string // ".tar.gz" or ".zip"
	Bin    string // "inat" or "inat.exe"
}

// ResolveTriple maps GOOS/GOARCH to the release artifact metadata.
// Mirrors install.sh's detect_target table verbatim.
func ResolveTriple(goos, goarch string) (triple, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return triple{"x86_64-unknown-linux-gnu", ".tar.gz", "inat"}, nil
	case "linux/arm64":
		return triple{"aarch64-unknown-linux-gnu", ".tar.gz", "inat"}, nil
	case "windows/amd64":
		return triple{"x86_64-pc-windows-msvc", ".zip", "inat.exe"}, nil
	case "darwin/amd64":
		return triple{"x86_64-apple-darwin", ".tar.gz", "inat"}, nil
	case "darwin/arm64":
		return triple{"aarch64-apple-darwin", ".tar.gz", "inat"}, nil
	}
	return triple{}, errs.NewUnsupportedPlatform(goos + "/" + goarch)
}

// CurrentTriple is ResolveTriple applied to the running binary's
// runtime.GOOS / runtime.GOARCH. Tests use ResolveTriple with explicit
// arguments instead.
func CurrentTriple() (triple, error) {
	return ResolveTriple(runtime.GOOS, runtime.GOARCH)
}

// executableFn is package-private so tests can stage a fake path
// without touching os.Executable's behaviour. Defaults to os.Executable.
var executableFn = os.Executable

// ResolveBinaryPath returns the absolute path of the running inat
// binary, resolving symlinks. Refuses to return when the path is under
// the OS temp dir (heuristic for `go run`'s build cache).
func ResolveBinaryPath() (string, error) {
	raw, err := executableFn()
	if err != nil {
		return "", errs.WrapIO(err)
	}
	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		// EvalSymlinks fails on Windows when the path doesn't exist as
		// a regular file (rare, e.g. during very early boot); fall back
		// to the raw path so the rest of the pipeline can still proceed.
		resolved = raw
	}
	// Resolve the temp dir through EvalSymlinks too so the two paths are
	// comparable on Windows: os.Executable normalises 8.3 short names
	// (e.g. EXAMPL~1) to the long form, while os.TempDir does not. A raw
	// filepath.Rel between the two would walk up to drive root and the
	// "is under temp dir" check would be a false negative.
	tmp := normalisedTempDir()
	if IsRunningViaGoRun(resolved, tmp) {
		return "", errs.NewRunningViaGoRun(resolved)
	}
	return resolved, nil
}

func normalisedTempDir() string {
	t, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return os.TempDir()
	}
	return t
}

// IsRunningViaGoRun reports whether execPath looks like a `go run`
// build-cache binary. Heuristic: the path is rooted under tempDir and
// contains a `go-build` segment. Pure function so tests can drive it
// with synthetic paths.
func IsRunningViaGoRun(execPath, tempDir string) bool {
	if tempDir == "" {
		return false
	}
	rel, err := filepath.Rel(tempDir, execPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	return strings.Contains(filepath.ToSlash(rel), "go-build")
}

// ExtractBinary extracts the inat binary entry from archivePath into
// destPath. ext selects the archive format (".tar.gz" or ".zip").
// On Unix the destination gets mode 0755.
func ExtractBinary(archivePath, ext, binName, destPath string) error {
	switch ext {
	case ".tar.gz":
		return extractTarGz(archivePath, binName, destPath)
	case ".zip":
		return extractZip(archivePath, binName, destPath)
	}
	return errs.NewArchiveLayout(fmt.Sprintf("unsupported archive extension '%s'", ext))
}

func extractTarGz(archivePath, binName, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return errs.WrapIO(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return errs.NewArchiveLayout("gzip reader: " + err.Error())
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	suffix := "/" + binName
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errs.NewArchiveLayout("tar read: " + err.Error())
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !strings.HasSuffix(hdr.Name, suffix) {
			continue
		}
		return writeBinary(destPath, tr)
	}
	return errs.NewArchiveLayout("no '" + binName + "' entry in tar.gz")
}

func extractZip(archivePath, binName, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return errs.NewArchiveLayout("zip open: " + err.Error())
	}
	defer r.Close()
	suffix := "/" + binName
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(entry.Name)
		if !strings.HasSuffix(name, suffix) && name != binName {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return errs.NewArchiveLayout("zip entry open: " + err.Error())
		}
		err = writeBinary(destPath, rc)
		_ = rc.Close()
		return err
	}
	return errs.NewArchiveLayout("no '" + binName + "' entry in zip")
}

// writeBinary copies r into destPath with mode 0755 (chmod is a no-op
// on Windows but harmless).
func writeBinary(destPath string, r io.Reader) error {
	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return errs.WrapIO(err)
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return errs.WrapIO(err)
	}
	if err := out.Close(); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

// SwapBinary atomically replaces target with replacement using the
// rename-self-aside pattern (target → target.old; replacement → target;
// best-effort os.Remove of target.old). Rollback restores target on
// any failure between the two renames.
func SwapBinary(target, replacement string) error {
	old := target + ".old"
	// Step 1: rename running binary aside. Windows allows rename of a
	// locked exe; Unix kernels keep the inode mapped via the exec fd.
	if err := os.Rename(target, old); err != nil {
		return errs.WrapIO(err)
	}
	// Step 2: move the new binary into the original path.
	if err := os.Rename(replacement, target); err != nil {
		rollbackErr := os.Rename(old, target)
		if rollbackErr != nil {
			return errs.NewUnrecoverableSwap(
				fmt.Sprintf("rename '%s' → '%s' failed and rollback rename '%s' → '%s' also failed: %v",
					replacement, target, old, target, rollbackErr),
				err)
		}
		return errs.WrapIO(err)
	}
	// Step 3: best-effort cleanup. Windows refuses while the parent
	// process holds the lock; the next inat invocation cleans it up via
	// the root command's PersistentPreRunE.
	_ = os.Remove(old)
	return nil
}

// CleanupOldBinary removes <runningBinary>.old if present. Silent on
// every failure mode: file not found (no prior update), permission
// error, or still-locked (a prior process holds the lock — fine, the
// next invocation by *that* process will retry). Wired into the cobra
// root's PersistentPreRunE so every inat invocation gets a chance to
// clean up.
func CleanupOldBinary() {
	path, err := ResolveBinaryPath()
	if err != nil {
		return
	}
	_ = os.Remove(path + ".old")
}
