package client

import (
	"encoding/json"
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestDismissedParticipationSurvivesRetiredInviter(t *testing.T) {
	w, _, packet, stops := groupTurnsFixture(t)
	phone := linked(t, w.alice)
	stopPhone := runAgent(t, phone)
	publishGroupFixtureCaps(t, phone, true)
	copies, err := w.alice.groupDeliveryCopies(tctx(t), packet)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.alice.store.addConvOutbox(copies, envelope.Inner{}, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err = w.alice.FlushOutbox(tctx(t)); err != nil {
		t.Fatal(err)
	}
	groupGovernanceAwait(t, packet, phone)
	conv := packet.State.Conv
	inv := roomInvite(t, phone, conv, w.bob, "", 0)
	me, _, _ := phone.store.selfPerson(phone.Address)
	end := protocol.ParticipationEvent{V: 1, Conv: conv, PID: inv.PID, Type: protocol.EventDismiss, Prev: inv.Hash(), TS: inv.TS + 1, Author: inv.Author}
	end.Sign(phone.id.Sign)
	for _, ev := range []protocol.ParticipationEvent{inv, end} {
		raw, _ := json.Marshal(ev)
		if err = w.bob.store.addParticipationEvent(ev, raw); err != nil {
			t.Fatal(err)
		}
	}
	if p := stateAt(t, w.bob, inv.PID); p.State != PartDismissed {
		t.Fatalf("before rotation: %+v", p)
	}
	stopPhone()
	stops[w.bob]()
	if err = w.alice.RemoveDevice(tctx(t), phone.Address); err != nil {
		t.Fatal(err)
	}
	if _, err = w.bob.refreshPerson(tctx(t), me.info.Person, false); err != nil {
		t.Fatal(err)
	}
	p := stateAt(t, w.bob, inv.PID)
	if p.State != PartDismissed || p.Held != 0 || p.Invite != inv.Hash() || p.Dismissal != end.Hash() || p.Claimable() || p.Following() || len(p.TaskKeys)+len(p.Grant) != 0 {
		t.Fatalf("retired inviter changed terminal history: %+v", p)
	}
	m, err := w.bob.dmMembers(conv)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.author(inv.Author); ok {
		t.Fatal("retired key regained live authority")
	}
	// Dropping the terminal record must never restore its old live invite.
	events := []protocol.ParticipationEvent{inv}
	if err = m.loadHosts(w.bob.store.db, events); err != nil {
		t.Fatal(err)
	}
	base := resolve(conv, inv.PID, events, m)
	got, err := closedParticipation(w.bob.store.db, conv, inv.PID, events, m, base)
	if err != nil || got.State != PartPending || got.Invite != "" {
		t.Fatalf("historical invitation gained authority: %+v %v", got, err)
	}
	// Even for display, corrupt signatures cannot manufacture a closed record.
	bad := end
	bad.Sig = append([]byte(nil), end.Sig...)
	bad.Sig[0] ^= 1
	events = []protocol.ParticipationEvent{inv, bad}
	base = resolve(conv, inv.PID, events, m)
	got, err = closedParticipation(w.bob.store.db, conv, inv.PID, events, m, base)
	if err != nil || got.State == PartDismissed {
		t.Fatalf("forged historical dismissal: %+v %v", got, err)
	}
}
