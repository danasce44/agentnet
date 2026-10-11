package client

import (
	"encoding/json"
	"github.com/misunders2d/agentnet/internal/envelope"
	"testing"
)

func TestTopicPermanentlyUndeliveredRequestDoesNotWait(t *testing.T) {
	request := threadRow{link: link{id: "original"}, kind: envelope.KindQuestion, state: "custody"}
	if pending := pendingTopicRows([]threadRow{request}); len(pending) != 1 {
		t.Fatal("custody must remain pending for a possible accepted answer")
	}
	request.state = stateNotDelivered
	if pending := pendingTopicRows([]threadRow{request}); len(pending) != 0 {
		t.Fatal("permanently unsent request kept topic pending")
	}
	if request.id != "original" || request.kind != envelope.KindQuestion {
		t.Fatal("original evidence changed")
	}
	// Authoritative late admission/reply may still be retained. Transport
	// terminalization neither invents completion nor removes the original.
	request.state = "custody"
	answer := threadRow{link: link{id: "answer", replyTo: request.id}, in: true, kind: envelope.KindAnswer, status: envelope.StatusDone}
	rows := []threadRow{request, answer}
	if pending := pendingTopicRows(rows); len(pending) != 0 {
		t.Fatal("accepted terminal reply failed to settle topic")
	}
	if len(rows) != 2 || rows[0].id != "original" || rows[1].id != "answer" {
		t.Fatal("late answer lost retained history")
	}
}

func TestRevokedExecProjectionDoesNotClaimCompletion(t *testing.T) {
	w := newWorld(t, "")
	a, b := w.alice, w.bob
	original := ExecView{Host: b.Address, State: stateNeedHuman, Attempt: 1}
	view := original
	if err := a.projectRevokedExec(&view, b.Self().Fingerprint()); err != nil || view.State != stateNeedHuman {
		t.Fatal("offline host was ended")
	}
	if err := a.store.endRecipient(b.Address, a.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	view = original
	if err := a.projectRevokedExec(&view, b.Self().Fingerprint()); err != nil || view.State != stateNeedHuman {
		t.Fatal("wrong-key marker ended execution")
	}
	if err := a.store.endRecipient(b.Address, b.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	view = original
	if err := a.projectRevokedExec(&view, b.Self().Fingerprint()); err != nil || view.State != "unavailable" || view.Detail == "" {
		t.Fatalf("removed host remains actionable: %+v %v", view, err)
	}
	view = original
	view.State = envelope.StatusInterrupted
	if err := a.projectRevokedExec(&view, b.Self().Fingerprint()); err != nil || view.State != "unavailable" {
		t.Fatal("removed interrupted host remained pending")
	}
	if original.State != stateNeedHuman {
		t.Fatal("saved execution state changed")
	}
	view = original
	view.settle(false, envelope.StatusDone, 2, 3)
	if err := a.projectRevokedExec(&view, b.Self().Fingerprint()); err != nil || view.State != envelope.StatusDone {
		t.Fatal("proven late answer hidden")
	}
}

func TestExecRevocationUsesExactWinningStatusKey(t *testing.T) {
	ref := ControlRef{ID: "request", Fingerprint: "requester"}
	status := func(n int64) string {
		b, _ := json.Marshal(envelope.Status{N: n, State: stateNeedHuman, At: 1, Attempt: 1})
		return string(b)
	}
	rows := []controlRow{
		{id: "old", sub: envelope.SubStatus, refID: ref.ID, refFP: ref.Fingerprint, author: "host/device", authorFP: "old-key", body: status(1)},
		{id: "current", sub: envelope.SubStatus, refID: ref.ID, refFP: ref.Fingerprint, author: "host/device", authorFP: "current-key", body: status(2)},
	}
	if got := execViews(rows)[ref].sourceKey; got != "current-key" {
		t.Fatalf("revocation rebound winning status: %s", got)
	}
	if got := execViews(rows)[ref]; got.State != stateNeedHuman || got.Host != "host/device" {
		t.Fatal("status selection changed")
	}
}
