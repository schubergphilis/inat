// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package main

import (
	"strings"
	"testing"

	"github.com/schubergphilis/inat/internal/testfixture"
)

// The real command tree: building it must not panic, and the --user
// gates (spec 0025 §4) hold end to end through cobra.
func TestRootUserFlagGates(t *testing.T) {
	testfixture.SetHome(t, t.TempDir())
	cases := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"--user", "init"}, "--user is not supported by 'inat init'"},
		{[]string{"validate", "--user"}, "--user is not supported by 'inat validate'"},
		{[]string{"self", "update", "--user"}, "--user is not supported by 'inat self'"},
		{[]string{"agents", "add", "claude-code", "--global", "--user"}, "--global and --user"},
		{[]string{"agents", "remove", "claude-code", "--user", "--global"}, "--global and --user"},
	}
	for _, c := range cases {
		root := newRootCmd()
		root.SetArgs(c.args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%v: err = %v, want %q", c.args, err, c.wantErr)
		}
	}
}
