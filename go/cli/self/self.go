// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// File ownership: cobra wiring for `inat self update` and the
// orchestrating RunSelfUpdate function.
package self

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/schubergphilis/inat/errs"
)

// Options is the parsed CLI invocation. Constructed by the cobra
// command, passed to RunSelfUpdate. Tests construct it directly.
type Options struct {
	Version   string // explicit --version pin; bare or v-prefixed
	Check     bool   // --check: print status, do not update
	Force     bool   // --force: re-run download+swap even when current = latest
	Endpoints Endpoints
	GOOS      string // testing knob; defaults to runtime.GOOS
	GOARCH    string // testing knob; defaults to runtime.GOARCH
	Current   string // running binary version; defaults to inputs from main
}

// NewSelfCmd returns the `self` parent command. Currently has one
// child (`update`); future siblings (`info`, `uninstall`) plug in here.
func NewSelfCmd(currentVersion string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the inat binary itself.",
	}
	cmd.AddCommand(newSelfUpdateCmd(currentVersion))
	return cmd
}

func newSelfUpdateCmd(currentVersion string) *cobra.Command {
	var version string
	var check, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Upgrade the running inat binary to the latest release.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunSelfUpdate(Options{
				Version:   version,
				Check:     check,
				Force:     force,
				Endpoints: DefaultEndpoints(currentVersion),
				Current:   currentVersion,
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Install a specific tag (e.g. v0.5.0) instead of latest.")
	cmd.Flags().BoolVar(&check, "check", false, "Print current/latest and exit without changes.")
	cmd.Flags().BoolVar(&force, "force", false, "Re-download and swap even if current = latest.")
	return cmd
}

// RunSelfUpdate is the testable entry point. Mirrors the pipeline laid
// out in spec 0015 §4.2.
func RunSelfUpdate(opts Options) error {
	binaryPath, err := ResolveBinaryPath()
	if err != nil {
		return err
	}
	goos, goarch := opts.GOOS, opts.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	tri, err := ResolveTriple(goos, goarch)
	if err != nil {
		return err
	}
	client := NewHTTPClient()
	target, err := resolveTargetVersion(client, opts)
	if err != nil {
		return err
	}
	if reportCheckOrShortCircuit(opts, target) {
		return nil
	}
	return downloadAndSwap(client, opts, tri, target, binaryPath)
}

// resolveTargetVersion returns the bare version the run is targeting:
// the explicit --version (normalised) or the latest stable tag.
func resolveTargetVersion(client *http.Client, opts Options) (string, error) {
	if opts.Version != "" {
		return strings.TrimPrefix(opts.Version, "v"), nil
	}
	return LatestVersion(client, opts.Endpoints)
}

// reportCheckOrShortCircuit handles the --check and "already on latest"
// printing; returns true when the caller should exit early (no swap).
func reportCheckOrShortCircuit(opts Options, target string) bool {
	current := strings.TrimPrefix(opts.Current, "v")
	upToDate := current == target
	if opts.Check {
		if upToDate {
			fmt.Fprintf(os.Stderr, "current %s, already on latest\n", current)
		} else {
			fmt.Fprintf(os.Stderr,
				"current %s, latest %s, update available — run `inat self update` to install\n",
				current, target)
		}
		return true
	}
	if upToDate && !opts.Force {
		fmt.Fprintf(os.Stderr, "already on %s\n", current)
		return true
	}
	return false
}

// downloadAndSwap runs steps 7-12 of the pipeline: stage SHA256SUMS +
// archive into the binary's dir, verify, extract, swap, print success.
func downloadAndSwap(client *http.Client, opts Options, tri triple, target, binaryPath string) error {
	stageDir := filepath.Dir(binaryPath)
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	stagedSums := filepath.Join(stageDir, ".inat-update-"+suffix+".sums")
	stagedArchive := filepath.Join(stageDir, ".inat-update-"+suffix+tri.Ext)
	stagedBinary := filepath.Join(stageDir, ".inat-update-"+suffix+".new")
	defer cleanupTemps(stagedSums, stagedArchive, stagedBinary)

	if err := fetchSums(client, opts, target, stagedSums); err != nil {
		return err
	}
	archiveURL := ArchiveURL(opts.Endpoints, target, tri.Target, tri.Ext)
	archiveName := fmt.Sprintf("inat-%s-%s%s", target, tri.Target, tri.Ext)
	fmt.Fprintf(os.Stderr, "==> downloading %s\n", archiveName)
	if err := DownloadFile(client, opts.Endpoints, archiveURL, stagedArchive); err != nil {
		if IsNotFound(err) {
			return errs.NewVersionNotPublished(target, tri.Target)
		}
		return err
	}
	if err := VerifySHA256(stagedArchive, stagedSums, archiveName, target, tri.Target); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "==> checksum OK")
	if err := ExtractBinary(stagedArchive, tri.Ext, tri.Bin, stagedBinary); err != nil {
		return err
	}
	if err := SwapBinary(binaryPath, stagedBinary); err != nil {
		return err
	}
	current := strings.TrimPrefix(opts.Current, "v")
	fmt.Fprintf(os.Stderr, "==> updated inat %s → %s at %s\n", current, target, binaryPath)
	return nil
}

func fetchSums(client *http.Client, opts Options, target, dest string) error {
	url := SumsURL(opts.Endpoints, target)
	if err := DownloadFile(client, opts.Endpoints, url, dest); err != nil {
		if IsNotFound(err) {
			return errs.NewVersionNotPublished(target, "(any)")
		}
		return err
	}
	return nil
}

func cleanupTemps(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// randomSuffix returns a short hex string for tagging temp files.
// crypto/rand keeps it collision-free across concurrent inat
// processes (rare but possible: a wrapper script + manual run).
func randomSuffix() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errs.WrapIO(err)
	}
	return hex.EncodeToString(b[:]), nil
}
