package client

import (
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
)

func TestDeviceHistoryFollowupRequestDirections(t *testing.T) {
	w, laptop, _, _ := historyCatchupFixture(t, 0)
	a := w.alice
	stop := runAgent(t, a)
	reader, awaited, _ := linkPhone(t, a, "followup-reader")
	pending := pendingLink(t, a)
	stop()
	if err := a.DecideLink(tctx(t), pending.ID, true); err != nil {
		t.Fatal(err)
	}
	if r := <-awaited; r.err != nil {
		t.Fatal(r.err)
	}

	receive := func(kind, reply string) string {
		env := sealTo(t, laptop, a, envelope.Inner{Kind: kind, Body: "retained request", ReplyTo: reply})
		if err := a.verifyAndStore(tctx(t), env); err != nil {
			t.Fatal(err)
		}
		return env.ID
	}
	answer := func(reply string) string {
		sent, err := a.SendMessage(tctx(t), Outgoing{To: laptop.Address, Kind: envelope.KindAnswer, Body: "retained response", ReplyTo: reply})
		if err != nil {
			t.Fatal(err)
		}
		return sent.ID
	}
	first := receive(envelope.KindQuestion, "")
	response := answer(first)
	follow := receive(envelope.KindQuestion, response)
	final := answer(follow)
	task := receive(envelope.KindTask, final)
	peerRequest, err := a.SendMessage(tctx(t), Outgoing{To: laptop.Address, Kind: envelope.KindQuestion, Body: "peer counter-question", ReplyTo: task})
	if err != nil {
		t.Fatal(err)
	}
	peerTask, err := a.SendMessage(tctx(t), Outgoing{To: laptop.Address, Kind: envelope.KindTask, Body: "peer task", ReplyTo: peerRequest.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.deviceHistoryPage(reader.Self()); err != nil {
		t.Fatal(err)
	}
	if n := deliverDeviceHistory(t, a, reader, envelope.SubDeviceHistory); n != 7 {
		t.Fatalf("copies=%d", n)
	}
	for _, id := range []string{follow, task} {
		var direction, fromKey, recipientKey string
		if err := reader.store.db.QueryRow(`SELECT direction,author_fp,recipient_fp FROM device_history_rows WHERE id=?`, id).Scan(&direction, &fromKey, &recipientKey); err != nil || direction != "out" || fromKey != laptop.Self().Fingerprint() || recipientKey != a.Self().Fingerprint() {
			t.Fatalf("fresh copy direction/key changed: %s %s %v", id, direction, err)
		}
	}
	check := func() {
		t.Helper()
		thread, err := reader.Conversation(first, 0, 0)
		if err != nil || thread.Peer != a.Address || len(thread.Messages) != 7 {
			t.Fatalf("thread=%+v err=%v", thread, err)
		}
		expected := map[string]string{first: "out", response: "in", follow: "out", final: "in", task: "out", peerRequest.ID: "in", peerTask.ID: "in"}
		for _, m := range thread.Messages {
			if m.Dir != expected[m.ID] || (!m.History || m.State != "") {
				t.Fatalf("id=%s dir=%s history=%v state=%s", m.ID, m.Dir, m.History, m.State)
			}
			if m.From != laptop.Address && m.From != a.Address && m.From != reader.Address {
				t.Fatalf("original sender changed: %s", m.From)
			}
		}
	}
	check()
	// Reproduce the previously shipped saved projection. Reading corrects it
	// without mutating the immutable original or the retained mapping.
	if _, err := reader.store.db.Exec(`UPDATE device_history_rows SET direction='in' WHERE id IN (?,?)`, follow, task); err != nil {
		t.Fatal(err)
	}
	check()
	var saved string
	if err := reader.store.db.QueryRow(`SELECT direction FROM device_history_rows WHERE id=?`, follow).Scan(&saved); err != nil || saved != "in" {
		t.Fatalf("read rewrote saved data: %s %v", saved, err)
	}
}
