package ui

import (
	"os/exec"
	"testing"
)

func TestBrowserVerifiedRosterPrefix(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	out, err := exec.Command(node, "testdata/roster_prefix_check.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	t.Log(string(out))
}
