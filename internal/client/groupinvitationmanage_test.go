package client

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
)

func TestLinkedHumanCancelsInvitationWithOriginOffline(t *testing.T) {
	w, p := groupLifecycleFixture(t, false)
	stop := runAgent(t, w.alice)
	phone := linked(t, w.alice)
	stop()
	if err := phone.SyncGroupFromRoot(tctx(t), p.Root); err != nil {
		t.Fatal(err)
	}
	if err := phone.AcceptGroupContext(tctx(t), p); err != nil {
		t.Fatal(err)
	}
	inv := groupLifecycleInvite(t, w, p, nil)
	if _, err := phone.refreshPerson(tctx(t), inv.Proposal.Target, false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.alice.syncInvitations(); err != nil {
		t.Fatal(err)
	}
	var view []byte
	if err := w.alice.store.db.QueryRow(`SELECT envelope FROM outbox WHERE recipient=? AND sub='invitation-sync' ORDER BY rowid DESC LIMIT 1`, phone.Address).Scan(&view); err != nil {
		t.Fatal(err)
	}
	var e envelope.Envelope
	if err := json.Unmarshal(view, &e); err != nil {
		t.Fatal(err)
	}
	if err := phone.verifyAndStore(tctx(t), e); err != nil {
		t.Fatal(err)
	}
	infos, err := phone.GroupInvitations()
	if err != nil {
		t.Fatal(err)
	}
	var visible bool
	for _, v := range infos {
		if v.ID == inv.ID {
			visible = v.CanCancel && v.CanRefresh
		}
	}
	if !visible {
		t.Fatalf("linked own human lacks actions: %+v", infos)
	}
	var n int
	phone.store.db.QueryRow(`SELECT count(*) FROM group_invitations WHERE id=?`, inv.ID).Scan(&n)
	if n != 0 {
		t.Fatal("passive mirror promoted to local intent")
	}
	deliverGroupLifecycleSubtype(t, w.alice, w.bob, envelope.SubGroupProof)
	deliverGroupLifecycleSubtype(t, w.alice, w.bob, envelope.SubGroupInvite)
	if err := w.bob.DecideGroupInvitation(tctx(t), inv.ID, true); err != nil {
		t.Fatal(err)
	}
	injectFaults(w.alice).add("POST", "/publish", 1, false)
	deliverGroupLifecycleSubtype(t, w.bob, w.alice, envelope.SubGroupConsent)
	var original []byte
	if err := w.alice.store.db.QueryRow(`SELECT record FROM group_publications WHERE conv=? AND seq=1 AND published=0`, p.Root.ID()).Scan(&original); err != nil {
		t.Fatal(err)
	}
	// Simulate a durable attempt created by the previous release: no binding
	// metadata yet, and exact signed ciphertext must survive the upgrade.
	if _, err := w.alice.store.db.Exec(`UPDATE group_publications SET invitation=NULL WHERE conv=? AND seq=1`, p.Root.ID()); err != nil {
		t.Fatal(err)
	}
	home := w.alice.home
	w.alice.Close()
	// Original process is closed: cancellation cannot depend on its stream.
	injectFaults(phone).add("POST", "/cancel-invitation", 1, true)
	if err := phone.CancelGroupInvitation(tctx(t), inv.ID); err == nil {
		t.Fatal("lost cancellation response claimed success")
	}
	if err := phone.CancelGroupInvitation(tctx(t), inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := phone.CancelGroupInvitation(tctx(t), inv.ID); err != nil {
		t.Fatal(err)
	}
	deliverGroupLifecycleSubtype(t, phone, w.bob, envelope.SubGroupConsent)
	row, err := groupInvitationIn(w.bob.store.db, inv.ID, "in")
	if err != nil || row.State != "cancelled" {
		t.Fatalf("same-person cancellation not admitted: %s %v", row.State, err)
	}
	a, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	w.alice = a
	if err = a.RecoverGroupPublications(tctx(t)); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("old publication bypassed terminal fence: %v", err)
	}
	var recovered []byte
	if err = a.store.db.QueryRow(`SELECT record FROM group_publications WHERE conv=? AND seq=1`, p.Root.ID()).Scan(&recovered); err != nil || !bytes.Equal(original, recovered) {
		t.Fatal("upgrade replaced uncertain publication bytes", err)
	}
	if err = a.RecoverGroupPublications(tctx(t)); err != nil {
		t.Fatalf("terminal cancellation retried forever: %v", err)
	}
	row, err = groupInvitationIn(a.store.db, inv.ID, "out")
	if err != nil || row.State != "cancelled" {
		t.Fatalf("source did not reconcile exact cancelled intent: %s %v", row.State, err)
	}
	ctx, err := a.GroupContext(p.Root.ID())
	if err != nil || ctx.State.Seq != p.State.Seq {
		t.Fatal("cancel changed group membership", err)
	}
}
