package ui

import (
	"os/exec"
	"testing"
)

func TestBrowserChatProjection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	out, err := exec.Command(node, "testdata/chatprojection_check.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Log(string(out))
}

func TestBrowserChatProjectionRealIndexedDB(t *testing.T) {
	testOwnDeviceModuleIndexedDB(t, "chatprojection_check.mjs", "chatProjectionResult")
}
