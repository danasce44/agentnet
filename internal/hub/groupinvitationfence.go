package hub

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"

	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Routing metadata only. Cancellation does not advance the group head and
// cannot invalidate a different proposal on that same head.
const groupInvitationFenceSchema = `CREATE TABLE group_invitation_fences(
 conv TEXT NOT NULL, bootstrap TEXT NOT NULL, person TEXT NOT NULL, id TEXT NOT NULL,
 seq INTEGER NOT NULL, prev TEXT NOT NULL, outcome TEXT NOT NULL CHECK(outcome IN ('cancelled','published')),
 hash TEXT NOT NULL, PRIMARY KEY(conv,bootstrap,person,id));
ALTER TABLE group_journal ADD COLUMN invitation TEXT;`

var errGroupPublicationRequired = errors.New("group: update AgentNet to publish with an exact invitation binding")
var errGroupInvitationCancelled = errors.New("group: this exact invitation was cancelled by your person")
var errGroupInvitationPublished = errors.New("group: publication already committed; inspect current membership")

type groupInvitationFence struct {
	seq                 int64
	prev, outcome, hash string
}

func groupInvitationFenceIn(q querier, conv, bootstrap, person, id string) (groupInvitationFence, bool, error) {
	var f groupInvitationFence
	err := q.QueryRow(`SELECT seq,prev,outcome,hash FROM group_invitation_fences WHERE conv=? AND bootstrap=? AND person=? AND id=?`, conv, bootstrap, person, id).Scan(&f.seq, &f.prev, &f.outcome, &f.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	return f, err == nil, err
}

func (s *store) cancelGroupInvitation(caller identity.Public, c protocol.GroupInvitationCancel, realm string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Realm != realm {
		return errGroupRealm
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	me, roster, err := groupPersonIn(tx, caller.Address)
	if err != nil {
		return err
	}
	if me.Public.Fingerprint() != caller.Fingerprint() || me.Person != c.Person || !roster.Human(caller.Fingerprint()) {
		return errGroupForbidden
	}
	head, found, err := groupHeadIn(tx, c.Conv, c.Bootstrap)
	if err != nil {
		return err
	}
	if !found {
		return errNotFound
	}
	if head.realm != realm {
		return errGroupRealm
	}
	// An established outcome can be read again without requiring the old head
	// or granting any new authority after administrator removal.
	f, found, err := groupInvitationFenceIn(tx, c.Conv, c.Bootstrap, c.Person, c.Invitation)
	if err != nil {
		return err
	}
	if found {
		if f.seq != c.Seq || f.prev != c.Prev {
			return errGroupForbidden
		}
		if f.outcome == "cancelled" {
			return nil
		}
		return errGroupInvitationPublished
	}
	if !slices.Contains(head.admins, me.Person) {
		return errGroupForbidden
	}
	if head.hash != c.Head {
		return errGroupStale
	}
	// A legacy committed slot has no ID binding. Never declare that invitation
	// cancelled on the strength of an absence lookup or a changed local status.
	var occupied sql.NullString
	err = tx.QueryRow(`SELECT invitation FROM group_journal WHERE conv=? AND bootstrap=? AND seq=?`, c.Conv, c.Bootstrap, c.Seq).Scan(&occupied)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if !occupied.Valid || occupied.String == c.Invitation {
			return errGroupInvitationPublished
		}
		// Another known publication already won this slot. The old proposal
		// cannot append; close only its ID without changing the current head.
		var previous string
		if err = tx.QueryRow(`SELECT hash FROM group_journal WHERE conv=? AND bootstrap=? AND seq=?`, c.Conv, c.Bootstrap, c.Seq-1).Scan(&previous); err != nil {
			return err
		}
		if previous != c.Prev {
			return errGroupStale
		}
	} else if c.Seq != head.seq+1 || c.Prev != head.hash {
		return errGroupStale
	}
	if _, err = tx.Exec(`INSERT INTO group_invitation_fences(conv,bootstrap,person,id,seq,prev,outcome,hash) VALUES(?,?,?,?,?,?,'cancelled','')`, c.Conv, c.Bootstrap, c.Person, c.Invitation, c.Seq, c.Prev); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *Hub) handleGroupPublication(w http.ResponseWriter, r *http.Request) {
	caller, body, ok := h.authenticateBody(w, r)
	if !ok {
		return
	}
	var p protocol.GroupPublication
	if decodeStrict(body, &p) != nil || p.Validate() != nil || p.Commit.Conv != r.PathValue("id") || p.Commit.Bootstrap != r.URL.Query().Get("creator") {
		writeError(w, 400, "", "invalid group publication")
		return
	}
	me, err := h.store.agent(caller)
	if err != nil {
		writeError(w, 500, "", "storage error")
		return
	}
	result, err := h.store.putGroupCommit(me.Public, p.Commit, h.RealmID(), *p.Invitation)
	if err != nil {
		writeGroupFenceError(w, err)
		return
	}
	h.streams.notifyAll()
	writeJSON(w, 200, result)
}
func (h *Hub) handleGroupInvitationCancel(w http.ResponseWriter, r *http.Request) {
	caller, body, ok := h.authenticateBody(w, r)
	if !ok {
		return
	}
	var c protocol.GroupInvitationCancel
	if decodeStrict(body, &c) != nil || c.Validate() != nil || c.Conv != r.PathValue("id") || c.Bootstrap != r.URL.Query().Get("creator") {
		writeError(w, 400, "", "invalid group invitation cancellation")
		return
	}
	me, err := h.store.agent(caller)
	if err != nil {
		writeError(w, 500, "", "storage error")
		return
	}
	if err = h.store.cancelGroupInvitation(me.Public, c, h.RealmID()); err != nil {
		writeGroupFenceError(w, err)
		return
	}
	h.streams.notifyAll()
	writeJSON(w, 200, map[string]bool{"cancelled": true})
}
func writeGroupFenceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errGroupForbidden):
		writeError(w, 403, "group_forbidden", err.Error())
	case errors.Is(err, errGroupStale):
		writeError(w, 409, "group_stale", err.Error())
	case errors.Is(err, errGroupRealm):
		writeError(w, 409, "group_realm", err.Error())
	case errors.Is(err, errGroupInvitationCancelled):
		writeError(w, 409, "group_invitation_cancelled", err.Error())
	case errors.Is(err, errGroupInvitationPublished):
		writeError(w, 409, "group_invitation_published", err.Error())
	case errors.Is(err, errNotFound):
		writeError(w, 404, "", "group not found")
	default:
		writeError(w, 400, "", "group publication refused")
	}
}
