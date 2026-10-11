package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/protocol"
)

// Even without a platform network event, a silent stream must not leave its
// dead HTTP/2 route available for the next connection attempt. Pending device
// approval has the same bounded receive lifetime as an enrolled session.
func TestStreamWatchdogRetiresConnection(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "member"
		if pending {
			name = "pending link"
		}
		t.Run(name, func(t *testing.T) {
			realm := protocol.NewID()
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ProtoMajor != 2 {
					t.Errorf("expected HTTP/2, got %s", r.Proto)
				}
				if r.URL.Path == "/v1/version" {
					json.NewEncoder(w).Encode(protocol.VersionInfo{Protocol: protocol.ProtocolVersion, RealmID: realm})
					return
				}
				if r.URL.Path == "/v1/stream" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.Write([]byte(": connected\n\n"))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				w.Write([]byte("{}"))
			}))
			srv.EnableHTTP2 = true
			srv.StartTLS()
			defer srv.Close()
			a := enrolledAt(t, srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})))
			defer a.Close()
			a.heartbeat = 25 * time.Millisecond
			a.adQuery = "?ad=test"
			generation := a.hub.connectionGeneration()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var streamErr error
			if pending {
				_, streamErr = a.pendingStream(ctx, "", "")
			} else {
				_, streamErr = a.streamOnce(ctx)
			}
			if ctx.Err() != nil {
				t.Fatal("silent stream was not bounded by its watchdog")
			}
			if a.hub.connectionGeneration() <= generation {
				t.Fatalf("watchdog retained dead connection pool: %v", streamErr)
			}
			if err := a.hub.do(ctx, "GET", "/v1/version", nil, nil); err != nil {
				t.Fatal("fresh connection failed:", err)
			}
		})
	}
}
