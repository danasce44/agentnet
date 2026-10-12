package client

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/misunders2d/agentnet/internal/envelope"
)

// StopAction names the exact remote attempt. Pending is only a queued decision;
// Result is the host's authenticated response, never transport success.
type StopAction struct {
	ContinuationAction
	Pending bool            `json:"pending,omitempty"`
	Result  *DecisionResult `json:"result,omitempty"`
}

func (a *Agent) StopFor(id, key string, exec *ExecView) (*StopAction, error) {
	r, err := a.remoteRequestFor(id, key, exec, stateRunning)
	if err != nil || r == nil {
		return nil, err
	}
	s := &StopAction{ContinuationAction: *r}
	var decision, state string
	err = a.store.db.QueryRow(`SELECT id,state FROM outbox WHERE recipient=? AND sub=? AND ref_id=? AND ref_fp=? AND json_extract(body,'$.action')='cancel' AND coalesce(json_extract(body,'$.report'),'')='' AND json_extract(body,'$.attempt')=? ORDER BY created_ms DESC,id DESC LIMIT 1`, r.Host, envelope.SubDecision, r.ID, r.Key, r.Attempt).Scan(&decision, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.Pending = state != stateFailed
	var body string
	err = a.store.db.QueryRow(`SELECT body FROM inbox WHERE sender=? AND sub=? AND ref_id=? AND ref_fp=? AND json_extract(body,'$.decision')=? ORDER BY received_ms DESC,id DESC LIMIT 1`, r.Host, envelope.SubStatus, r.ID, r.Key, decision).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var status envelope.Status
	if err = json.Unmarshal([]byte(body), &status); err != nil {
		return nil, err
	}
	s.Pending = false
	s.Result = &DecisionResult{Decision: decision, State: status.State, Refused: status.Refused, At: status.At}
	return s, nil
}
