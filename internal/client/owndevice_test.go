package client

import (
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
)

// Any current verified own human device runs tasks without a second approval;
// unrelated people still wait. Native harness arguments are unchanged.
func TestOwnTrustedDeviceTaskRunsAndIsClaimed(t *testing.T) {
	st := installStub(t, "answer")
	w := newWorld(t, "")
	runAgent(t, w.alice)
	runAgent(t, w.bob)
	persons(t, w.alice)
	setResponder(t, w.alice, "stub", st.dir, time.Minute)
	desk := linkedVia(t, w.alice, "desk", w.alice.ApproveNativeLink)
	task, err := desk.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Body: "tidy the build folder", Kind: envelope.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, w.alice, task.ID, stateAnswered)
	eventually(t, "the result on the desk", func() bool { r, ok := findReply(desk, task.ID); return ok && r.Kind == envelope.KindResult })

	phone := linkedVia(t, w.alice, "phone", approveLink(w.alice)) // ordinary human link, no native grant
	fromPhone, err := phone.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Body: "clear the downloads", Kind: envelope.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, w.alice, fromPhone.ID, stateAnswered)

	other, err := w.bob.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Body: "someone else's task", Kind: envelope.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, w.alice, other.ID, stateAwaiting)

	again, err := desk.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Body: "and the cache", Kind: envelope.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, w.alice, again.ID, stateAnswered)
	if st.count() != 3 {
		t.Fatalf("ran %d time(s)", st.count())
	}
}

// ownPendingTask has desk (a trusted own device of a's person) send a a
// task that stays pending to run as the person's own: a has no responder.
func ownPendingTask(t *testing.T, a, desk *Agent, body string) string {
	t.Helper()
	task, err := desk.SendMessage(tctx(t), Outgoing{To: a.Address, Body: body, Kind: envelope.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, a, task.ID, statePending)
	return task.ID
}

// Removing an obsolete native trust record does not change current human
// authority. Signed device removal, key changes and person loss do.
func TestOwnHumanTaskDoesNotDependOnNativeTrust(t *testing.T) {
	w := newWorld(t, "")
	runAgent(t, w.alice)
	persons(t, w.alice)
	phone := linkedVia(t, w.alice, "phone", approveLink(w.alice))
	id := ownPendingTask(t, w.alice, phone, "rotate the logs")
	if err := w.alice.store.setConfig(map[string]string{"self_consent": "[]"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := ownDeviceHolds(w.alice.store.db, phone.Address, phone.Self().Fingerprint()); err != nil || !ok {
		t.Fatalf("own authority %v %v", ok, err)
	}
	if ok, _ := ownDeviceHolds(w.alice.store.db, phone.Address, w.bob.Self().Fingerprint()); ok {
		t.Fatal("wrong key allowed")
	}
	if m := inboxRow(t, w.alice, id); m.State != statePending {
		t.Fatalf("lost pending task %+v", m)
	}
}

// A pending own-device task never stays pending where the worker no longer
// claims it (pending is no review state: nobody would see it). When the
// person removes the sending device from the roster, or this device leaves
// the person, it waits for the person's OK again once the step is pinned.
func TestOwnDeviceTaskWaitsAgainWhenNoLongerOwn(t *testing.T) {
	w := newWorld(t, "")
	runAgent(t, w.alice)
	persons(t, w.alice)
	desk := linkedVia(t, w.alice, "desk", w.alice.ApproveNativeLink)
	laptop := linkedVia(t, w.alice, "laptop", w.alice.ApproveNativeLink)

	removed := ownPendingTask(t, w.alice, desk, "rotate the logs")
	if err := w.alice.RemoveDevice(tctx(t), desk.Address); err != nil {
		t.Fatal(err)
	}
	if m := inboxRow(t, w.alice, removed); m.State != stateAwaiting || !strings.Contains(m.Detail, "no longer one of yours") {
		t.Fatalf("after the roster removal %+v", m)
	}

	left := ownPendingTask(t, w.alice, laptop, "update the notes")
	// The next independent removal must be based on the verified roster
	// containing the first one. Delivering a task does not wait for that
	// roster push; proposing from the older head correctly loses the CAS.
	current, ok, err := w.alice.Person()
	if err != nil || !ok {
		t.Fatalf("person after desk removal: %v %v", ok, err)
	}
	eventually(t, "laptop pins the exact desk removal", func() bool {
		p, ok, err := laptop.Person()
		if err != nil {
			t.Fatal(err)
		}
		return ok && p.Person == current.Person && p.Seq == current.Seq && p.Roster == current.Roster
	})
	if err := laptop.RemoveDevice(tctx(t), w.alice.Address); err != nil { // the person removes this device
		t.Fatal(err)
	}
	me, _, _ := laptop.Person()
	if _, err := w.alice.refreshPerson(tctx(t), me.Person, false); err != nil {
		t.Fatal(err)
	}
	if m := inboxRow(t, w.alice, left); m.State != stateAwaiting || !strings.Contains(m.Detail, "no longer one of your devices") {
		t.Fatalf("after leaving the person %+v", m)
	}
}
