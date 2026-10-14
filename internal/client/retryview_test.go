package client

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

type retryReadCounter struct {
	dbq
	reads int
}

func (q *retryReadCounter) QueryRow(query string, args ...any) *sql.Row {
	q.reads++
	return q.dbq.QueryRow(query, args...)
}

// This deliberately inserts admitted-row fixtures: the read hint must share
// the exact stopped-parent check, without resolving a conversation or grants.
// Full admission/authority remains exercised by TestP6OriginAndTargetFences.
func TestAgentRetryBlockerReadsExactLocalAncestry(t *testing.T) {
	w := newWorld(t, "")
	a := w.alice
	conv, parent, child, source, target := strings.Repeat("a", 64), protocol.NewID(), protocol.NewID(), protocol.NewID(), protocol.NewID()
	fp := a.Self().Fingerprint()
	insert := func(id, pid, reply, state string, h *envelope.HumanTurn) {
		t.Helper()
		rawTarget, _ := json.Marshal(envelope.Target{Address: a.Address, Fingerprint: fp, AgentID: protocol.NewID()})
		var human any
		if h != nil {
			raw, _ := json.Marshal(h)
			human = string(raw)
		}
		if _, err := a.store.db.Exec(`INSERT INTO inbox(id,sender,ts,kind,body,received_at,verified_by,conv,lid,pid,target,human,reply_to,state) VALUES(?,?,1,'task','admitted fixture',1,?,?,?,?,?,?,?,?)`, id, a.Address, fp, conv, id, pid, string(rawTarget), human, reply, state); err != nil {
			t.Fatal(err)
		}
	}
	insert(parent, source, "", stateInterrupt, nil)
	insert(child, target, parent, stateInterrupt, &envelope.HumanTurn{AuthorPID: source, Proof: []protocol.ParticipationEvent{{Type: protocol.EventScope, PID: source, Audience: protocol.AudienceRoom}}})
	q := &retryReadCounter{dbq: a.store.db}
	why, err := agentRetryBlocker(q, conv, child, a.Address, fp)
	if err != nil || why != roomOriginStopped || q.reads != 2 {
		t.Fatalf("exact stopped parent: %q reads=%d %v", why, q.reads, err)
	}
	if _, err = a.store.db.Exec(`UPDATE inbox SET state=? WHERE id=?`, stateRunning, parent); err != nil {
		t.Fatal(err)
	}
	q.reads = 0
	why, err = agentRetryBlocker(q, conv, child, a.Address, fp)
	if err != nil || why != "" || q.reads != 2 {
		t.Fatalf("running parent incorrectly blocked: %q %d %v", why, q.reads, err)
	}
	if _, err = a.store.db.Exec(`UPDATE inbox SET state=? WHERE id=?`, stateResolved, parent); err != nil {
		t.Fatal(err)
	}
	why, err = a.AgentRetryBlocker(conv, child)
	if err != nil || why != roomOriginStopped {
		t.Fatalf("resolved parent: %q %v", why, err)
	}
	var state string
	if err = a.store.db.QueryRow(`SELECT state FROM inbox WHERE id=?`, child).Scan(&state); err != nil || state != stateInterrupt {
		t.Fatalf("read hint mutated child: %s %v", state, err)
	}
	if _, err = a.AgentRetryBlocker(strings.Repeat("b", 64), child); err == nil {
		t.Fatal("foreign conversation found cause")
	}
}
