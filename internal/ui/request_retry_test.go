package ui

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestRequestRetryHintSkipsHumanAndRunningRows(t *testing.T) {
	live := (&Live{}) // Any attempted client read would panic.
	for _, tc := range []struct {
		state string
		agent bool
	}{{"interrupted", false}, {"needs_human", false}, {"running", true}, {"awaiting", true}, {"answered", true}} {
		actions, detail := live.agentRequestActions("conv", "id", KindTask, tc.state, "retained detail", tc.agent)
		if !reflect.DeepEqual(actions, AgentActions(KindTask, tc.state)) || detail != "retained detail" {
			t.Fatalf("changed ordinary row %+v: %v %q", tc, actions, detail)
		}
	}
}

func TestLegacyListAndAgentChildRendered(t *testing.T) {
	if os.Getenv("AGENTNET_PLAYWRIGHT") == "" {
		t.Skip("opt-in: installed Playwright required")
	}
	for _, mode := range []string{"AGENTNET_LEGACY_LIST_REGRESSION=1", "AGENTNET_AGENT_CHILD_REGRESSION=1"} {
		cmd := exec.CommandContext(t.Context(), "node", "testdata/group_guest_controls_rendered.cjs")
		cmd.Env = append(os.Environ(), "AGENTNET_SCREENSHOTS="+t.TempDir(), "AGENTNET_TEST_SKINS=comic", mode)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), `"ok":true`) {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
		t.Logf("%s", out)
	}
}
