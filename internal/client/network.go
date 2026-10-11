package client

import (
	"sync"
	"time"
)

// NetworkChanged retires connections that belonged to the previous network and
// wakes the existing push run, including one waiting to reconnect. Platforms
// call this on a default-network change or resume after suspension. It changes
// no stored identity, messages or permissions; durable sends keep their IDs.
func (a *Agent) NetworkChanged() {
	a.hub.resetConnections()
	select {
	case a.networkWake <- struct{}{}:
	default: // one reconnect is already due
	}
}

// A watchdog may fire concurrently with the last read or stream cleanup. The
// small lock ensures cleanup joins that callback and a late read cannot rearm
// it against the next stream.
func (c *hubConn) streamWatchdog(generation uint64, wait time.Duration, cancel func()) (touch, stop func()) {
	var mu sync.Mutex
	ended := false
	timer := time.AfterFunc(wait, func() {
		mu.Lock()
		defer mu.Unlock()
		if ended {
			return
		}
		ended = true
		c.resetConnectionsAt(&generation)
		cancel()
	})
	return func() {
			mu.Lock()
			defer mu.Unlock()
			if !ended {
				timer.Reset(wait)
			}
		}, func() {
			mu.Lock()
			defer mu.Unlock()
			ended = true
			timer.Stop()
		}
}
