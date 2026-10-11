package core

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/client"
	"github.com/misunders2d/agentnet/internal/hub"
	"github.com/misunders2d/agentnet/internal/protocol"
	"github.com/misunders2d/agentnet/internal/ui"
)

// Existing TLS sockets become silent; newly accepted sockets remain usable.
// Closing a retired socket unblocks the fault without a timer or mock Run.
type mobileRouteConn struct {
	net.Conn
	broken atomic.Bool
	closed chan struct{}
	once   sync.Once
}

func (c *mobileRouteConn) Read(p []byte) (int, error) {
	for {
		n, err := c.Conn.Read(p)
		if !c.broken.Load() || err != nil {
			return n, err
		}
		// Drop old-route bytes, but still observe the client's actual close.
	}
}
func (c *mobileRouteConn) Write(p []byte) (int, error) {
	if c.broken.Load() {
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}
func (c *mobileRouteConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type mobileRouteListener struct {
	net.Listener
	mu    sync.Mutex
	conns []*mobileRouteConn
}

func (l *mobileRouteListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	x := &mobileRouteConn{Conn: c, closed: make(chan struct{})}
	l.mu.Lock()
	l.conns = append(l.conns, x)
	l.mu.Unlock()
	return x, nil
}
func (l *mobileRouteListener) breakExisting() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		c.broken.Store(true)
	}
	return len(l.conns)
}
func (l *mobileRouteListener) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.conns)
}

func TestAppNetworkRecoveryPreservesQueuedQuestionAndReceive(t *testing.T) {
	t.Setenv("AGENTNET_NOTIFY", "off")
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	route := &mobileRouteListener{Listener: ln}
	home := t.TempDir()
	relay, err := hub.Open(hub.Config{DataDir: home, PublicURL: "https://" + ln.Addr().String(), AdminLabel: "owner", Version: "synthetic-test", Logf: func(string, ...any) {}})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = relay.Serve(ctx, route) }()
	defer func() { cancel(); <-done; relay.Close() }()
	code, err := os.ReadFile(filepath.Join(home, hub.BootstrapFile))
	if err != nil {
		t.Fatal(err)
	}
	phoneHome := t.TempDir()
	enrolled, err := client.Join(ctx, phoneHome, strings.TrimSpace(string(code)), "phone")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := enrolled.Invite(ctx, "peer", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	address, key := enrolled.Address, enrolled.Self().Fingerprint()
	enrolled.Close()
	peer, err := client.Join(ctx, t.TempDir(), invite, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peerCtx, stopPeer := context.WithCancel(ctx)
	peerDone := make(chan struct{})
	go func() { defer close(peerDone); _ = peer.Run(peerCtx, client.RunOptions{HumanOnly: true}) }()
	defer func() { stopPeer(); <-peerDone }()
	app, err := OpenApp(phoneHome, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	url := app.URL()
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	waitMobile(t, "real phone and peer streams", func() bool { return app.session.a.MemberView().Current && peer.MemberView().Current })
	app.Stop()
	id := protocol.NewID()
	draft, _ := json.Marshal(ui.Draft{ID: id, To: peer.Address, Kind: "question", Body: "Original queued mobile question"})
	raw, err := app.session.SendJSON(string(draft))
	if err != nil {
		t.Fatal(err)
	}
	var queued ui.Sent
	if err = json.Unmarshal([]byte(raw), &queued); err != nil || queued.ID != id || queued.State != "queued" {
		t.Fatal("original question was not durably queued", queued, err)
	}
	old := route.breakExisting()
	resumeStarted := time.Now()
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	peer.NetworkChanged()
	waitMobile(t, "new TLS route after stopped resume", func() bool {
		return route.count() > old && app.session.a.MemberView().Current && peer.MemberView().Current
	})
	waitMobile(t, "original question at peer", func() bool {
		thread, err := ui.NewLive(peer).Thread(id)
		return err == nil && len(thread.Messages) == 1 && thread.Messages[0].ID == id && thread.Messages[0].Body == "Original queued mobile question" && thread.Messages[0].State == "held"
	})
	t.Logf("stopped resume to original question stored: %s", time.Since(resumeStarted))
	// Break the active push route too, then deliver in both directions using
	// the public App lifecycle hook rather than replacing Session or its Run.
	app.session.mu.Lock()
	run := app.session.done
	app.session.mu.Unlock()
	old = route.breakExisting()
	peer.NetworkChanged()
	incoming, err := peer.Send(client.WithQueuedSend(ctx, protocol.NewID()), address, "Incoming after active handover", "")
	if err != nil {
		t.Fatal(err)
	}
	handoverStarted := time.Now()
	app.NetworkChanged()
	waitMobile(t, "incoming stored through fresh phone stream", func() bool {
		messages, err := app.session.a.Inbox(false, false)
		for _, m := range messages {
			if m.ID == incoming.ID && m.Body == "Incoming after active handover" {
				return err == nil
			}
		}
		return false
	})
	t.Logf("active network change to incoming stored: %s", time.Since(handoverStarted))
	waitMobile(t, "proven incoming delivery", func() bool {
		thread, err := ui.NewLive(peer).Thread(incoming.ID)
		return err == nil && len(thread.Messages) == 1 && thread.Messages[0].State == "delivered"
	})
	t.Logf("active network change to proven incoming delivery: %s", time.Since(handoverStarted))
	app.session.mu.Lock()
	sameRun := app.session.done == run
	app.session.mu.Unlock()
	if !sameRun || route.count() <= old || app.URL() != url || app.session.a.Address != address || app.session.a.Self().Fingerprint() != key {
		t.Fatal("network recovery replaced the run, origin or identity")
	}
	// Repeated lifecycle wakes drain existing storage, not another UI send.
	// The unrelated peer remains held: networking grants no permission.
	app.NetworkChanged()
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	waitMobile(t, "original question proven delivered once", func() bool {
		thread, err := app.session.live.Thread(id)
		return err == nil && len(thread.Messages) == 1 && thread.Messages[0].ID == id && thread.Messages[0].Body == "Original queued mobile question" && thread.Messages[0].State == "delivered"
	})
	thread, err := ui.NewLive(peer).Thread(id)
	if err != nil || len(thread.Messages) != 1 || thread.Messages[0].ID != id || thread.Messages[0].State != "held" {
		t.Fatal("lifecycle wake duplicated or admitted original question", thread, err)
	}
	if agents, err := app.session.a.LocalAgents(); err != nil || len(agents) != 0 {
		t.Fatal("recovery introduced a phone executor", err)
	}
}
