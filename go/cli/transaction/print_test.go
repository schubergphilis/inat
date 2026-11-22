// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"bytes"
	"errors"
	"testing"
)

func TestPrintPlanDetails(t *testing.T) {
	tests := []struct {
		name string
		plan []PlannedAction
		want string
	}{
		{
			name: "empty plan emits nothing",
			plan: nil,
			want: "",
		},
		{
			name: "up-to-date single",
			plan: []PlannedAction{
				{Kind: ActionUpToDate, InstallKey: "company/foo"},
			},
			want: "up-to-date company/foo\n",
		},
		{
			name: "update single",
			plan: []PlannedAction{
				{Kind: ActionUpdate, InstallKey: "company/foo"},
			},
			want: "update company/foo\n",
		},
		{
			name: "rename with from-to arrow",
			plan: []PlannedAction{
				{Kind: ActionRename, InstallKey: "company/foo", NewKey: "company/foo-renamed"},
			},
			want: "rename company/foo → company/foo-renamed\n",
		},
		{
			name: "replace with from-to arrow",
			plan: []PlannedAction{
				{Kind: ActionReplace, InstallKey: "company/foo", NewKey: "other/bar"},
			},
			want: "replace company/foo → other/bar\n",
		},
		{
			name: "needs-attention with error suffix",
			plan: []PlannedAction{
				{Kind: ActionNeedsAttention, InstallKey: "company/foo", Err: errors.New("kit not found")},
			},
			want: "needs-attention company/foo: kit not found\n",
		},
		{
			name: "multi-action preserves plan order",
			plan: []PlannedAction{
				{Kind: ActionUpdate, InstallKey: "a/x"},
				{Kind: ActionUpToDate, InstallKey: "b/y"},
				{Kind: ActionRename, InstallKey: "c/z", NewKey: "c/z-new"},
				{Kind: ActionNeedsAttention, InstallKey: "d/w", Err: errors.New("boom")},
			},
			want: "update a/x\n" +
				"up-to-date b/y\n" +
				"rename c/z → c/z-new\n" +
				"needs-attention d/w: boom\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintPlanDetails(&buf, tt.plan)
			if got := buf.String(); got != tt.want {
				t.Fatalf("PrintPlanDetails:\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestPrintPlanDetailsArrowByteSequence pins the U+2192 (→) encoding to
// its UTF-8 byte sequence so a future refactor that swaps in `->` or
// another character fails loudly. Spec 0020 §4.5 commits to this byte
// sequence for stable greppability.
func TestPrintPlanDetailsArrowByteSequence(t *testing.T) {
	var buf bytes.Buffer
	PrintPlanDetails(&buf, []PlannedAction{
		{Kind: ActionRename, InstallKey: "a", NewKey: "b"},
	})
	want := []byte{
		'r', 'e', 'n', 'a', 'm', 'e', ' ',
		'a', ' ',
		0xE2, 0x86, 0x92, // U+2192 →
		' ', 'b', '\n',
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("byte sequence mismatch:\n got: % x\nwant: % x", buf.Bytes(), want)
	}
}
