package core

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/misunders2d/agentnet/internal/client"
	"github.com/misunders2d/agentnet/internal/hub"
)

type countedNetworkListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countedNetworkListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e == nil {
		l.accepted.Add(1)
	}
	return c, e
}

// Real signed TLS push traffic, not a mocked Run: route refresh creates a new
// remote connection while preserving the mobile host, key and current Run.
func TestAppNetworkChangedReconnectsWithoutReplacingHost(t *testing.T) {
	t.Setenv("AGENTNET_NOTIFY", "off")
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	counted := &countedNetworkListener{Listener: ln}
	home := t.TempDir()
	relay, e := hub.Open(hub.Config{DataDir: home, PublicURL: "https://" + ln.Addr().String(), AdminLabel: "owner", Version: "synthetic-test", Logf: func(string, ...any) {}})
	if e != nil {
		ln.Close()
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = relay.Serve(ctx, counted) }()
	defer func() { cancel(); <-done; relay.Close() }()
	code, e := os.ReadFile(filepath.Join(home, hub.BootstrapFile))
	if e != nil {
		t.Fatal(e)
	}
	appHome := t.TempDir()
	enrolled, e := client.Join(ctx, appHome, strings.TrimSpace(string(code)), "phone")
	if e != nil {
		t.Fatal(e)
	}
	address, fp := enrolled.Address, enrolled.Self().Fingerprint()
	enrolled.Close()
	app, e := OpenApp(appHome, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	url, origin, port := app.URL(), app.Origin(), app.Port()
	if e = app.Start(); e != nil {
		t.Fatal(e)
	}
	waitMobile(t, "real phone push connected", func() bool { return app.session.a.MemberView().Current })
	app.session.mu.Lock()
	run := app.session.done
	app.session.mu.Unlock()
	before := counted.accepted.Load()
	app.NetworkChanged()
	waitMobile(t, "fresh route TLS connection", func() bool { return counted.accepted.Load() > before && app.session.a.MemberView().Current })
	if e = app.Start(); e != nil {
		t.Fatal(e)
	}
	app.session.mu.Lock()
	sameRun := app.session.done == run
	app.session.mu.Unlock()
	if !sameRun || app.URL() != url || app.Origin() != origin || app.Port() != port || app.session.a.Address != address || app.session.a.Self().Fingerprint() != fp {
		t.Fatal("network event replaced running host or identity")
	}
	app.Stop()
	app.NetworkChanged()
	if _, e = app.session.OverviewJSON(); e != nil {
		t.Fatal("offline data unavailable", e)
	}
	if e = app.Start(); e != nil {
		t.Fatal(e)
	}
	waitMobile(t, "stopped phone reconnect", func() bool { return app.session.a.MemberView().Current })
	if app.URL() != url || app.session.a.Self().Fingerprint() != fp {
		t.Fatal("restart changed local identity/origin")
	}
}
