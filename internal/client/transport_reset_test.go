package client

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Future dials work, but this old mobile route silently drops traffic.
type resetLostRoute struct {
	net.Conn
	lost   atomic.Bool
	closed chan struct{}
	once   sync.Once
}

func (c *resetLostRoute) Write(b []byte) (int, error) {
	if c.lost.Load() {
		return len(b), nil
	}
	return c.Conn.Write(b)
}
func (c *resetLostRoute) Read(b []byte) (int, error) {
	if c.lost.Load() {
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Read(b)
}
func (c *resetLostRoute) Close() error { c.once.Do(func() { close(c.closed) }); return c.Conn.Close() }

func TestHubConnectionsResetHTTP2Route(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("not HTTP2: %s", r.Proto)
		}
		if r.URL.Path == "/body" || r.URL.Path == "/range" {
			if r.URL.Path == "/range" {
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write([]byte("{"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Method == "POST" {
			posts.Add(1)
		}
		w.WriteHeader(204)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	c, err := newHubConn(server.URL, cert, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.release()
	tr := c.http.Transport.(*http.Transport)
	tr.ResponseHeaderTimeout = 100 * time.Millisecond
	var dials atomic.Int32
	var first *resetLostRoute
	tr.DialContext = c.trackDial(func(ctx context.Context, n, a string) (net.Conn, error) {
		conn, e := (&net.Dialer{}).DialContext(ctx, n, a)
		if e != nil {
			return nil, e
		}
		wrapped := &resetLostRoute{Conn: conn, closed: make(chan struct{})}
		if dials.Add(1) == 1 {
			first = wrapped
		}
		return wrapped, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/stream", nil)
	stream, err := c.sendRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if first == nil {
		t.Fatal("no held connection")
	}
	first.lost.Store(true)
	req, _ = http.NewRequestWithContext(ctx, "POST", server.URL+"/send", nil)
	if _, err = c.sendRequest(req); err == nil {
		t.Fatal("lost route succeeded")
	}
	if posts.Load() != 0 {
		t.Fatal("timed-out POST was replayed")
	}
	req, _ = http.NewRequestWithContext(ctx, "POST", server.URL+"/send", nil)
	resp, err := c.sendRequest(req)
	if err != nil {
		t.Fatal("fresh route failed:", err)
	}
	resp.Body.Close()
	if dials.Load() < 2 || posts.Load() != 1 {
		t.Fatalf("dials=%d posts=%d", dials.Load(), posts.Load())
	}
	if tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("reset weakened pinned TLS")
	}
	// An old failure must not kill the healthy replacement connection.
	old := uint64(0)
	c.resetConnectionsAt(&old)
	req, _ = http.NewRequestWithContext(ctx, "GET", server.URL+"/ok", nil)
	resp, err = c.sendRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if dials.Load() != 2 {
		t.Fatal("old generation reset healthy route")
	}
	c.timeout = 100 * time.Millisecond
	var out map[string]any
	if err := c.doBytes(ctx, "GET", "/body", nil, &out); err == nil {
		t.Fatal("body blackhole did not time out")
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", server.URL+"/ok", nil)
	resp, err = c.sendRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if dials.Load() != 3 {
		t.Fatal("body timeout did not reset route")
	}
	var sink strings.Builder
	if err := c.getRange(ctx, "/range", 0, 2, &sink); err == nil {
		t.Fatal("range blackhole did not time out")
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", server.URL+"/ok", nil)
	resp, err = c.sendRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if dials.Load() != 4 {
		t.Fatal("range timeout did not reset route")
	}

}

func TestHubConnectionResetFencesDials(t *testing.T) {
	c := &hubConn{}
	entered, release := make(chan struct{}), make(chan struct{})
	left, right := net.Pipe()
	defer right.Close()
	dial := c.trackDial(func(context.Context, string, string) (net.Conn, error) { close(entered); <-release; return left, nil })
	done := make(chan error, 1)
	go func() { _, err := dial(context.Background(), "tcp", "synthetic"); done <- err }()
	<-entered
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.resetConnections() }()
	}
	wg.Wait()
	close(release)
	if err := <-done; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("late dial=%v", err)
	}
	if _, err := right.Write([]byte("closed")); err == nil {
		t.Fatal("late socket survived")
	}
}

type resetFailureTransport func(*http.Request) (*http.Response, error)

func (f resetFailureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHubConnectionResetDoesNotTreatCancellationAsRouteFailure(t *testing.T) {
	c := &hubConn{http: &http.Client{Transport: resetFailureTransport(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })}}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://synthetic.invalid", nil)
	if _, err := c.sendRequest(req); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if c.connectionGeneration() != 0 {
		t.Fatal("caller cancellation reset network")
	}
	c.http.Transport = resetFailureTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	resp, err := c.sendRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if c.connectionGeneration() != 0 {
		t.Fatal("HTTP authority refusal reset network")
	}
}

func TestHubConnectionCopiesShareResetTracking(t *testing.T) {
	c := &hubConn{connections: &hubConnections{}}
	left, right := net.Pipe()
	defer right.Close()
	dial := c.trackDial(func(context.Context, string, string) (net.Conn, error) { return left, nil })
	socket, err := dial(context.Background(), "tcp", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	clone := *c // waitReceipt changes only its timeout, retaining the same pool.
	clone.timeout = time.Minute
	clone.resetConnections()
	if c.connectionGeneration() != 1 || clone.connectionGeneration() != 1 {
		t.Fatal("copied pool has independent generations")
	}
	if _, err := right.Write([]byte("closed")); err == nil {
		t.Fatal("clone failed to close original tracked socket")
	}
	// Custom short-lived direct connections have no shared Hub pool tracker.
	custom := &hubConn{}
	custom.resetConnections()
	if custom.connectionGeneration() != 0 {
		t.Fatal("nil tracker mutated")
	}
}
