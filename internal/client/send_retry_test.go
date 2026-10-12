package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestSendRateLimitRetriesExactCopy(t *testing.T) {
	w := newWorld(t, "")
	// One actual TLS HTTP 429, as a busy relay/proxy may return. The next
	// attempt uses the real Hub, with unchanged signed/encrypted bytes.
	busy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "try later", http.StatusTooManyRequests)
	}))
	defer busy.Close()
	base := w.alice.hub.http.Transport
	var posted []string
	w.alice.hub.http.Transport = storageTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && r.URL.Path == "/v1/messages" {
			body, err := r.GetBody()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(body)
			body.Close()
			if err != nil {
				return nil, err
			}
			posted = append(posted, string(data))
			if len(posted) == 1 {
				copy := r.Clone(r.Context())
				copy.URL, _ = url.Parse(busy.URL + r.URL.RequestURI())
				return busy.Client().Transport.RoundTrip(copy)
			}
		}
		return base.RoundTrip(r)
	})
	res, err := w.alice.Send(tctx(t), w.bob.Address, "keep my exact send", "")
	if err != nil || res.State != stateQueued {
		t.Fatalf("rate limited send: %+v %v", res, err)
	}
	if err := w.alice.FlushOutbox(tctx(t)); err != nil {
		t.Fatal(err)
	}
	if s := outboxState(t, w.alice, res.ID); s != protocol.StateCustody {
		t.Fatalf("retry state %s", s)
	}
	if len(posted) != 2 || posted[0] != posted[1] {
		t.Fatalf("retry changed or duplicated the copy (%d posts)", len(posted))
	}
	if err := w.alice.FlushOutbox(tctx(t)); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Fatal("custody copy posted again")
	}
}

func TestSendRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&HubError{Status: 429}, true},
		{&HubError{Status: 503}, true},
		{&HubError{Status: 403}, false},
		{&HubError{Status: 400}, false},
		{errors.Join(errPermanent, context.DeadlineExceeded), false},
		{context.Canceled, true},
		{nil, false},
	} {
		if got := retryable(tc.err); got != tc.want {
			t.Errorf("retryable(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}
