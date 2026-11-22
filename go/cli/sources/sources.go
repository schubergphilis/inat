// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package sources implements `inat sources add/remove/list`.
//
// Mirrors the Rust implementation's `src/cli/sources/`.
package sources

import (
	"fmt"
	"os"
	"strings"

	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/naming"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// RunSourcesAdd handles `inat sources add <url> [--name <alias>] [--path <subpath>]`.
func RunSourcesAdd(url, name, path string, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return SourcesAdd(dir, url, name, path)
}

// RunSourcesRemove handles `inat sources remove <name>`.
func RunSourcesRemove(name string, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return SourcesRemove(dir, name)
}

// RunSourcesList handles `inat sources list`.
func RunSourcesList(user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return SourcesList(dir)
}

// SourcesAdd is the dir-explicit form of RunSourcesAdd.
func SourcesAdd(dir, url, name, path string) error {
	sourceName, err := resolveSourceName(url, name)
	if err != nil {
		return err
	}
	normalizedPath, err := NormalizeSourcePath(path)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	if err := validateSourceUnique(&cfg, sourceName); err != nil {
		return err
	}
	repo, err := source.FetchSource(url, sourceName)
	if err != nil {
		return err
	}
	reg, err := source.FetchRegistryAt(repo, normalizedPath)
	if err != nil {
		return err
	}
	if err := registry.ValidateRegistry(&reg); err != nil {
		return err
	}
	src := config.Source{Name: sourceName, URL: url}
	if normalizedPath != "" {
		p := normalizedPath
		src.Path = &p
	}
	cfg.Sources = append(cfg.Sources, src)
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "added source '%s' — %d kit(s) available\n", sourceName, len(reg.Kits))
	return nil
}

// SourcesRemove is the dir-explicit form of RunSourcesRemove.
func SourcesRemove(dir, name string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range cfg.Sources {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return errs.NewSourceNotFound(name)
	}
	prefix := name + "/"
	for k := range cfg.Installed {
		if strings.HasPrefix(k, prefix) {
			return errs.NewSourceInUse(name)
		}
	}
	cfg.Sources = append(cfg.Sources[:idx], cfg.Sources[idx+1:]...)
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "removed source '%s'\n", name)
	return nil
}

// SourcesList is the dir-explicit form of RunSourcesList.
func SourcesList(dir string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	if len(cfg.Sources) == 0 {
		fmt.Fprintln(os.Stderr, "no sources configured")
		return nil
	}
	for _, s := range cfg.Sources {
		fmt.Printf("%s\t%s\n", s.Name, s.URL)
	}
	return nil
}

// resolveSourceName picks the explicit name when provided, otherwise
// derives one from the URL. Validates the result against naming rules.
func resolveSourceName(url, name string) (string, error) {
	resolved := name
	if resolved == "" {
		derived, ok := deriveSourceName(url)
		if !ok {
			return "", errs.NewInvalidName(url, "cannot derive source name from URL; use --name")
		}
		resolved = derived
	}
	if err := naming.ValidateName(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func deriveSourceName(url string) (string, bool) {
	trimmed := strings.TrimRight(url, "/")
	segment := trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		segment = trimmed[i+1:]
	} else if i := strings.LastIndex(trimmed, ":"); i >= 0 {
		segment = trimmed[i+1:]
	}
	name := strings.TrimSuffix(segment, ".git")
	if name == "" {
		return "", false
	}
	return name, true
}

func validateSourceUnique(cfg *config.Config, name string) error {
	for _, s := range cfg.Sources {
		if s.Name == name {
			return errs.NewSourceAlreadyExists(name)
		}
	}
	return nil
}
