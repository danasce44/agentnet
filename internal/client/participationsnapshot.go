package client

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func grantSnapshot(ref protocol.GrantRef, snapshots []protocol.GrantSnapshot) (protocol.GrantSnapshot, bool) {
	for _, s := range snapshots {
		if s.LID == ref.LID && s.Fingerprint == ref.Fingerprint {
			return s, true
		}
	}
	return protocol.GrantSnapshot{}, false
}

func snapshotRequest(conv string, topic *string, refs []protocol.GrantRef) TopicOrganizationRequest {
	r := TopicOrganizationRequest{Conv: conv}
	if topic != nil {
		r.Topic, r.Main = *topic, *topic == ""
	}
	for _, ref := range refs {
		r.IDs = append(r.IDs, ref.LID)
	}
	return r
}

func (a *Agent) snapshotSourceItem(q dbq, conv string, ref protocol.GrantRef) (HistoryItem, error) {
	rows, err := a.historySourceRows(q, "conv=? AND lid=? AND coalesce(sub,'')=''", "dir,id", 3, conv, ref.LID)
	if err != nil {
		return HistoryItem{}, err
	}
	var selected *historySourceRow
	for i := range rows {
		if rows[i].key != ref.Fingerprint {
			continue
		}
		if selected != nil {
			return HistoryItem{}, errors.New("selected history source is ambiguous")
		}
		selected = &rows[i]
	}
	if selected == nil {
		return HistoryItem{}, sql.ErrNoRows
	}
	return a.historySourceItem(q, *selected)
}

// Only an explicit new disclosure captures display placement. The transaction
// stamp binds the selected originals/controls and current organization/audience.
func (a *Agent) prepareGrantSnapshots(conv string, topic *string, refs []protocol.GrantRef) ([]protocol.GrantSnapshot, func(*sql.Tx) error, error) {
	if topic == nil || len(refs) == 0 {
		return nil, nil, nil
	}
	request := snapshotRequest(conv, topic, refs)
	before, err := a.topicOrganizationStamp(a.store.db, request)
	if err != nil {
		return nil, nil, err
	}
	msgs, err := a.ConversationMessages(conv)
	if err != nil {
		return nil, nil, err
	}
	assigned := ChatTopicAssignments(msgs)
	var snapshots []protocol.GrantSnapshot
	moved := false
	for _, ref := range refs {
		var msg *ConvMessage
		for i := range msgs {
			m := &msgs[i]
			if m.LID == ref.LID && m.Key == ref.Fingerprint && m.Sub == "" && m.ExcerptPID == "" && !m.Deleted {
				msg = m
				break
			}
		}
		if msg == nil || assigned[ref.LID] != *topic {
			return nil, nil, errParticipationTopic
		}
		original, e := topicReference(a.store.db, conv, ref.LID, ref.Fingerprint)
		if e != nil {
			return nil, nil, e
		}
		moved = moved || original != *topic
		item, e := a.snapshotSourceItem(a.store.db, conv, ref)
		if e != nil {
			return nil, nil, e
		}
		item.Body = msg.Controls.Shown(msg.Body)
		snapshots = append(snapshots, protocol.GrantSnapshot{LID: ref.LID, Fingerprint: ref.Fingerprint, Hash: contentHash(item.inner(conv))})
	}
	if !moved {
		return nil, nil, nil
	}
	if err := topicOrganizationAuthor(a.store.db, conv, a.Address, a.Self().Fingerprint()); err != nil {
		return nil, nil, err
	}
	after, err := a.topicOrganizationStamp(a.store.db, request)
	if err != nil || after != before {
		return nil, nil, ErrTopicOrganizationChanged
	}
	claim := func(tx *sql.Tx) error {
		if err := topicOrganizationAuthor(tx, conv, a.Address, a.Self().Fingerprint()); err != nil {
			return err
		}
		stamp, e := a.topicOrganizationStamp(tx, request)
		if e != nil || stamp != before {
			return ErrTopicOrganizationChanged
		}
		return nil
	}
	return snapshots, claim, nil
}

