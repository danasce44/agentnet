package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBrowserNativeApproval(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.CommandContext(t.Context(), node, "testdata/nativeapproval_engine_check.mjs").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}

func TestNativeApprovalComicRendered(t *testing.T) {
	if os.Getenv("AGENTNET_PLAYWRIGHT") == "" {
		t.Skip("opt-in: installed Playwright required")
	}
	cmd := exec.CommandContext(t.Context(), "node", "testdata/nativeapproval_rendered.cjs")
	cmd.Env = append(os.Environ(), "AGENTNET_SCREENSHOTS="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "NATIVE RENDERED PASS") {
		t.Fatalf("%v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
