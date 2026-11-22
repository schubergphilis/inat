// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package config reads, writes, and validates `.inat/config.json` and
// `~/.inat/config.json`.
//
// Mirrors the Rust implementation's `src/core/config.rs`. JSON shape MUST round-trip with the
// Rust binary's output: same field names, same field order, same
// `skip_serializing_if` semantics for `Source.path` and `kits_repo`.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

const (
	configDir  = ".inat"
	configFile = "config.json"
)

// Config is the parsed `.inat/config.json` for a project.
//
// Field order matches the historical Rust `Config` struct (sources,
// installed, agents, kits_repo). New fields are appended with omitempty so
// configs written by older binaries round-trip without diff.
type Config struct {
	Sources          []Source                   `json:"sources"`
	Installed        map[string]InstalledKit    `json:"installed"`
	Agents           []string                   `json:"agents"`
	KitsRepo         bool                       `json:"kits_repo,omitempty"`
	BuiltinDelivered map[string][]DeliveredFile `json:"builtinDelivered,omitempty"`

	// Scope is set at load time, never serialised: User when the config
	// was loaded from the home directory (spec 0025).
	Scope scope.Scope `json:"-"`
}

// Source is a registered remote kit source.
type Source struct {
	Name string  `json:"name"`
	URL  string  `json:"url"`
	Path *string `json:"path,omitempty"`
}

// InstalledKit records a kit that has been installed in this project.
type InstalledKit struct {
	Tree      string          `json:"tree"`
	Names     []string        `json:"names,omitempty"`
	Delivered []DeliveredFile `json:"delivered"`
}

// DeliveredFile is one file delivered as part of a kit install, with its
// SHA-256 hash captured at delivery time for modified-file detection.
type DeliveredFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// ConfigPath returns the path to `<dir>/.inat/config.json`.
func ConfigPath(dir string) string {
	return filepath.Join(dir, configDir, configFile)
}

// LoadConfig reads and parses `<dir>/.inat/config.json`. When dir is
// the home directory the user-scope config is loaded instead (see
// LoadUserConfig).
//
// Returns *errs.Error with Kind=ConfigNotFound if the file does not exist,
// Kind=JSON on parse failure, Kind=IO on read failure.
func LoadConfig(dir string) (Config, error) {
	if IsUserRoot(dir) {
		return LoadUserConfig(dir)
	}
	path := ConfigPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, errs.NewConfigNotFound(path)
		}
		return Config{}, errs.WrapIO(err)
	}
	return parseConfig(data)
}

// parseConfig decodes config JSON and normalises absent fields to empty.
func parseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, errs.WrapJSON(err)
	}
	// Ensure the maps/slices are non-nil for ergonomic use, mirroring
	// `#[serde(default)]` on each field.
	if cfg.Sources == nil {
		cfg.Sources = []Source{}
	}
	if cfg.Installed == nil {
		cfg.Installed = map[string]InstalledKit{}
	}
	if cfg.Agents == nil {
		cfg.Agents = []string{}
	}
	if cfg.BuiltinDelivered == nil {
		cfg.BuiltinDelivered = map[string][]DeliveredFile{}
	}
	return cfg, nil
}

// SaveConfig serialises cfg to `<dir>/.inat/config.json`, creating the
// `.inat/` directory if needed. Output format matches Rust's
// `serde_json::to_string_pretty`: 2-space indent, struct-order fields.
func SaveConfig(dir string, cfg *Config) error {
	path := ConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errs.WrapIO(err)
	}
	data, err := marshalPretty(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

// EnsureConfig creates `<dir>/.inat/config.json` with empty defaults if
// it does not already exist. Returns ConfigAlreadyExists if it does.
func EnsureConfig(dir string) error {
	path := ConfigPath(dir)
	if _, err := os.Stat(path); err == nil {
		return errs.NewConfigAlreadyExists(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return errs.WrapIO(err)
	}
	cfg := Config{
		Sources:   []Source{},
		Installed: map[string]InstalledKit{},
		Agents:    []string{},
	}
	return SaveConfig(dir, &cfg)
}

// ReservedNames returns the inat-reserved names that user kits may not
// ship. Mirror of Rust's `RESERVED_NAMES` constant.
func ReservedNames() []string {
	return []string{reserved.BuiltinRuleName, reserved.BuiltinKitsRuleName}
}

// CheckNameUniqueness verifies none of newNames is reserved by inat or
// already delivered by another kit (anything other than installKey).
//
// Mirrors `check_name_uniqueness` in Rust. Returns ReservedName or
// DuplicateName.
func CheckNameUniqueness(cfg *Config, newNames []string, installKey string) error {
	reserved := ReservedNames()
	for _, n := range newNames {
		for _, r := range reserved {
			if n == r {
				return errs.NewReservedName(n)
			}
		}
	}
	for key, ik := range cfg.Installed {
		if key == installKey {
			continue
		}
		for _, n := range newNames {
			for _, existing := range ik.Names {
				if existing == n {
					return errs.NewDuplicateName(n, key)
				}
			}
		}
	}
	return nil
}

// marshalPretty is `json.MarshalIndent(v, "", "  ")` wrapped to return
// inat-flavoured errors. Output matches Rust's `serde_json::to_string_pretty`.
func marshalPretty(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, errs.WrapJSON(err)
	}
	return data, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
