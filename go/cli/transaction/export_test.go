// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

// SetBuildHookForTest installs a callback that fires once per
// sourceSHAIndex.build() invocation. Returns the restore function.
//
// Test-only seam (file is _test.go, so it's compiled only into the
// test binary and not visible to package consumers in production).
// Per spec 0018 §7 — used to assert build counts in lazy-index tests.
func SetBuildHookForTest(f func()) func() {
	prev := buildHook
	buildHook = f
	return func() { buildHook = prev }
}

// OrderDonorsFirstForTest exposes orderDonorsFirst for the ordering
// unit tests in plan_order_test.go (spec 0026).
var OrderDonorsFirstForTest = orderDonorsFirst
