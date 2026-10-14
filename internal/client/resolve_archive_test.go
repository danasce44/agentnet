package client

import (
	"slices"
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
)

// A successful follow-up is not a decision on the original blocked request.
// Exact human handling clears only that request and permits explicit archive.
func TestResolveHistoricalRequestArchiveExact(t *testing.T) {
	w := newWorld(t, "")
	runAgent(t, w.alice)
	runAgent(t, w.bob)
	send := func(body, reply string) string {
		t.Helper()
		r, err := w.alice.SendMessage(tctx(t), Outgoing{To: w.bob.Address, Kind: envelope.KindQuestion, Body: body, ReplyTo: reply})
		if err != nil {
			t.Fatal(err)
		}
		waitState(t, w.bob, r.ID, stateHeld)
		return r.ID
	}
	original := send("finish the report", "")
	if _, err := w.bob.store.db.Exec(`UPDATE inbox SET state=?,attempts=1,detail='native environment blocked' WHERE id=?`, stateNeedHuman, original); err != nil {
		t.Fatal(err)
	}
	followup := send("finish the same report", original)
	if _, err := w.bob.Reply(tctx(t), followup, "report finished"); err != nil {
		t.Fatal(err)
	}
	if state, _ := w.bob.store.jobState(original); state != stateNeedHuman {
		t.Fatalf("unrelated outcome settled original: %s", state)
	}
	if err := w.bob.ArchiveTopic(w.alice.Address, original); err != nil {
		t.Fatal(err)
	}
	if topic := topicOf(t, w.bob, original); !topic.Pending || topic.State == TopicArchived {
		t.Fatalf("archive hid pending original: %+v", topic)
	}
	if _, err := w.bob.ReopenTopic(w.alice.Address, original, 0); err != nil {
		t.Fatal(err)
	}
	pending := send("a separate open request", "")
	if err := w.bob.Resolve(original); err != nil {
		t.Fatal(err)
	}
	if state, _ := w.bob.store.jobState(pending); state != stateHeld {
		t.Fatalf("handling changed pending work: %s", state)
	}
	if review, err := w.bob.Review(); err != nil || !slices.ContainsFunc(review, func(m Message) bool { return m.ID == pending }) || slices.ContainsFunc(review, func(m Message) bool { return m.ID == original }) {
		t.Fatalf("exact review after handling: %+v %v", review, err)
	}
	var body, detail string
	var attempts int
	if err := w.bob.store.db.QueryRow(`SELECT body,detail,attempts FROM inbox WHERE id=?`, original).Scan(&body, &detail, &attempts); err != nil || body != "finish the report" || detail != "native environment blocked" || attempts != 1 {
		t.Fatalf("original audit changed: %s %s %d %v", body, detail, attempts, err)
	}
	if topic := topicOf(t, w.bob, original); topic.Pending || topic.State == TopicArchived {
		t.Fatalf("handling should permit, not perform archive: %+v", topic)
	}
	if err := w.bob.ArchiveTopic(w.alice.Address, original); err != nil {
		t.Fatalf("handled historical request blocks archive: %v", err)
	}
	if topic := topicOf(t, w.bob, original); topic.State != TopicArchived {
		t.Fatalf("explicit archive: %+v", topic)
	}
	if _, ok := findReply(w.alice, original); ok {
		t.Fatal("handling invented a reply")
	}
}
