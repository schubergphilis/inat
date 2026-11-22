// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package install

import (
	"fmt"
	"os"
	"strings"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/delivery"
	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// DeliverToAgent copies every entry of kitCfg into the agent's targets.
// Returns the resulting DeliveredFile records (only copy-strategy files;
// always-priority rules go into the managed section and are not tracked
// as delivered files in the config).
//
// skip holds project-relative delivered paths (as recorded in
// DeliveredFile.Path) that must not be written: refresh passes the
// locally-modified files so it keeps them (spec 0001 §Rules item 9).
// Skipped files are left out of the result. Managed-section blocks and
// references are still delivered. Pass nil to write everything.
func DeliverToAgent(
	dir string,
	mapping *agents.ProfileMapping,
	kitCfg *kit.KitConfig,
	allFiles []source.FileBlob,
	sourceName, kitName string,
	skip map[string]bool,
) ([]config.DeliveredFile, error) {
	var delivered []config.DeliveredFile
	for _, entry := range kitCfg.Files {
		var files []config.DeliveredFile
		var err error
		switch entry.FileType {
		case kit.Skill:
			files, err = deliverSkillEntry(dir, mapping, entry, allFiles, skip)
		case kit.Rule:
			files, err = deliverRuleEntry(dir, mapping, entry, allFiles, sourceName, kitName, skip)
		}
		if err != nil {
			return nil, err
		}
		delivered = append(delivered, files...)
	}
	return delivered, nil
}

func deliverSkillEntry(
	dir string,
	mapping *agents.ProfileMapping,
	entry kit.FileEntry,
	allFiles []source.FileBlob,
	skip map[string]bool,
) ([]config.DeliveredFile, error) {
	entryName := kit.DeriveEntryName(entry.Path, entry.FileType)
	var skillFiles []delivery.FileBlob
	for _, f := range extractSkillFiles(allFiles, entry.Path) {
		if !skip[mapping.Skill.Path+entryName+"/"+f.Path] {
			skillFiles = append(skillFiles, f)
		}
	}
	return delivery.DeliverSkill(dir, entryName, skillFiles, mapping.Skill.Path)
}

func deliverRuleEntry(
	dir string,
	mapping *agents.ProfileMapping,
	entry kit.FileEntry,
	allFiles []source.FileBlob,
	sourceName, kitName string,
	skip map[string]bool,
) ([]config.DeliveredFile, error) {
	content, err := findFileContent(allFiles, entry.Path)
	if err != nil {
		return nil, err
	}
	priority := kit.Contextual
	if entry.Priority != nil {
		priority = *entry.Priority
	}
	switch priority {
	case kit.Always:
		return nil, deliverAlwaysRule(dir, mapping, content, entry.Path, sourceName, kitName)
	case kit.Contextual:
		entryName := kit.DeriveEntryName(entry.Path, entry.FileType)
		return deliverContextualRule(dir, mapping, entryName, content, skip)
	}
	return nil, nil
}

func deliverAlwaysRule(
	dir string,
	mapping *agents.ProfileMapping,
	content []byte,
	entryPath, sourceName, kitName string,
) error {
	filename := entryPath
	if i := strings.LastIndex(entryPath, "/"); i >= 0 {
		filename = entryPath[i+1:]
	}
	header := sourceName + "/" + kitName + "/" + filename
	body, err := stripFrontmatter(content)
	if err != nil {
		return err
	}
	target := dir + string(os.PathSeparator) + mapping.Rule.Always.Path
	return delivery.AddAlwaysBlock(target, header, body)
}

func deliverContextualRule(
	dir string,
	mapping *agents.ProfileMapping,
	entryName string,
	content []byte,
	skip map[string]bool,
) ([]config.DeliveredFile, error) {
	ctx := mapping.Rule.Contextual
	var out []config.DeliveredFile
	if !skip[ctx.RuleFile(entryName)] {
		file, err := delivery.DeliverRuleContextual(dir, entryName, content, mapping.Rule.Contextual.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	description, err := descriptionFromContent(content)
	if err != nil {
		return nil, err
	}
	target := dir + string(os.PathSeparator) + mapping.Rule.Contextual.Reference
	if err := delivery.AddContextualReference(target, ctx.RefText(entryName), description); err != nil {
		return nil, err
	}
	return out, nil
}

// descriptionFromContent extracts the rule's frontmatter `description`
// for embedding in the managed-section reference bullet. Frontmatter
// shape has already been validated by ReadAndValidateKit; here we only
// surface an empty string when no description is present.
func descriptionFromContent(content []byte) (string, error) {
	fm, _, _, err := frontmatter.Parse(string(content))
	if err != nil {
		return "", err
	}
	return fm.Description, nil
}

// stripFrontmatter returns the rule body with any leading YAML
// frontmatter block removed. Used when inlining always-priority rule
// content into the managed section: the agent reads the body directly,
// so the metadata block would only be noise.
func stripFrontmatter(content []byte) (string, error) {
	_, body, _, err := frontmatter.Parse(string(content))
	if err != nil {
		return "", err
	}
	return strings.TrimLeft(body, "\r\n"), nil
}

func extractSkillFiles(allFiles []source.FileBlob, entryPath string) []delivery.FileBlob {
	prefix := strings.TrimRight(entryPath, "/") + "/"
	var out []delivery.FileBlob
	for _, f := range allFiles {
		if strings.HasPrefix(f.Path, prefix) {
			out = append(out, delivery.FileBlob{
				Path:    f.Path[len(prefix):],
				Content: f.Content,
			})
		}
	}
	return out
}

func findFileContent(allFiles []source.FileBlob, path string) ([]byte, error) {
	for _, f := range allFiles {
		if f.Path == path {
			return f.Content, nil
		}
	}
	return nil, errs.NewKitValidation(
		fmt.Sprintf("file '%s' declared in kit.json not found in tree", path))
}
