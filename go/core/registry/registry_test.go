// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package registry

import (
	"errors"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

const validRegistryJSON = `{
    "version": "1",
    "kits": {
        "coding-standards": {
            "description": "Common coding rules",
            "types": ["skill", "rule"]
        },
        "security": {
            "description": "Security rules",
            "types": ["rule"]
        }
    }
}`

func TestParseValidRegistry(t *testing.T) {
	r, err := ParseRegistry(validRegistryJSON)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	if r.Version != "1" {
		t.Errorf("Version = %q, want %q", r.Version, "1")
	}
	if len(r.Kits) != 2 {
		t.Errorf("len(Kits) = %d, want 2", len(r.Kits))
	}
	if _, ok := r.Kits["coding-standards"]; !ok {
		t.Errorf("missing coding-standards entry")
	}
}

func TestValidateValidRegistry(t *testing.T) {
	r, err := ParseRegistry(validRegistryJSON)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	if err := ValidateRegistry(&r); err != nil {
		t.Errorf("ValidateRegistry on valid input: %v", err)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := ParseRegistry("not json")
	if err == nil {
		t.Fatalf("expected error on invalid JSON")
	}
	if !errors.Is(err, errs.ErrJSON) {
		t.Errorf("error kind = %v, want JSON", err)
	}
}

func TestParseMissingVersion(t *testing.T) {
	_, err := ParseRegistry(`{ "kits": {} }`)
	if err == nil {
		t.Fatalf("expected error on missing version")
	}
}

func TestValidateUnsupportedVersion(t *testing.T) {
	r, err := ParseRegistry(`{
        "version": "99",
        "kits": { "test": { "description": "test", "types": ["skill"] } }
    }`)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	verr := ValidateRegistry(&r)
	if verr == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(verr.Error(), "unsupported registry version") {
		t.Errorf("err %q missing 'unsupported registry version'", verr)
	}
}

func TestValidateEmptyKits(t *testing.T) {
	r, err := ParseRegistry(`{ "version": "1", "kits": {} }`)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	verr := ValidateRegistry(&r)
	if verr == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(verr.Error(), "no kits") {
		t.Errorf("err %q missing 'no kits'", verr)
	}
}

func TestValidateInvalidKitName(t *testing.T) {
	r, err := ParseRegistry(`{
        "version": "1",
        "kits": { "Invalid-Name": { "description": "bad", "types": ["skill"] } }
    }`)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	verr := ValidateRegistry(&r)
	if verr == nil {
		t.Fatalf("expected validation error")
	}
	if !errors.Is(verr, errs.ErrInvalidName) {
		t.Errorf("err kind = %v, want InvalidName", verr)
	}
}

func TestValidateUnknownType(t *testing.T) {
	r, err := ParseRegistry(`{
        "version": "1",
        "kits": { "test": { "description": "test", "types": ["unknown"] } }
    }`)
	if err != nil {
		t.Fatalf("ParseRegistry: %v", err)
	}
	verr := ValidateRegistry(&r)
	if verr == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(verr.Error(), "unknown type") {
		t.Errorf("err %q missing 'unknown type'", verr)
	}
}
