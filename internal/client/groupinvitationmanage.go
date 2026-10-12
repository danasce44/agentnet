package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/misunders2d/agentnet/internal/protocol"
)

// A mirror is evidence for an explicit human cancellation/refresh, never a
// runnable publication intent. The acting device must be a current own human.
func (a *Agent) groupInvitationManager(r groupInvitationRow) error {
	self, ok, err := a.store.selfPerson(a.Address)
	if err != nil {
		return err
	}
	fp := a.Self().Fingerprint()
	if !ok || self.info.State != personSelf || self.roster.Person != r.peerPerson || !self.has(a.Address, fp) || !self.roster.Human(fp) {
		return ErrGroupInvitationStale
	}
	return nil
}
func (a *Agent) managedGroupInvitation(id string) (groupInvitationRow, error) {
	r, err := groupInvitationIn(a.store.db, id, "out")
	if err == nil {
		return r, a.groupInvitationManager(r)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	rows, err := a.store.db.Query(`SELECT source,source_fp,payload FROM own_invitation_views WHERE id=? ORDER BY revision DESC`, id)
	if err != nil {
		return r, err
	}
	var candidates []groupInvitationRow
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&r.Inviter, &r.peerFP, &raw); err != nil {
			break
		}
		v, e := protocol.ParseInvitationSync(raw)
		if e != nil {
			err = e
			break
		}
		r.ID, r.Direction, r.State, r.Proposal, r.peerPerson = v.ID, "out", v.Status, v.Proposal, v.Person
		candidates = append(candidates, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return r, err
	}
	for _, r := range candidates {
		if a.groupInvitationManager(r) == nil {
			return r, nil
		}
	}
	return r, ErrNoMessage
}

// Recover the original binding from the retained invitation, including a
// cancelled one. Generic publication recovery cannot mislabel it governance.
const groupPublicationBindingSchema = `ALTER TABLE group_publications ADD COLUMN invitation TEXT;`

func (a *Agent) groupPublicationInvitation(c protocol.GroupCommit, packet GroupContext) (string, error) {
	rows, err := a.store.db.Query(`SELECT id,payload FROM group_invitations WHERE direction='out' AND conv=?`, c.Conv)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var matched string
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return "", err
		}
		var p protocol.GroupInvitation
		if err = json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		m, ok := packet.State.Member(p.Target)
		if p.Seq == c.Seq && p.Prev == c.Prev && ok && m.Admission.Seq == p.Seq && m.Admission.Prev == p.Prev && m.Admission.Roster == p.Roster {
			if matched != "" && matched != id {
				return "", errors.New("group: legacy publication has ambiguous invitation binding; retained for reconciliation")
			}
			matched = id
		}
	}
	return matched, rows.Err()
}
func (a *Agent) fenceGroupInvitation(ctx context.Context, r groupInvitationRow, current GroupContext) error {
	request := protocol.GroupInvitationCancel{V: 1, Realm: r.Proposal.Root.Realm, Conv: r.Proposal.Root.ID(), Bootstrap: r.Proposal.Root.Creator.Fingerprint, Person: r.peerPerson, Invitation: r.ID, Seq: r.Proposal.Seq, Prev: r.Proposal.Prev, Head: current.State.Hash()}
	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := a.hub.do(ctx, "POST", "/v1/groups/"+request.Conv+"/cancel-invitation?creator="+request.Bootstrap, request, &result); err != nil {
		return err
	}
	if !result.Cancelled {
		return errors.New("group: relay did not confirm exact invitation cancellation")
	}
	return nil
}
