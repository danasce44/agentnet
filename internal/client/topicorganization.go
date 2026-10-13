package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/misunders2d/agentnet/internal/envelope"
)

// Organization is a full member's human action, never a guest/agent scope
// operation. Received names or an origin label cannot establish this authority.
func topicOrganizationAuthor(q dbq, conv, address, fp string) error {
	m, err := membersIn(q, conv)
	if err != nil {
		return err
	}
	for _, p := range m.persons {
		if p.has(address, fp) && p.roster.Human(fp) {
			key, pinned, err := pinnedKey(q, address)
			if err != nil {
				return err
			}
			var pending int
			if err = q.QueryRow(`SELECT count(*) FROM peers WHERE address=? AND pending IS NOT NULL`, address).Scan(&pending); err != nil {
				return err
			}
			if pending != 0 || pinned && key.Fingerprint() != fp {
				break
			}
			if m.group != nil {
				return groupTurnCheck(q, *m.group, address, fp)
			}
			return nil
		}
	}
	return errors.New("only a current human device of a full member may organize topics")
}

func (a *Agent) prepareTopicOrganization(ctx context.Context, conv string, m *ConvOutgoing) error {
	if !envelope.TopicOrganization(m.TopicEvent) {
		return nil
	}
	in := envelope.Inner{V: envelope.Version2, Conv: conv, From: a.Address, Kind: envelope.KindMessage, Topic: m.Topic, TopicEvent: m.TopicEvent, TopicDone: m.TopicDone, PID: m.PID, Human: m.human, Target: m.Target, Origin: m.Origin, AgentID: m.AgentID, Followup: m.Followup}
	if err := envelope.CheckTopic(in); err != nil {
		return err
	}
	if envelope.AgentTopicOrganization(in) {
		r, err := a.organizationRun(a.store.db, m.agentOrganizationCause, m.agentOrganizationAttempt)
		if err != nil {
			return err
		}
		if r.p.Conv != conv || r.p.PID != m.PID || r.p.AgentID != m.AgentID || r.original.LID != m.TopicEvent.Request.LID || r.original.FromKey != m.TopicEvent.Request.Author || r.context.Hash != m.TopicEvent.Request.Hash {
			return errors.New("organization differs from current native human request")
		}
	}
	if _, err := a.topicOrganizationAdmission(a.store.db, in, a.Address, a.Self().Fingerprint(), false); err != nil {
		return err
	}
	// Capability belongs to each sealed copy, not to the author's operation.
	// Older readers retain their copy until the normal delivery gate opens.
	prior := m.claim
	m.claim = func(tx *sql.Tx, id string) error {
		if _, err := a.topicOrganizationAdmission(tx, in, a.Address, a.Self().Fingerprint(), false); err != nil {
			return err
		}
		if prior != nil {
			return prior(tx, id)
		}
		return nil
	}
	return nil
}

func topicOrganizationCopy(q dbq, id, sub, body string) (bool, error) {
	if sub == envelope.SubHistory {
		// Generic history copies intentionally keep only their ciphertext in
		// outbox.body. Organization copies retain the item for capability gates.
		if body == "" {
			return false, nil
		}
		var item HistoryItem
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			return false, err
		}
		return envelope.TopicOrganization(item.TopicEvent), nil
	}
	var raw string
	if err := q.QueryRow(`SELECT coalesce(topic_event,'') FROM outbox WHERE id=?`, id).Scan(&raw); err != nil {
		return false, err
	}
	if raw == "" {
		return false, nil
	}
	var event envelope.TopicEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return false, err
	}
	return envelope.TopicOrganization(&event), nil
}

// applyTopicOrganization is display-only. Resolve original placement first so
// unselected descendants and later replies never inherit a moved ancestor.
// A stale concurrent operation cannot overwrite an intervening assignment;
// signed causal order and stable ties produce the same result on each reader.
func applyTopicOrganization(msgs []ConvMessage, assigned map[string]string) map[string]string {
	by := map[string]string{}
	ambiguous := map[string]bool{}
	var events []ConvMessage
	for _, m := range msgs {
		if m.Sub != "" {
			continue
		}
		if envelope.TopicOrganization(m.TopicEvent) {
			events = append(events, m)
			continue
		}
		if m.TopicEvent != nil {
			continue
		}
		key := m.Key
		if key == "" {
			key = m.Claimed
		}
		if prev, ok := by[m.LID]; ok && prev != key {
			ambiguous[m.LID] = true
		}
		by[m.LID] = key
	}
	sortChatEvents(events)
	redirects := map[string]string{}
	for _, m := range events {
		e := m.TopicEvent
		if e.Merge != "" {
			seen := map[string]bool{e.Merge: true}
			to := m.Topic
			for to != "" && !seen[to] {
				seen[to] = true
				to = redirects[to]
			}
			if to != "" {
				continue
			} // no self-merge or redirect cycle
		}
		moved := false
		for _, ref := range e.Moves {
			if !ambiguous[ref.LID] && by[ref.LID] == ref.Author && assigned[ref.LID] == ref.Topic {
				assigned[ref.LID] = m.Topic
				moved = true
			}
		}
		if moved && e.Merge != "" {
			redirects[e.Merge] = m.Topic
		}
	}
	return redirects
}

func ChatTopicRedirects(msgs []ConvMessage) map[string]string {
	originals := make([]ConvMessage, 0, len(msgs))
	for _, m := range msgs {
		if !envelope.TopicOrganization(m.TopicEvent) {
			originals = append(originals, m)
		}
	}
	return applyTopicOrganization(msgs, ChatTopicAssignments(originals))
}
