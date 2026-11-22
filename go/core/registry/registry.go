// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package registry parses and validates source `registry.json` files.
//
// Mirrors the Rust implementation's `src/core/registry.rs`. JSON schema is identical so configs
// written by the Rust binary parse cleanly here and vice versa.
package registry

import (
	"encoding/json"

	"github.com/schubergphilis/inat/core/naming"
	"github.com/schubergphilis/inat/errs"
)

// Registry is the parsed form of `registry.json` at a source repo's root
// (or under its configured subpath).
type Registry struct {
	Version string                   `json:"version"`
	Kits    map[string]RegistryEntry `json:"kits"`
}

// RegistryEntry describes a single kit listed in the registry.
type RegistryEntry struct {
	Description string   `json:"description"`
	Types       []string `json:"types"`
}

// ParseRegistry deserialises a registry.json string into a Registry.
//
// Returns *errs.Error with Kind=JSON on malformed JSON or missing required
// fields (`version`, `kits`). Mirrors Rust serde's strict required-field
// behaviour.
func ParseRegistry(content string) (Registry, error) {
	if err := requireFields(content, "version", "kits"); err != nil {
		return Registry{}, err
	}
	var r Registry
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		return Registry{}, errs.WrapJSON(err)
	}
	return r, nil
}

func requireFields(content string, fields ...string) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return errs.WrapJSON(err)
	}
	for _, f := range fields {
		if _, ok := probe[f]; !ok {
			return errs.WrapJSON(&missingFieldError{field: f})
		}
	}
	return nil
}

// ValidateRegistry checks structural rules:
//   - Version must equal "1".
//   - Kits map must be non-empty.
//   - Every kit name passes core/naming.ValidateName.
//   - Every entry's types[] entry is "skill" or "rule".
//
// Returns *errs.Error (Kind=RegistryValidation or InvalidName) on failure.
func ValidateRegistry(r *Registry) error {
	if r.Version != "1" {
		return errs.NewRegistryValidation("unsupported registry version '" + r.Version + "'")
	}
	if len(r.Kits) == 0 {
		return errs.NewRegistryValidation("registry contains no kits")
	}
	for name := range r.Kits {
		if err := naming.ValidateName(name); err != nil {
			return err
		}
	}
	for name, entry := range r.Kits {
		for _, t := range entry.Types {
			if t != "skill" && t != "rule" {
				return errs.NewRegistryValidation("kit '" + name + "' has unknown type '" + t + "'")
			}
		}
	}
	return nil
}

type missingFieldError struct{ field string }

func (e *missingFieldError) Error() string {
	return "missing required field '" + e.field + "'"
}
