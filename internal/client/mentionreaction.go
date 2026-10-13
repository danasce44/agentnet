package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// SendAgentReaction is social feedback by an already admitted local agent.
// An ordinary canonical mention creates no request, worker or approval.
func (a *Agent) SendAgentReaction(ctx context.Context, pid string, ref ControlRef, emoji string, remove bool) (ControlSent, error) {
	p, err := a.Participation(pid)
	if err != nil {
		return ControlSent{}, err
	}
	if !p.HostHere || !p.Claimable() || p.Role != "" || ref.Conv != p.Conv {
		return ControlSent{}, errors.New("choose this host's active agent and an exact message in its chat")
	}
	if enabled, err := a.agentEnabled(p.AgentID); err != nil || !enabled {
		return ControlSent{}, errors.New("the admitted agent is not enabled")
	}
	in := envelope.Inner{Conv: p.Conv, PID: p.PID, AgentID: p.AgentID, Ref: &envelope.Ref{ID: ref.ID, Fingerprint: ref.Fingerprint}, Origin: envelope.OriginMentionReaction}
	if _, err := mentionReactionRequest(a.store.db, in, p, a.Self().Fingerprint()); err != nil {
		return ControlSent{}, err
	}
	var id, sender string
	err = a.store.db.QueryRow(`SELECT id,sender FROM inbox WHERE conv=? AND lid=? AND verified_by=? AND ref_id IS NULL AND local=0
 UNION ALL SELECT id,? FROM outbox WHERE conv=? AND lid=? AND ?=? AND ref_id IS NULL LIMIT 1`, ref.Conv, ref.ID, ref.Fingerprint, a.Address, ref.Conv, ref.ID, ref.Fingerprint, a.Self().Fingerprint()).Scan(&id, &sender)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ControlSent{}, err
		}
		h, carrier, snapshotErr := grantedAgentSnapshot(a.store.db, p, protocol.GrantRef{LID: ref.ID, Fingerprint: ref.Fingerprint})
		if snapshotErr != nil {
			return ControlSent{}, snapshotErr
		}
		id, sender = carrier, h.From
	}
	t := reactionTarget{ref: ref, conv: ref.Conv, pid: pid, lid: ref.ID, key: ref.Fingerprint, sender: sender}
	return a.sendAssistantReactionTarget(ctx, t, id, p.AgentID, p.PID, envelope.OriginMentionReaction, emoji, remove)
}

// Original signed fields decide visibility. Display topic moves cannot expand
// an invitation, and a label/plain @name cannot manufacture a canonical PID.
func mentionReactionRequest(q dbq, in envelope.Inner, p ParticipationInfo, selfFP string) (string, error) {
	if in.Ref == nil || in.Conv != p.Conv || in.PID != p.PID || in.AgentID != p.AgentID || p.Role != "" {
		return reasonInvalid, errors.New("reaction differs from the exact admitted agent")
	}
	var kind, body, topic, human string
	err := q.QueryRow(`SELECT kind,body,coalesce(topic,''),coalesce(human,'') FROM inbox WHERE conv=? AND lid=? AND coalesce(verified_by,claimed_fp,'')=? AND ref_id IS NULL AND coalesce(sub,'')='' AND local=0
 UNION ALL SELECT kind,body,coalesce(topic,''),coalesce(human,'') FROM outbox WHERE conv=? AND lid=? AND ?=? AND ref_id IS NULL AND coalesce(sub,'')='' LIMIT 1`, in.Conv, in.Ref.ID, in.Ref.Fingerprint, in.Conv, in.Ref.ID, in.Ref.Fingerprint, selfFP).Scan(&kind, &body, &topic, &human)
	if errors.Is(err, sql.ErrNoRows) {
		h, _, snapshotErr := grantedAgentSnapshot(q, p, protocol.GrantRef{LID: in.Ref.ID, Fingerprint: in.Ref.Fingerprint})
		if snapshotErr != nil {
			return reasonProof, errors.New("the exact mentioned message is not here yet")
		}
		kind, body, topic, human = h.Kind, h.Body, h.Topic, humanJSON(h.Human)
		err = nil
	}
	if err != nil {
		return "", err
	}
	if kind != envelope.KindMessage || !strings.Contains(body, "](agentnet:agent/"+p.PID+")") {
		return reasonInvalid, errors.New("choose an ordinary message with this exact canonical agent mention")
	}
	if p.Topic != nil && topic != *p.Topic {
		return reasonInvalid, errParticipationTopic
	}
	var captured *envelope.HumanTurn
	if human != "" {
		if err := json.Unmarshal([]byte(human), &captured); err != nil {
			return "", err
		}
	}
	visible := slices.Contains(p.Grant, protocol.GrantRef{LID: in.Ref.ID, Fingerprint: in.Ref.Fingerprint})
	if p.Member && captured != nil {
		visible = visible || slices.ContainsFunc(captured.Audience, func(s envelope.HumanScope) bool {
			return s.PID == p.PID && s.Invite == p.Invite && s.Decision == p.Decision
		})
	}
	if !visible {
		return reasonInvalid, errors.New("the original message was not admitted to this agent's scope")
	}
	if in.Human != nil {
		if captured == nil {
			return reasonInvalid, errors.New("the original message had no guest audience")
		}
		for _, scope := range in.Human.Audience {
			if !slices.Contains(captured.Audience, scope) {
				return reasonInvalid, errors.New("reaction audience exceeds the original message")
			}
		}
	}
	return "", nil
}

func (a *Agent) mentionReactionCurrent(q dbq, in envelope.Inner) error {
	if err := a.proactiveAuthority(q, in); err != nil {
		return err
	}
	if err := a.mentionReactionCurrentAgent(q, in.AgentID); err != nil {
		return err
	}
	m, err := membersIn(q, in.Conv)
	if err != nil {
		return err
	}
	p, err := participationIn(q, in.Conv, in.PID, m, a.Address)
	if err != nil {
		return err
	}
	_, err = mentionReactionRequest(q, in, p, a.Self().Fingerprint())
	return err
}

func (a *Agent) mentionReactionCurrentAgent(q dbq, agentID string) error {
	enabled := false
	if agentID == "" {
		responder, err := responderIn(q)
		if err != nil {
			return err
		}
		enabled = responder != nil
	} else {
		entries, err := a.localAgentsIn(q)
		if err != nil {
			return err
		}
		enabled = slices.ContainsFunc(entries, func(e LocalAgentInfo) bool { return e.Record.ID == agentID && e.Responder != nil })
	}
	if !enabled {
		return errors.New("the admitted agent is no longer enabled")
	}
	return nil
}
