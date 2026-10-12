package client

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Legacy native originals have no device_history_rows: deletion retains their
// immutable skeleton and exact tombstone, but deliberately removes the body.
func TestDeviceHistoryErasedNativeOriginal(t *testing.T) {
	w, phone, _, _ := historyCatchupFixture(t, 0)
	a := w.alice
	if err := a.store.pin(phone.Self()); err != nil {
		t.Fatal(err)
	}
	path, _ := writeFile(t, t.TempDir(), "erased-native.txt", 97)
	recipient, e := a.Self().Recipient()
	if e != nil {
		t.Fatal(e)
	}
	attachment, e := w.bob.spoolNamed(OutgoingFile{Path: path, Name: "erased-native.txt"}, recipient)
	if e != nil {
		t.Fatal(e)
	}
	original, e := envelope.Seal(envelope.Inner{ID: protocol.NewID(), From: w.bob.Address, To: a.Address, TS: time.Now().Unix(), Kind: envelope.KindQuestion, Body: "deleted native text", Attachments: []envelope.Attachment{attachment}}, w.bob.id.Sign, recipient)
	if e != nil {
		t.Fatal(e)
	}
	if err := a.verifyAndStore(tctx(t), original); err != nil {
		t.Fatal(err)
	}
	// A completed native task/question stays completed, never replayed.
	if _, err := a.store.db.Exec(`UPDATE inbox SET state=?,attempts=1 WHERE id=?`, stateAnswered, original.ID); err != nil {
		t.Fatal(err)
	}
	source, err := a.deviceHistorySource(a.store.db, "in", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	own, _, err := a.store.selfPerson(a.Address)
	if err != nil {
		t.Fatal(err)
	}
	carrier := func(row deviceHistoryRow) envelope.Envelope {
		t.Helper()
		item, _ := json.Marshal(row.item)
		return sealTo(t, phone, a, envelope.Inner{V: envelope.Version2, Kind: envelope.KindMessage, Sub: envelope.SubDeviceHistory, Replica: true, Body: mustJSON(protocol.DeviceHistory{V: 1, Person: own.info.Person, Roster: own.info.Roster, Recipient: row.to, RecipientKey: row.toKey, Item: item})})
	}
	altered := source
	altered.item.Body = "real non-erased conflict"
	conflict := carrier(altered)
	if err = a.verifyAndStore(tctx(t), conflict); err != nil {
		t.Fatal(err)
	}
	if got := heldReason(t, a, conflict.ID); got != reasonConflict {
		t.Fatalf("non-erased conflict: %s", got)
	}
	if _, err = a.DeleteThread(w.bob.Address, original.ID); err != nil {
		t.Fatal(err)
	}
	exact := carrier(source)
	if err = a.store.holdAs(exact, reasonConflict); err != nil {
		t.Fatal(err)
	}
	// Model the old installation's once-only upgrade scan, then actually reopen.
	if err = a.store.deleteConfig(erasedDeviceHistoryRecoveryScan); err != nil {
		t.Fatal(err)
	}
	home := a.home
	a.Close()
	a, err = Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	historyRecoveryReason(t, a, exact.ID, reasonProof)
	a.retryProof(tctx(t))
	if got, e := a.store.disposition(exact.ID); e != nil || got != protocol.StateDelivered {
		t.Fatalf("upgrade retry did not recover: %s %v", got, e)
	}
	for _, env := range []envelope.Envelope{exact, carrier(source), carrier(source)} {
		if err = a.verifyAndStore(tctx(t), env); err != nil {
			t.Fatal(err)
		}
		if got, e := a.store.disposition(env.ID); e != nil || got != protocol.StateDelivered {
			t.Fatalf("erased duplicate %s: %s / %s / %v", env.ID, got, heldReason(t, a, env.ID), e)
		}
	}
	var body, state string
	var replica, attempts, metadata int
	if err = a.store.db.QueryRow(`SELECT body,state,replica,attempts FROM inbox WHERE id=?`, original.ID).Scan(&body, &state, &replica, &attempts); err != nil {
		t.Fatal(err)
	}
	if err = a.store.db.QueryRow(`SELECT count(*) FROM device_history_rows WHERE id=?`, original.ID).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if body != "" || state != stateAnswered || replica != 0 || attempts != 1 || metadata != 0 {
		t.Fatalf("erased original changed: body=%q state=%q replica=%d attempts=%d metadata=%d", body, state, replica, attempts, metadata)
	}
	if _, err = a.Conversation(original.ID, 0, 0); err != ErrNoMessage {
		t.Fatalf("erased text visible: %v", err)
	}
	for _, change := range []string{"kind", "timestamp", "recipient", "key", "file"} {
		row := source
		switch change {
		case "kind":
			row.item.Kind = envelope.KindTask
		case "timestamp":
			row.item.TS++
		case "recipient":
			row.to = phone.Address
		case "key":
			row.toKey = phone.Self().Fingerprint()
		case "file":
			row.item.Attachments = append([]envelope.Attachment(nil), row.item.Attachments...)
			row.item.Attachments[0].Name = "different.txt"
		}
		env := carrier(row)
		if err = a.verifyAndStore(tctx(t), env); err != nil {
			t.Fatal(err)
		}
		if got := heldReason(t, a, env.ID); got != reasonConflict {
			t.Fatalf("erased %s conflict: %s", change, got)
		}
	}
	// A current freeze still prevents the inert acknowledgement; history never
	// grants authority merely because the original was once seen here.
	if _, err = a.store.db.Exec(`UPDATE persons SET state=? WHERE person=?`, personConflict, own.info.Person); err != nil {
		t.Fatal(err)
	}
	blocked := carrier(source)
	if err = a.verifyAndStore(tctx(t), blocked); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.disposition(blocked.ID); got == protocol.StateDelivered {
		t.Fatal("frozen person admitted erased carrier")
	}
}
