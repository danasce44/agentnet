package ui

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The counts are deliberately mixed: deferred is not evidence of missing
// proof, and transport bookkeeping is not a request for the owner's approval.
type devicesDetailsFixture struct{ *Fixture }

func (f *devicesDetailsFixture) Overview() (Overview, error) {
	o, err := f.Fixture.Overview()
	if err != nil {
		return o, err
	}
	o.Role, o.Link, o.Links = "person", nil, nil
	o.Person = &PersonView{Person: "own", Label: "Owner", Address: "owner/laptop", State: PersonSelf,
		Devices: []DeviceView{
			{Address: "owner/laptop", Name: "Laptop", Fingerprint: "local-key", This: true},
			{Address: "owner/phone", Name: "Phone", Fingerprint: "phone-key"},
		},
	}
	o.History = []HistoryCopy{{Device: "owner/phone", Name: "Phone", State: "running", Total: 9, Done: 3,
		DeliveryKnown: true, Deferred: 7, Blocked: 6, Queued: 8, Custody: 212, Retained: 19}}
	return o, nil
}

func TestDevicesHistoryDetailsRendered(t *testing.T) {
	if os.Getenv("AGENTNET_PLAYWRIGHT") == "" {
		t.Skip("opt-in installed Playwright")
	}
	f := &devicesDetailsFixture{NewFixture(time.Now)}
	ts := httptest.NewUnstartedServer(nil)
	ts.Config.Handler = New(f, ts.Listener.Addr().String(), testToken).Handler()
	ts.Start()
	defer ts.Close()
	out, err := exec.Command("node", "testdata/devices_details_rendered.cjs", ts.URL+"/?t="+testToken).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}
