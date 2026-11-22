// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package errs

import (
	"errors"
	"strings"
	"testing"
)

func TestHomeAsProject(t *testing.T) {
	err := NewHomeAsProject("/home/u")
	if !errors.Is(err, ErrHomeAsProject) {
		t.Fatal("errors.Is(ErrHomeAsProject) = false")
	}
	if msg := err.Error(); !strings.Contains(msg, "/home/u") || !strings.Contains(msg, "--user") {
		t.Errorf("message = %q, want home path and --user hint", msg)
	}
}

func TestConfigInvalid(t *testing.T) {
	err := NewConfigInvalid("/h/.inat/config.json", "kits_repo is not allowed in the user config")
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatal("errors.Is(ErrConfigInvalid) = false")
	}
	if msg := err.Error(); !strings.Contains(msg, "/h/.inat/config.json") || !strings.Contains(msg, "kits_repo") {
		t.Errorf("message = %q", msg)
	}
}
