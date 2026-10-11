package client

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestRunOutputWakeDoesNotRecheckStoredAuthority(t *testing.T) {
	w := newWorld(t, "")
	for _, reason := range []string{"cancel", "revoke"} {
		t.Run(reason, func(t *testing.T) {
			id := protocol.NewID()
			if _, err := w.bob.store.db.Exec(`INSERT INTO inbox(id,sender,ts,kind,body,received_at,state,verified_by) VALUES(?,?,?,?,?,?,?,?)`, id, w.alice.Address, 1, envelope.KindQuestion, "PRIVATE", 1, stateRunning, w.alice.Self().Fingerprint()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(tctx(t))
			defer cancel()
			activity := newRunActivity()
			checked, done := make(chan struct{}, 16), make(chan struct{})
			var calls atomic.Int32
			go func() {
				defer close(done)
				w.bob.watchRunActivity(ctx, ctx, job{ID: id}, activity, func() bool {
					calls.Add(1)
					state, err := w.bob.store.jobState(id)
					revoked, e := revokedDeviceKnown(w.bob.store.db, w.alice.Address, w.alice.Self().Fingerprint())
					checked <- struct{}{}
					return err == nil && e == nil && state != stateCancelReq && !revoked
				})
			}()
			defer func() { cancel(); <-done }()
			select {
			case <-checked:
			case <-ctx.Done():
				t.Fatal("initial authority check missing")
			}
			before := calls.Load()
			for i := 0; i < 10; i++ {
				activity.touch(time.Now())
				time.Sleep(10 * time.Millisecond)
			}
			time.Sleep(75 * time.Millisecond)
			if calls.Load() != before {
				t.Fatalf("output alone queried stored authority %d extra times", calls.Load()-before)
			}
			if reason == "cancel" {
				if _, err := w.bob.store.db.Exec(`UPDATE inbox SET state=? WHERE id=?`, stateCancelReq, id); err != nil {
					t.Fatal(err)
				}
				w.bob.wakeWorker() // the existing cross-process cancellation wake
			} else {
				if _, err := w.bob.store.db.Exec(`INSERT INTO config(k,v) VALUES(?,?)`, revokedDevicePrefix+w.alice.Address+"/"+w.alice.Self().Fingerprint(), w.alice.Address); err != nil {
					t.Fatal(err)
				}
				w.bob.NoteChange()
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("stored cancellation/revocation did not stop watcher")
			}
			if calls.Load() <= before {
				t.Fatal("real authority change omitted stored check")
			}
		})
	}
}
