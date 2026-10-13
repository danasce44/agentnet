package ui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/client"
	"github.com/misunders2d/agentnet/internal/hub"
	"github.com/misunders2d/agentnet/internal/protocol"
	"github.com/misunders2d/agentnet/internal/testhub"
)

// Real linked browser + native host, with signed events and an actual offline
// interval. No manual invitation acceptance or permission grant is used.
func TestOwnInviteBrowserHostJourney(t *testing.T) {
	t.Setenv("AGENTNET_NOTIFY", "off")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "hub")
	// The browser reconnects after enrollment. Its pre-link session advertised
	// no conversation capabilities; expire that isolated stale session promptly.
	relay := testhub.StartConfig(t, hub.Config{DataDir: dir, SessionGrace: time.Second}, "127.0.0.1:0")
	home := filepath.Join(t.TempDir(), "host")
	host, err := client.Join(ctx, home, testhub.BootstrapCode(t, dir), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	stop := runDaemon(t, host)
	if _, err = host.CreatePerson(ctx, "Alice"); err != nil {
		t.Fatal(err)
	}
	if err = host.SetResponder(&client.Responder{Harness: "claude", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	offer, err := host.NewDeviceLink(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o, err := protocol.DecodeLinkOffer(offer.Code)
	if err != nil {
		t.Fatal(err)
	}
	o.Invite = browserCode(t, o.Invite)
	w := startEngineNode(t, dir)
	base := "https://" + relay.Addr
	w.ok(map[string]any{"op": "init", "base": base})
	tablet := w.ok(map[string]any{"op": "joinLink", "code": base + "/#" + o.Encode(), "name": "tablet"})["address"].(string)
	w.ok(map[string]any{"op": "start"})
	var link string
	waitFor(t, "device enrollment", func() bool {
		rs, _ := host.PendingLinks()
		for _, r := range rs {
			if r.State == "pending" && r.Address == tablet {
				link = r.ID
			}
		}
		return link != ""
	})
	if err = host.DecideLink(ctx, link, true); err != nil {
		t.Fatal(err)
	} // device enrollment, not agent consent
	w.until("own browser linked", func() bool { return w.ok(map[string]any{"op": "status"})["link"] == "linked" })
	packet, err := host.CreateGroup(ctx, "Own invitation journey")
	if err != nil {
		t.Fatal(err)
	}
	conv := packet.State.Conv
	reachBrowser(t, w, host, conv, tablet) // established, readable group before the invitation under test
	w.until("browser group history", func() bool { return w.call(map[string]any{"op": "api", "path": "/api/dm?id=" + conv})["error"] == nil })
	stop()
	started := time.Now()
	inv := w.api("/api/dm/agent/invite", map[string]any{"conv": conv, "host": host.Address, "note": "No second consent"})
	pid, _ := inv["pid"].(string)
	if inv["state"] != "invited" || inv["owner_invited"] != true || inv["can_decide"] == true || inv["can_ask"] == true || !strings.Contains(inv["state_text"].(string), "Joining") {
		t.Fatalf("offline projection: %v", inv)
	}
	t.Logf("browser pending at %s (%s)", time.Now().UTC().Format(time.RFC3339Nano), time.Since(started))
	snapshots := map[string]any{}
	browserSnapshot := func(name string) {
		overview := w.api("/api/overview", nil)
		if items, _ := overview["needs_you"].([]any); len(items) != 0 {
			t.Fatalf("own invite creates approval: %v", items)
		}
		snapshots[name] = map[string]any{"overview": overview, "thread": w.api("/api/dm?id="+conv, nil), "platform": "browser"}
	}
	browserSnapshot("browser-pending")
	// Even after another view/retry while the host is offline, no fake acceptance.
	if v := w.api("/api/dm/agent/invite", map[string]any{"conv": conv, "host": host.Address}); v["pid"] != pid || v["state"] != "invited" {
		t.Fatalf("offline retry: %v", v)
	}
	browserSnapshot("browser-delayed")
	stop = runDaemon(t, host)
	waitFor(t, "host signed automatic acceptance", func() bool {
		p, e := host.Participation(pid)
		return e == nil && p.State == client.PartActive && p.Decision != ""
	})
	w.until("signed acceptance in browser", func() bool {
		for _, v := range w.api("/api/dm?id="+conv, nil)["agents"].([]any) {
			a := v.(map[string]any)
			if a["pid"] == pid {
				return a["state"] == "active" && a["can_ask"] == true
			}
		}
		return false
	})
	t.Logf("browser active at %s (%s), no consent call", time.Now().UTC().Format(time.RFC3339Nano), time.Since(started))
	browserSnapshot("browser-active")
	// Same-host disabled agent: surface the actual setup blocker, no OK card.
	stop()
	if err = host.SetResponder(nil); err != nil {
		t.Fatal(err)
	}
	packet, err = host.CreateGroup(ctx, "Local own invitation")
	if err != nil {
		t.Fatal(err)
	}
	conv = packet.State.Conv
	local, err := host.InviteAgent(ctx, conv, host.Address, nil, nil, "local owner")
	if err != nil || local.State != client.PartInvited {
		t.Fatalf("disabled local invite: %+v %v", local, err)
	}
	live := NewLive(host)
	nativeSnapshot := func(name string) {
		overview, e := live.Overview()
		if e != nil {
			t.Fatal(e)
		}
		thread, e := live.DM(conv)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range thread.Agents {
			if a.PID == local.PID && a.State == "invited" && (a.CanDecide || !a.OwnerInvited || !strings.Contains(a.StateText, "Enable this agent")) {
				t.Fatalf("native own pending: %+v", a)
			}
		}
		if len(overview.NeedsYou) != 0 {
			t.Fatalf("native own invitation asks for consent: %+v", overview.NeedsYou)
		}
		snapshots[name] = map[string]any{"overview": overview, "thread": thread, "platform": "daemon"}
	}
	nativeSnapshot("native-pending")
	if err = host.SetResponder(&client.Responder{Harness: "claude", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	runDaemon(t, host)
	waitFor(t, "configured local agent autojoins", func() bool { p, e := host.Participation(local.PID); return e == nil && p.State == client.PartActive })
	nativeSnapshot("native-active")
	if os.Getenv("AGENTNET_PLAYWRIGHT") != "" {
		fixture := filepath.Join(t.TempDir(), "views.json")
		data, _ := json.Marshal(snapshots)
		if err = os.WriteFile(fixture, data, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "node", "testdata/own_invite_rendered.cjs", fixture)
		cmd.Env = append(os.Environ(), "AGENTNET_SCREENSHOTS="+t.TempDir())
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("rendered own invitation: %v\n%s", e, out)
		}
		t.Logf("%s", out)
	} else {
		t.Log("Rendered coverage not requested; set AGENTNET_PLAYWRIGHT to execute it.")
	}
}
