package client

import (
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestDeviceHistorySelfAgentRequestsStayOutgoing(t *testing.T) {
	w, phone, _, _ := historyCatchupFixture(t, 0)
	a := w.alice
	agentID := protocol.NewID()
	target := &envelope.Target{Address: a.Address, Fingerprint: a.Self().Fingerprint(), AgentID: agentID}
	file := envelope.Attachment{Name: "pasted-picture.png", Size: 391379, SHA256: strings.Repeat("a", 64), Blob: envelope.Blob{ID: protocol.NewID(), Size: 391500, SHA256: strings.Repeat("b", 64)}}
	var ids []string
	requests := map[string]bool{}
	for i, in := range []envelope.Inner{
		{Kind: envelope.KindQuestion, Body: "check this bug", Target: target},
		{Kind: envelope.KindAnswer, Body: "which bug?", AgentID: agentID},
		{Kind: envelope.KindQuestion, Body: "see screenshot", Target: target, Attachments: []envelope.Attachment{file}},
		{Kind: envelope.KindTask, Body: "fix it", Target: target},
	} {
		if i > 0 {
			in.ReplyTo = ids[i-1]
		}
		in.ID, in.From, in.To, in.TS = protocol.NewID(), a.Address, a.Address, time.Now().Unix()
		recipient, err := a.Self().Recipient()
		if err != nil {
			t.Fatal(err)
		}
		env, err := envelope.Seal(in, a.id.Sign, recipient)
		if err != nil {
			t.Fatal(err)
		}
		original, err := envelope.Open(env, a.id, a.Address, a.Self())
		if err != nil {
			t.Fatal(err)
		}
		// Same-device delivery retains both the signed send and received copy.
		// No worker runs: projecting history must not change execution state.
		if err = a.store.addOutbox(env, original, "", nil, boundOutgoing{fingerprint: a.Self().Fingerprint()}); err != nil {
			t.Fatal(err)
		}
		if err = a.store.addInbox(original, a.Self().Fingerprint()); err != nil {
			t.Fatal(err)
		}
		if _, err = a.store.db.Exec(`UPDATE outbox SET state='delivered' WHERE id=?`, env.ID); err != nil {
			t.Fatal(err)
		}
		if in.Kind == envelope.KindQuestion || in.Kind == envelope.KindTask {
			requests[env.ID] = true
			if _, err = a.store.db.Exec(`UPDATE inbox SET state='answered',attempts=1 WHERE id=?`, env.ID); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, env.ID)
	}
	check := func(reader *Agent, history bool) {
		t.Helper()
		thread, err := reader.Conversation(ids[0], 0, 0)
		if err != nil || thread.Peer != a.Address || len(thread.Messages) != len(ids) {
			t.Fatalf("self thread: %+v %v", thread, err)
		}
		for _, m := range thread.Messages {
			want := "out"
			if history && !requests[m.ID] {
				want = "in"
			}
			if m.Dir != want || m.History != history {
				t.Errorf("self message %s: dir=%s history=%v, want %s %v", m.Kind, m.Dir, m.History, want, history)
			}
			if requests[m.ID] && (m.Target == nil || m.Target.AgentID != agentID) {
				t.Errorf("lost selected agent: %+v", m.Target)
			}
			if m.ID == ids[2] && (len(m.Attachments) != 1 || m.Attachments[0].Name != file.Name || m.Attachments[0].SHA256 != file.SHA256) {
				t.Errorf("lost attachment: %+v", m.Attachments)
			}
			if history && m.State != "" {
				t.Errorf("history acquired execution state: %s", m.State)
			}
		}
	}
	check(a, false)
	if _, err := a.deviceHistoryPage(phone.Self()); err != nil {
		t.Fatal(err)
	}
	check(a, false) // History export must not relabel the native sent question.
	if n := deliverDeviceHistory(t, a, phone, envelope.SubDeviceHistory); n == 0 {
		t.Fatal("no self-thread history exported")
	}
	check(phone, true)
	for id := range requests {
		var direction string
		if err := phone.store.db.QueryRow(`SELECT direction FROM device_history_rows WHERE storage='in' AND id=?`, id).Scan(&direction); err != nil || direction != "out" {
			t.Fatalf("new retained request direction=%s: %v", direction, err)
		}
		if _, err := phone.store.db.Exec(`UPDATE device_history_rows SET direction='in' WHERE storage='in' AND id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	check(phone, true) // Repair presentation of old metadata without replaying it.
	for id := range requests {
		var direction string
		var attempts int
		if err := phone.store.db.QueryRow(`SELECT direction FROM device_history_rows WHERE storage='in' AND id=?`, id).Scan(&direction); err != nil || direction != "in" {
			t.Fatalf("read rewrote retained metadata: %s %v", direction, err)
		}
		if err := a.store.db.QueryRow(`SELECT attempts FROM inbox WHERE id=?`, id).Scan(&attempts); err != nil || attempts != 1 {
			t.Fatalf("original execution changed: %d %v", attempts, err)
		}
	}
}
