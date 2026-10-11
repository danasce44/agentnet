package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

// The selected executor must enforce its topic. Other original members keep
// exact sealed copies waiting independently until their own apps can read them.
func TestTopicAgentInviteUnrelatedOlderReaderDoesNotBlock(t *testing.T) {
	w, old, packet, stops := groupTurnsFixture(t)
	a, host := w.alice, w.bob
	stops[a]() // deterministic signed profiles and members view, no live refresh
	signCapsAfter(t, old, without(ownCaps, protocol.CapTopicParticipation))
	everyone := []string{a.Address, host.Address, old.Address}
	var invited []ParticipationInfo
	for _, suspended := range []bool{true, false} {
		var paused map[string]bool
		if suspended {
			paused = map[string]bool{old.Address: true}
		}
		listMembers(t, a, paused, everyone...)
		topic := protocol.NewID()
		p, err := a.InviteAgentInScope(tctx(t), packet.State.Conv, host.Address, "", nil, nil, "", &topic)
		if err != nil {
			t.Fatalf("unrelated older reader (suspended=%v) stopped healthy agent invitation: %v", suspended, err)
		}
		invited = append(invited, p)
		eventually(t, "selected host receives exact scoped invitation", func() bool {
			got := stateAt(t, host, p.PID)
			return got.Invite != "" && got.Topic != nil && *got.Topic == topic
		})
		var waiting int
		if err = a.store.db.QueryRow(`SELECT count(*) FROM outbox WHERE recipient=? AND pid=? AND state='waiting' AND instr(coalesce(error,''),?)=1`, old.Address, p.PID, WaitPeerUpdate).Scan(&waiting); err != nil || waiting == 0 {
			t.Fatalf("old reader's copies did not wait independently: %d %v", waiting, err)
		}
		if stateAt(t, old, p.PID).Invite != "" {
			t.Fatal("incompatible reader learned a scoped invitation")
		}
	}
	// The target's capability still is mandatory, even if the relay also calls
	// that target suspended. No invite is committed on a failed host precheck.
	signCapsAfter(t, host, without(ownCaps, protocol.CapTopicParticipation))
	listMembers(t, a, map[string]bool{host.Address: true}, everyone...)
	topic := protocol.NewID()
	var update *NeedsUpdateError
	if _, err := a.InviteAgentInScope(tctx(t), packet.State.Conv, host.Address, "", nil, nil, "", &topic); !errors.As(err, &update) || update.Address != host.Address {
		t.Fatalf("old target host was not refused specifically: %v", err)
	}
	signCapsAfter(t, host, ownCaps)
	m, err := a.dmMembers(packet.State.Conv)
	if err != nil {
		t.Fatal(err)
	}
	changed := protocol.ParticipationHost{Address: host.Address, Fingerprint: strings.Repeat("f", 64)}
	if err = a.requireTopicHost(tctx(t), m, changed); err == nil {
		t.Fatal("changed exact target key accepted")
	}
	// Updating the reader releases the SAME envelopes; no invitation is remade.
	type held struct{ id, raw string }
	var heldCopies []held
	rows, err := a.store.db.Query(`SELECT id,envelope FROM outbox WHERE recipient=? AND pid IN (?,?)`, old.Address, invited[0].PID, invited[1].PID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var h held
		if err = rows.Scan(&h.id, &h.raw); err != nil {
			t.Fatal(err)
		}
		heldCopies = append(heldCopies, h)
	}
	rows.Close()
	signCapsAfter(t, old, ownCaps)
	listMembers(t, a, nil, everyone...)
	features, err := a.relayFeatures(tctx(t))
	if err != nil {
		t.Fatal(err)
	}
	a.releaseConv(tctx(t), features)
	if err = a.FlushOutbox(tctx(t)); err != nil {
		t.Fatal(err)
	}
	for _, p := range invited {
		eventually(t, "updated reader receives retained scoped invitation", func() bool { return stateAt(t, old, p.PID).Invite != "" })
	}
	for _, h := range heldCopies {
		var raw string
		if err = a.store.db.QueryRow(`SELECT envelope FROM outbox WHERE id=?`, h.id).Scan(&raw); err != nil || raw != h.raw {
			t.Fatal("reader update regenerated sealed carrier")
		}
	}
	if err = a.Revoke(tctx(t), host.Address); err != nil {
		t.Fatal(err)
	}
	revokedTopic := protocol.NewID()
	if _, err = a.InviteAgentInScope(tctx(t), packet.State.Conv, host.Address, "", nil, nil, "", &revokedTopic); err == nil {
		t.Fatal("revoked target host accepted")
	}
}
