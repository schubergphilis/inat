// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package naming

import (
	"errors"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

// TestValidateName covers all 14 cases from the Rust implementation's `src/core/naming.rs::tests`,
// table-driven for clarity.
func TestValidateName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
		// substring asserted in the error reason; empty means no check
		wantSub string
	}{
		// valid cases
		{"valid_simple_name", "my-kit", false, ""},
		{"valid_numeric_name", "abc123", false, ""},
		{"valid_single_char", "a", false, ""},
		{"valid_single_digit", "0", false, ""},
		{"valid_max_length", strings.Repeat("a", 64), false, ""},

		// invalid cases (substring matches Rust's error reasons)
		{"reject_empty", "", true, "must not be empty"},
		{"reject_too_long", strings.Repeat("a", 65), true, "at most 64 characters"},
		{"reject_consecutive_hyphens", "my--kit", true, "consecutive hyphens"},
		{"reject_leading_hyphen", "-leading", true, "start or end with a hyphen"},
		{"reject_trailing_hyphen", "trailing-", true, "start or end with a hyphen"},
		{"reject_uppercase", "MyKit", true, "lowercase alphanumeric"},
		{"reject_special_chars", "my_kit", true, "lowercase alphanumeric"},
		{"reject_spaces", "my kit", true, "lowercase alphanumeric"},
		{"reject_dots", "my.kit", true, "lowercase alphanumeric"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateName(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidateName(%q) returned nil, want error", c.input)
				}
				if !errors.Is(err, errs.ErrInvalidName) {
					t.Errorf("ValidateName(%q) error kind = %v, want InvalidName", c.input, err)
				}
				if c.wantSub != "" && !strings.Contains(err.Error(), c.wantSub) {
					t.Errorf("ValidateName(%q) error %q missing substring %q", c.input, err, c.wantSub)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateName(%q) returned error %v, want nil", c.input, err)
				}
			}
		})
	}
}
