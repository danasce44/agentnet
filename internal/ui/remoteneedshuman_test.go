package ui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRemoteNeedsHumanCardModel(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "testdata/remote_needs_human_check.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}

type remoteNeedsHumanFixture struct{ *Fixture }

func (f *remoteNeedsHumanFixture) Overview() (Overview, error) {
	o, err := f.Fixture.Overview()
	if err != nil {
		return o, err
	}
	const seed = `{"me":{"address":"owner/zenbook","agent":true},"person":{"person":"own","address":"owner/zenbook","state":"self","devices":[{"address":"owner/zenbook"},{"address":"owner/bezos"},{"address":"owner/linux-laptop"}]},"threads":[{"id":"11111111111111111111111111111111","peer":"owner/bezos","count":1,"last":"Who is Bernard?","last_at":"2026-10-11T11:44:00Z"}],"dms":[],"needs_you":[],"review":[{"id":"22222222222222222222222222222222","peer":"owner/bezos","notice":true,"at":"2026-10-11T11:44:00Z","report":{"v":2,"host":"owner/bezos","at":1791719040,"items":[{"id":"11111111111111111111111111111111","key":"verified-key","kind":"question","state":"needs_human","attempt":2,"excerpt":"Professional memory unavailable; provide Bernard’s surname."}]}}],"agent_devices":["owner/bezos"],"people":[]}`
	o.Threads, o.Review = nil, nil // Do not retain unrelated fixture slice element fields.
	err = json.Unmarshal([]byte(seed), &o)
	return o, err
}
func (f *remoteNeedsHumanFixture) Thread(string) (Thread, error) {
	var t Thread
	err := json.Unmarshal([]byte(`{"id":"11111111111111111111111111111111","peer":"owner/bezos","messages":[{"id":"11111111111111111111111111111111","from":"owner/linux-laptop","to":"owner/bezos","dir":"out","kind":"question","body":"Who is Bernard?","history":true,"author":{},"at":"2026-10-11T11:44:00Z","exec":{"host":"owner/bezos","state":"needs_human","attempt":2,"detail":"needs_human"},"continuation":{"host":"owner/bezos","id":"11111111111111111111111111111111","key":"verified-key","attempt":2},"actions":["continue"]}]}`), &t)
	return t, err
}

func TestRemoteNeedsHumanCardRendered(t *testing.T) {
	if os.Getenv("AGENTNET_PLAYWRIGHT") == "" {
		t.Skip("opt-in installed Playwright")
	}
	f := &remoteNeedsHumanFixture{Fixture: NewFixture(time.Now)}
	ts := httptest.NewUnstartedServer(nil)
	ts.Config.Handler = New(f, ts.Listener.Addr().String(), testToken).Handler()
	ts.Start()
	defer ts.Close()
	cmd := exec.Command("node", "testdata/remote_needs_human_rendered.cjs", ts.URL+"/?t="+testToken)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}
