package client

import (
	"context"
	"github.com/misunders2d/agentnet/internal/envelope"
	"testing"
)

func TestContinuationRevokedExactHostStopsAction(t *testing.T) {
	w := newWorld(t, "")
	if _, err := w.alice.CreatePerson(tctx(t), "Alice"); err != nil {
		t.Fatal(err)
	}
	runAgent(t, w.alice)
	phone := linkedVia(t, w.alice, "phone", func(ctx context.Context, id string) error { return w.alice.DecideLink(ctx, id, true) })
	sent, err := phone.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Kind: envelope.KindQuestion, Body: "original"})
	if err != nil {
		t.Fatal(err)
	}
	exec := &ExecView{Host: w.alice.Address, State: stateNeedHuman, Attempt: 1}
	check := func(want bool) {
		t.Helper()
		got, err := phone.ContinuationFor(sent.ID, phone.Self().Fingerprint(), exec)
		if err != nil || (got != nil) != want {
			t.Fatalf("action=%+v want=%v err=%v", got, want, err)
		}
	}
	check(true) // pending/offline alone is not permanent revocation
	if err = phone.store.endRecipient(w.alice.Address, phone.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err = phone.store.endRecipient(w.alice.Address, w.alice.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	check(false)
	if exec.State != stateNeedHuman {
		t.Fatal("historical status mutated")
	}
}
