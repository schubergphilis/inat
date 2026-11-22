// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package frontmatter

import (
	"errors"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

func TestParse_NoFrontmatter(t *testing.T) {
	in := "# heading\n\nbody text\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if found {
		t.Errorf("found = true, want false")
	}
	if body != in {
		t.Errorf("body = %q, want %q", body, in)
	}
	if fm.Name != "" || fm.Description != "" {
		t.Errorf("fm = %#v, want zero", fm)
	}
}

func TestParse_Empty(t *testing.T) {
	fm, body, found, err := Parse("")
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if found {
		t.Errorf("found = true, want false")
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
	if fm != (Frontmatter{}) {
		t.Errorf("fm = %#v, want zero", fm)
	}
}

func TestParse_Complete(t *testing.T) {
	in := "---\nname: foo\ndescription: read when bar\n---\nbody line\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found {
		t.Errorf("found = false, want true")
	}
	if fm.Name != "foo" {
		t.Errorf("Name = %q, want foo", fm.Name)
	}
	if fm.Description != "read when bar" {
		t.Errorf("Description = %q, want %q", fm.Description, "read when bar")
	}
	if body != "body line\n" {
		t.Errorf("body = %q, want %q", body, "body line\n")
	}
}

func TestParse_OnlyName(t *testing.T) {
	in := "---\nname: foo\n---\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found || fm.Name != "foo" || fm.Description != "" || body != "" {
		t.Errorf("got fm=%#v body=%q found=%v", fm, body, found)
	}
}

func TestParse_OnlyDescription(t *testing.T) {
	in := "---\ndescription: x\n---\n"
	fm, _, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found || fm.Name != "" || fm.Description != "x" {
		t.Errorf("got fm=%#v found=%v", fm, found)
	}
}

func TestParse_Comments(t *testing.T) {
	in := "---\n# leading comment\nname: foo\n# trailing comment\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Name != "foo" {
		t.Errorf("Name = %q, want foo", fm.Name)
	}
}

func TestParse_BlankLines(t *testing.T) {
	in := "---\n\nname: foo\n\ndescription: x\n\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Name != "foo" || fm.Description != "x" {
		t.Errorf("got %#v", fm)
	}
}

func TestParse_QuotedDouble(t *testing.T) {
	in := "---\ndescription: \"hello world\"\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Description != "hello world" {
		t.Errorf("Description = %q", fm.Description)
	}
}

func TestParse_QuotedSingle(t *testing.T) {
	in := "---\ndescription: 'hello world'\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Description != "hello world" {
		t.Errorf("Description = %q", fm.Description)
	}
}

func TestParse_ColonInValue(t *testing.T) {
	in := "---\ndescription: a: b: c\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Description != "a: b: c" {
		t.Errorf("Description = %q", fm.Description)
	}
}

func TestParse_EmDashInValue(t *testing.T) {
	in := "---\ndescription: read when foo — bar happens\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Description != "read when foo — bar happens" {
		t.Errorf("Description = %q", fm.Description)
	}
}

func TestParse_NoClosingFence(t *testing.T) {
	in := "---\nname: foo\nbody continues\n"
	_, _, _, err := Parse(in)
	if err == nil {
		t.Fatalf("expected error for missing closing fence")
	}
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Errorf("err kind = %v, want KitValidation", err)
	}
}

func TestParse_MalformedLine(t *testing.T) {
	in := "---\nname foo\n---\n"
	_, _, _, err := Parse(in)
	if err == nil {
		t.Fatalf("expected error for malformed line")
	}
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Errorf("err kind = %v, want KitValidation", err)
	}
}

func TestParse_DuplicateKey(t *testing.T) {
	in := "---\nname: a\nname: b\n---\n"
	_, _, _, err := Parse(in)
	if err == nil {
		t.Fatalf("expected error for duplicate key")
	}
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Errorf("err kind = %v, want KitValidation", err)
	}
}

func TestParse_UnknownKeyTolerated(t *testing.T) {
	in := "---\nname: foo\ntags: a, b, c\n---\n"
	fm, _, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if fm.Name != "foo" {
		t.Errorf("Name = %q", fm.Name)
	}
}

func TestParse_CRLF(t *testing.T) {
	in := "---\r\nname: foo\r\ndescription: x\r\n---\r\nbody\r\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found || fm.Name != "foo" || fm.Description != "x" {
		t.Errorf("fm=%#v found=%v", fm, found)
	}
	if body != "body\r\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParse_BodyWithLeadingBlankLine(t *testing.T) {
	in := "---\nname: foo\n---\n\n# heading\nrest\n"
	_, body, _, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	want := "\n# heading\nrest\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestParse_LeadingNonFenceLine(t *testing.T) {
	in := "# heading\n---\nname: foo\n---\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if found {
		t.Errorf("found = true, want false (fm must be at top)")
	}
	if body != in {
		t.Errorf("body changed unexpectedly")
	}
	if fm != (Frontmatter{}) {
		t.Errorf("fm = %#v, want zero", fm)
	}
}

func TestParse_BOM(t *testing.T) {
	bom := string([]byte{0xEF, 0xBB, 0xBF})
	in := bom + "---\nname: foo\n---\n"
	fm, _, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found || fm.Name != "foo" {
		t.Errorf("fm=%#v found=%v", fm, found)
	}
}

func TestParse_FenceWithTrailingSpaces(t *testing.T) {
	in := "---  \nname: foo\n---\t\n"
	fm, _, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found || fm.Name != "foo" {
		t.Errorf("fm=%#v found=%v", fm, found)
	}
}

func TestParse_EmptyBlock(t *testing.T) {
	in := "---\n---\nbody\n"
	fm, body, found, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse returned err: %v", err)
	}
	if !found {
		t.Errorf("found = false, want true")
	}
	if fm != (Frontmatter{}) {
		t.Errorf("fm = %#v, want zero", fm)
	}
	if body != "body\n" {
		t.Errorf("body = %q", body)
	}
}
