//go:build linux

package ui

import "testing"

func TestBrowserGroupDismissalCanonicalHistory(t *testing.T) {
	testBrowserGroupCarrierEngine(t, "dismiss-canonical")
}

func TestBrowserGroupDismissalCanonicalRealIndexedDB(t *testing.T) {
	testBrowserGroupCarrierIndexedDB(t, false, false, false, false, false, false, false, true)
}
