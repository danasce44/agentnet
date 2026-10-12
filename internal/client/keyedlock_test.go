package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func lockRefs(locks *keyedLocks, id string) (entries, refs int) {
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if entry := locks.entries[id]; entry != nil {
		refs = entry.refs
	}
	return len(locks.entries), refs
}

func assertLocksDrained(t *testing.T, locks *keyedLocks) {
	t.Helper()
	if n, _ := lockRefs(locks, ""); n != 0 {
		t.Fatalf("retained %d inactive keyed locks", n)
	}
}

func TestKeyedLocksWaitCancelAndIndependentIDs(t *testing.T) {
	var locks keyedLocks
	ctx := tctx(t)
	unlock, err := locks.acquire(ctx, "same")
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		release, err := locks.acquire(cancelCtx, "same")
		if err == nil {
			release()
		}
		done <- err
	}()
	eventually(t, "waiter registered", func() bool { _, n := lockRefs(&locks, "same"); return n == 2 })
	// Another message can progress even while this ID is held and contended.
	other, err := locks.acquire(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	other()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v", err)
	}
	if entries, refs := lockRefs(&locks, "same"); entries != 1 || refs != 1 {
		t.Fatalf("cancel removed holder or leaked waiter: entries=%d refs=%d", entries, refs)
	}
	unlock()
	assertLocksDrained(t, &locks)
	if release, err := locks.acquire(cancelCtx, "same"); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled acquisition: %v", err)
	}
	assertLocksDrained(t, &locks)
}

func TestKeyedLocksContendedLifetime(t *testing.T) {
	var locks keyedLocks
	var inside, overlap atomic.Int32
	var wg sync.WaitGroup
	ctx := tctx(t)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				unlock, err := locks.acquire(ctx, "same")
				if err != nil {
					t.Error(err)
					return
				}
				if inside.Add(1) != 1 {
					overlap.Add(1)
				}
				inside.Add(-1)
				unlock()
			}
		}()
	}
	wg.Wait()
	if overlap.Load() != 0 {
		t.Fatal("same ID admitted simultaneous holders")
	}
	assertLocksDrained(t, &locks)
}

func TestDeliveryLocksDrainAfterMessages(t *testing.T) {
	w := newWorld(t, "")
	for range 24 {
		if _, err := w.alice.Send(tctx(t), w.bob.Address, "lock lifetime", ""); err != nil {
			t.Fatal(err)
		}
	}
	assertLocksDrained(t, &w.alice.deliveryLocks)
}