// The new signature authorizes an exact inert snapshot, not an original-topic
// stream. Missing originals are normal; a locally held conflicting source waits
// for a fresh review instead of silently substituting text or file manifests.
func (a *Agent) snapshotOriginalGuard(in envelope.Inner, p ParticipationInfo) (func(*sql.Tx) error, error) {
	if in.Sub != envelope.SubExcerpt || len(p.Snapshots) == 0 {
		return nil, nil
	}
	h, err := parseGrantedExcerpt(in, p)
	if err != nil {
		return nil, err
	}
	ref := protocol.GrantRef{LID: h.LID, Fingerprint: h.FromKey}
	local, err := a.snapshotSourceItem(a.store.db, in.Conv, ref)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		controls, e := a.controlsOf(ControlRef{Conv: in.Conv, ID: h.LID, Fingerprint: h.FromKey})
		if e != nil {
			return nil, e
		}
		local.Body = controls.Shown(local.Body)
		if controls.Deleted || contentHash(local.inner(in.Conv)) != contentHash(h.inner(in.Conv)) {
			return nil, errors.New("snapshot conflicts with the local selected original")
		}
	}
	r := snapshotRequest(in.Conv, p.Topic, []protocol.GrantRef{ref})
	stamp, err := a.topicOrganizationStamp(a.store.db, r)
	if err != nil {
		return nil, err
	}
	return func(tx *sql.Tx) error {
		got, e := a.topicOrganizationStamp(tx, r)
		if e != nil || got != stamp {
			return errors.New("selected original changed during snapshot admission")
		}
		return nil
	}, nil
}

// Inspect the actual carried events; public scope records carry no private
// selection. A stored requirement also survives local body erasure/repacking.
func snapshotCopy(q dbq, conv, pid, sub, body, human string) (bool, error) {
	if sub == envelope.SubHistory {
		if body == "" {
			return false, nil
		}
		var h HistoryItem
		if err := json.Unmarshal([]byte(body), &h); err != nil {
			return false, err
		}
		if h.GroupHistory != nil {
			for _, e := range h.GroupHistory.Memberships {
				if len(e.Snapshots) > 0 {
					return true, nil
				}
			}
		}
		pid, sub, body, human = h.PID, h.Sub, h.Body, humanJSON(h.Human)
	}
	if human != "" {
		var h envelope.HumanTurn
		if err := json.Unmarshal([]byte(human), &h); err != nil {
			return false, err
		}
		for _, e := range h.Proof {
			if len(e.Snapshots) > 0 {
				return true, nil
			}
		}
	}
	if sub == envelope.SubEvent {
		var e protocol.ParticipationEvent
		err := json.Unmarshal([]byte(body), &e)
		return len(e.Snapshots) > 0, err
	}
	if sub == envelope.SubGroupContext {
		var packet GroupContext
		if json.Unmarshal([]byte(body), &packet) == nil {
			for _, e := range packet.Memberships {
				if len(e.Snapshots) > 0 {
					return true, nil
				}
			}
		}
		return false, nil
	}
	if sub != envelope.SubExcerpt || conv == "" || pid == "" || q == nil {
		return false, nil
	}
	var n int
	err := q.QueryRow(`SELECT count(*) FROM participation_events WHERE conv=? AND pid=? AND json_array_length(event,'$.snapshots')>0`, conv, pid).Scan(&n)
	return n > 0, err
}

func mergeGrantSnapshots(p *ParticipationInfo, snapshots []protocol.GrantSnapshot) {
	for _, s := range snapshots {
		if old, ok := grantSnapshot(protocol.GrantRef{LID: s.LID, Fingerprint: s.Fingerprint}, p.Snapshots); ok && old.Hash != s.Hash {
			p.Held++
			continue
		}
		if !slices.Contains(p.Snapshots, s) {
			p.Snapshots = append(p.Snapshots, s)
		}
	}
}

func (a *Agent) snapshotContextMessages(p ParticipationInfo) ([]ConvMessage, error) {
	if !p.HostHere || p.Host.Address != a.Address {
		return nil, nil
	}
	var messages []ConvMessage
	for _, s := range p.Snapshots {
		ref := protocol.GrantRef{LID: s.LID, Fingerprint: s.Fingerprint}
		h, id, err := grantedAgentSnapshot(a.store.db, p, ref)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var local int
		if err := a.store.db.QueryRow(`SELECT count(*) FROM outbox WHERE id=? AND recipient=?`, id, a.Address).Scan(&local); err != nil {
			return nil, err
		}
		if local == 0 {
			continue
		} // received excerpts are already in the view
		files, err := a.store.sentAttachments(id)
		if err != nil {
			return nil, err
		}
		a.markOpenable(files, true)
		messages = append(messages, ConvMessage{ID: id, Dir: "out", LID: h.LID, From: h.From, At: h.TS, Body: h.Body, Kind: h.Kind, Topic: h.Topic, ReplyTo: h.ReplyTo, Origin: h.Origin, PID: h.PID, AgentID: h.AgentID, Claimed: h.FromKey, SyncedFrom: a.Address, ExcerptPID: p.PID, History: true, Replica: true, Attachments: files})
	}
	return messages, nil
}
