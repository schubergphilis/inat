// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package delivery copies skill directories and rule files into the
// project tree, and manages the inat-controlled section in agent rule
// files (CLAUDE.md and friends).
//
// Mirrors the Rust implementation's `src/core/delivery/`.
package delivery

import (
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/errs"
)

// FileBlob is a (relative-path, content) pair for a file inside a skill
// directory. Mirrors Rust's `(String, Vec<u8>)`.
type FileBlob struct {
	Path    string
	Content []byte
}

// DeliverSkill copies a skill directory into projectDir under
// targetPath/skillName. Returns one DeliveredFile entry per file written,
// each with its content hash captured.
//
// targetPath is expected to end with `/` (e.g. `.claude/skills/`); this
// mirrors Rust's behaviour where the format string `{target_path}{name}`
// concatenates without an extra separator.
func DeliverSkill(projectDir, skillName string, files []FileBlob, targetPath string) ([]config.DeliveredFile, error) {
	skillDir := filepath.Join(projectDir, targetPath, skillName)
	out := make([]config.DeliveredFile, 0, len(files))
	for _, f := range files {
		dest := filepath.Join(skillDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, errs.WrapIO(err)
		}
		if err := os.WriteFile(dest, f.Content, 0o644); err != nil {
			return nil, errs.WrapIO(err)
		}
		out = append(out, config.DeliveredFile{
			Path: targetPath + skillName + "/" + f.Path,
			Hash: hash.HashBytes(f.Content),
		})
	}
	return out, nil
}

// DeliverRuleContextual writes a contextual rule to
// projectDir/targetPath/<ruleName>.md. Returns the resulting DeliveredFile.
func DeliverRuleContextual(projectDir, ruleName string, content []byte, targetPath string) (config.DeliveredFile, error) {
	filename := ruleName + ".md"
	dest := filepath.Join(projectDir, targetPath, filename)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return config.DeliveredFile{}, errs.WrapIO(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return config.DeliveredFile{}, errs.WrapIO(err)
	}
	return config.DeliveredFile{
		Path: targetPath + filename,
		Hash: hash.HashBytes(content),
	}, nil
}
