package client

import (
	"errors"
	"github.com/misunders2d/agentnet/internal/envelope"
	"testing"
)

func TestProposalRevokedExactHostStopsConfirmation(t *testing.T) {
	w := newWorld(t, "")
	a, b := w.alice, w.bob
	request := envelope.Inner{Kind: envelope.KindQuestion, Body: "question"}
	req := sealTo(t, a, b, request)
	request.ID, request.From, request.To, request.TS = req.ID, req.From, req.To, req.TS
	if err := a.store.addOutbox(req, request, "", nil); err != nil {
		t.Fatal(err)
	}
	proposal := sealTo(t, b, a, envelope.Inner{Kind: envelope.KindAnswer, Status: envelope.StatusProposal, ReplyTo: req.ID, Body: "exact offered task"})
	if err := a.storeReceived(tctx(t), proposal); err != nil {
		t.Fatal(err)
	}
	if !a.CanConfirmProposal(proposal.ID) {
		t.Fatal("offline healthy proposal unavailable")
	}
	if err := a.store.endRecipient(b.Address, a.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	if !a.CanConfirmProposal(proposal.ID) {
		t.Fatal("wrong-key removal hid proposal")
	}
	if err := a.store.endRecipient(b.Address, b.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	if a.CanConfirmProposal(proposal.ID) {
		t.Fatal("removed exact host still offered confirmation")
	}
	if _, err := a.ConfirmProposal(tctx(t), proposal.ID); !errors.Is(err, ErrPeerRevoked) {
		t.Fatalf("revoked confirmation=%v", err)
	}
	var body, status string
	if err := a.store.db.QueryRow(`SELECT body,status FROM inbox WHERE id=?`, proposal.ID).Scan(&body, &status); err != nil || body != "exact offered task" || status != envelope.StatusProposal {
		t.Fatal("proposal evidence erased")
	}
}
