package client

import (
	"database/sql"
	"errors"
	"slices"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// An outside host holds only the inviter's already-admitted exact snapshot.
// This does not turn that claimed attribution into an original author proof.
func grantedAgentSnapshot(q dbq, p ParticipationInfo, ref protocol.GrantRef) (HistoryItem, string, error) {
	if !slices.Contains(p.Grant, ref) {
		return HistoryItem{}, "", sql.ErrNoRows
	}
	var body, id, sender, key string
	err := q.QueryRow(`SELECT body,id,sender,verified_by FROM inbox WHERE conv=? AND pid=? AND sub=? AND replica=1 AND json_extract(body,'$.lid')=? AND json_extract(body,'$.from_key')=? ORDER BY received_ms,id LIMIT 1`, p.Conv, p.PID, envelope.SubExcerpt, ref.LID, ref.Fingerprint).Scan(&body, &id, &sender, &key)
	if errors.Is(err, sql.ErrNoRows) && p.HostHere && len(p.Snapshots) > 0 {
		err = q.QueryRow(`SELECT body,id,recipient,recipient_fp FROM outbox WHERE conv=? AND pid=? AND sub=? AND recipient=? AND json_extract(body,'$.lid')=? AND json_extract(body,'$.from_key')=? ORDER BY rowid LIMIT 1`, p.Conv, p.PID, envelope.SubExcerpt, p.Host.Address, ref.LID, ref.Fingerprint).Scan(&body, &id, &sender, &key)
	}
	if err != nil {
		return HistoryItem{}, "", err
	}
	if !slices.ContainsFunc(p.Inviters, func(person PersonInfo) bool { return person.Address == sender && person.Fingerprint == key }) {
		return HistoryItem{}, "", errors.New("snapshot is not from the exact verified inviter")
	}
	h, err := parseGrantedExcerpt(envelope.Inner{Conv: p.Conv, Body: body, Sub: envelope.SubExcerpt, PID: p.PID, Replica: true}, p)
	return h, id, err
}
