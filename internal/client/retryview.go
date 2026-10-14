package client

import (
	"encoding/json"
)

// AgentRetryBlocker is a negative display hint for an own-host agent child's
// local retry. Executable host copies use their request LID as inbox ID
// (humansend.go). Read only that row and its exact parent by primary key:
// no history/membership scan, network or write transaction. A different-host
// or deeper ancestry still receives full current verification on Accept.
func (a *Agent) AgentRetryBlocker(conv, id string) (string, error) {
	return agentRetryBlocker(a.store.db, conv, id, a.Address, a.Self().Fingerprint())
}

func agentRetryBlocker(q querier, conv, id, self, fp string) (string, error) {
	read := func(id string) (roomCause, error) {
		var c roomCause
		var target, human string
		err := q.QueryRow(`SELECT lid,sender,verified_by,kind,coalesce(pid,''),coalesce(reply_to,''),state,target,coalesce(human,'') FROM inbox WHERE id=? AND conv=? AND verified_by IS NOT NULL AND replica=0 AND sub IS NULL AND kind IN ('question','task') AND target IS NOT NULL`, id, conv).Scan(&c.id, &c.from, &c.key, &c.kind, &c.pid, &c.reply, &c.state, &target, &human)
		if err != nil {
			return c, err
		}
		if err = json.Unmarshal([]byte(target), &c.target); err != nil {
			return c, err
		}
		if human != "" {
			err = json.Unmarshal([]byte(human), &c.human)
		}
		return c, err
	}
	c, err := read(id)
	if err != nil {
		return "", err
	}
	if c.from != self || c.key != fp || c.human == nil || !c.human.AgentAuthor() {
		return "", nil
	}
	cause, err := read(c.reply)
	if err != nil {
		return "", err
	}
	if cause.pid != c.human.AuthorPID || cause.target == nil || cause.target.Address != self || cause.target.Fingerprint != fp {
		return "agent ask is not caused by a request to that exact agent", nil
	}
	if stoppedLocalRoomCause(cause, c.id, self, false) {
		return roomOriginStopped, nil
	}
	return "", nil
}
