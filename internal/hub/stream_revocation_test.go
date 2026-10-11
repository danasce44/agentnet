package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestStreamRevocationBetweenAuthenticationAndSubscription(t *testing.T) {
	for _, tc := range []struct{ reason, code string }{{"refused", protocol.CodeLinkRefused}, {"expired", protocol.CodeLinkExpired}, {"removed", protocol.CodeRevoked}} {
		t.Run(tc.reason, func(t *testing.T) {
			h, id, addr := testHub(t)
			ad := protocol.SessionAd{Address: addr, Session: protocol.NewID()}
			protocol.SignAd(&ad, id.Sign)
			req := signed(t, id, addr, "GET", "/v1/stream?ad="+ad.Encode(), nil)
			w := httptest.NewRecorder()
			// Hold subscription registration while the real signed request authenticates.
			h.streams.mu.Lock()
			locked := true
			defer func() {
				if locked {
					h.streams.mu.Unlock()
				}
			}()
			done := make(chan struct{})
			go func() { h.handleStream(w, req); close(done) }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var n int
				if err := h.store.db.QueryRow(`SELECT count(*) FROM nonces WHERE agent=? AND nonce=?`, addr, req.Header.Get(protocol.HeaderNonce)).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("signed request did not authenticate")
				}
				time.Sleep(time.Millisecond)
			}
			if _, err := h.store.db.Exec(`UPDATE agents SET revoked_at=?,revoked_reason=? WHERE address=?`, time.Now().Unix(), tc.reason, addr); err != nil {
				t.Fatal(err)
			}
			h.streams.mu.Unlock()
			locked = false
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("revoked stream remained open")
			}
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("revocation %s: status=%d body=%s", tc.reason, w.Code, w.Body.String())
			}
		})
	}
}
