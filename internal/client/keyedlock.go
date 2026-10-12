package client

import (
	"context"
	"sync"
)

// keyedLocks serializes work for an exact ID, retaining entries only while
// holders or waiters exist. Separate registries keep unrelated work independent.
// Its zero value is ready to use; the registry mutex never covers I/O or waiting.
type keyedLocks struct {
	mu      sync.Mutex
	entries map[string]*keyedLock
}

type keyedLock struct {
	held chan struct{}
	refs int // holder plus waiters, registered before waiting
}

func (locks *keyedLocks) acquire(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	locks.mu.Lock()
	if locks.entries == nil {
		locks.entries = make(map[string]*keyedLock)
	}
	entry := locks.entries[id]
	if entry == nil {
		entry = &keyedLock{held: make(chan struct{}, 1)}
		locks.entries[id] = entry
	}
	entry.refs++
	locks.mu.Unlock()
	drop := func() {
		locks.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(locks.entries, id)
		}
		locks.mu.Unlock()
	}
	select {
	case entry.held <- struct{}{}:
		unlock := func() { <-entry.held; drop() }
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}
