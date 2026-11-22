// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

// LoadUserConfig reads the user-scope config `<home>/.inat/config.json`
// (spec 0025 §3). A missing file is an empty config (there is no
// `init --user`); a spec-0006 agents-only file loads with no sources or
// kits. `kits_repo` is rejected: kit authoring belongs to a repository.
func LoadUserConfig(home string) (Config, error) {
	path := ConfigPath(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = []byte("{}"), nil
	}
	if err != nil {
		return Config{}, errs.WrapIO(err)
	}
	cfg, err := parseConfig(data)
	if err != nil {
		return Config{}, err
	}
	if cfg.KitsRepo {
		return Config{}, errs.NewConfigInvalid(path, "kits_repo is not allowed in the user config")
	}
	cfg.Scope = scope.User
	return cfg, nil
}

// IsUserRoot reports whether dir is the home directory, i.e. the root of
// the user scope. An unresolvable home or unstattable dir is "no".
func IsUserRoot(dir string) bool {
	home, err := HomeDir()
	if err != nil {
		return false
	}
	a, errA := os.Stat(dir)
	b, errB := os.Stat(home)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(a, b)
}

// GlobalConfig is the spec 0006 view of `~/.inat/config.json`: just the
// global agents list. Read-only in production; since spec 0025 the file
// holds the full user-scope Config, so writes go through SaveConfig(home).
type GlobalConfig struct {
	Agents []string `json:"agents"`
}

// LoadGlobalConfig reads `~/.inat/config.json`. Returns an empty
// GlobalConfig if the file does not exist (since global config is
// optional).
func LoadGlobalConfig() (GlobalConfig, error) {
	path, err := globalConfigPath()
	if err != nil {
		return GlobalConfig{}, err
	}
	return LoadGlobalConfigFrom(path)
}

// LoadGlobalConfigFrom reads a global config from an explicit path. Used
// by tests with HOME-overrides.
func LoadGlobalConfigFrom(path string) (GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GlobalConfig{Agents: []string{}}, nil
		}
		return GlobalConfig{}, errs.WrapIO(err)
	}
	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, errs.WrapJSON(err)
	}
	if cfg.Agents == nil {
		cfg.Agents = []string{}
	}
	return cfg, nil
}

// SaveGlobalConfig serialises cfg to `~/.inat/config.json`, creating
// `~/.inat/` if needed. It overwrites the whole file and so drops any
// user-scope sources and kits; production code uses SaveConfig(home).
func SaveGlobalConfig(cfg *GlobalConfig) error {
	path, err := globalConfigPath()
	if err != nil {
		return err
	}
	return SaveGlobalConfigTo(path, cfg)
}

// SaveGlobalConfigTo serialises cfg to an explicit path. Used by tests
// with HOME-overrides.
func SaveGlobalConfigTo(path string, cfg *GlobalConfig) error {
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

// CollectActiveAgents returns the deduplicated union of project agents
// (cfg.Agents) and global agents (`~/.inat/config.json#agents`).
//
// `homeOverride` lets tests redirect global lookup; pass empty string to
// use the real home.
func CollectActiveAgents(cfg *Config, homeOverride string) ([]string, error) {
	var global GlobalConfig
	var err error
	if homeOverride != "" {
		path := filepath.Join(homeOverride, configDir, configFile)
		global, err = LoadGlobalConfigFrom(path)
	} else {
		global, err = LoadGlobalConfig()
	}
	if err != nil {
		return nil, err
	}
	out := append([]string{}, cfg.Agents...)
	for _, g := range global.Agents {
		if !contains(out, g) {
			out = append(out, g)
		}
	}
	return out, nil
}

// HomeDir resolves the user's home directory via os.UserHomeDir: $HOME
// off Windows, %USERPROFILE% on Windows. That is the folder Claude Code
// treats as `~` for `~/.claude/`, so user-scope delivery (spec 0025 §6.5)
// and the global cache land in the same place it reads from.
func HomeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", errs.WrapIO(err)
	}
	return h, nil
}

func globalConfigPath() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir, configFile), nil
}
