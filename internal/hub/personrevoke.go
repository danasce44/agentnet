package hub

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/misunders2d/agentnet/internal/protocol"
)

var errPersonRevokeRefused = errors.New("an admin cannot revoke its own person or an invite-admin person through a device grant")

// handlePersonRevoke offboards an exact verified person, never a display name.
func (h *Hub) handlePersonRevoke(w http.ResponseWriter, r *http.Request) {
	caller, body, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	var req protocol.RevokePersonRequest
	if decodeStrict(body, &req) != nil || !protocol.ValidID(req.Person) {
		writeError(w, 400, "", "choose an exact person id")
		return
	}
	addresses, err := h.store.revokePerson(caller, req.Person)
	switch {
	case errors.Is(err, errPersonRevokeRefused), errors.Is(err, errRevoked):
		writeError(w, 403, "", err.Error())
		return
	case errors.Is(err, errNotFound):
		writeError(w, 404, "", "unknown person")
		return
	case errors.Is(err, errBadStep):
		writeError(w, 409, "", "person identity records conflict; no devices were revoked")
		return
	case err != nil:
		writeError(w, 500, "", "storage error")
		return
	}
	for _, address := range addresses {
		h.streams.disconnect(address)
		h.presence.drop(address)
	}
	h.lookAgain()
	h.waiters.notifyAll()
	h.cfg.Logf("revoked person %s (%d devices) by %s", req.Person, len(addresses), caller)
	writeJSON(w, http.StatusOK, protocol.RevokePersonResponse{Revoked: addresses})
}

func (s *store) revokePerson(caller, person string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	me, err := agentIn(tx, caller)
	if err != nil {
		return nil, err
	}
	if !me.Admin || me.Revoked || me.Pending {
		return nil, errRevoked
	}
	if me.Person == person {
		return nil, errPersonRevokeRefused
	}
	head, ok, err := personHeadIn(tx, person)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotFound
	}
	roster, err := protocol.ParsePersonRoster(head.record)
	if err != nil || roster.Person != person {
		return nil, errBadStep
	}
	email := roster.Email
	var mapped string
	mapErr := tx.QueryRow(`SELECT email FROM google_people WHERE person=?`, person).Scan(&mapped)
	if mapErr != nil && !errors.Is(mapErr, sql.ErrNoRows) {
		return nil, mapErr
	}
	if mapped != "" {
		if email != "" && email != mapped {
			return nil, errBadStep
		}
		email = mapped
	}
	googleLabel := ""
	if email != "" {
		googleLabel = protocol.GoogleLabel(email)
	}
	// Google's verified email scope includes old detached standalone keys,
	// matching the existing email offboard operation rather than a display name.
	rows, err := tx.Query(`SELECT address FROM agents WHERE revoked_at IS NULL AND (person_id=? OR pending_person=? OR (?!='' AND label=?)) ORDER BY address`, person, person, googleLabel, googleLabel)
	if err != nil {
		return nil, err
	}
	addresses := []string{}
	for rows.Next() {
		var address string
		if err = rows.Scan(&address); err != nil {
			rows.Close()
			return nil, err
		}
		addresses = append(addresses, address)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Keep the same protection as exact device revocation, within this atomic
	// transaction so an admin grant cannot race a larger person offboarding.
	for _, address := range addresses {
		target, err := agentIn(tx, address)
		if err != nil {
			return nil, err
		}
		if address == caller || me.linked && target.Admin && !target.linked {
			return nil, errPersonRevokeRefused
		}
	}
	for _, address := range addresses {
		if err := revokeDeviceAdminGrants(tx, address, caller); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE agents SET revoked_at=?,revoked_reason='removed' WHERE address=? AND revoked_at IS NULL`, time.Now().Unix(), address); err != nil {
			return nil, err
		}
		if err := settleRevokedDevice(tx, address); err != nil {
			return nil, err
		}
	}
	// A signed link from an offboarded person never admits a fresh key later.
	if _, err := tx.Exec(`UPDATE invites SET expires_at=0 WHERE person=? AND used_by IS NULL`, person); err != nil {
		return nil, err
	}
	// Email denial overrides an allowed domain, preserving Google offboarding.
	if email != "" {
		if _, err := tx.Exec(`INSERT INTO google_emails(email,admin,denied) VALUES(?,0,1) ON CONFLICT(email) DO UPDATE SET admin=0,denied=1`, email); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM google_people WHERE person=?`, person); err != nil {
		return nil, err
	}
	return addresses, tx.Commit()
}

// settleRevokedDevice is called after a permanent revocation in the same
// transaction. Copies TO the removed device can no longer arrive. Ordinary
// messages already accepted FROM it still reach offline recipients, just as
// they reached online recipients before removal. Pending execution and bulk
// sync from that key end; neither a sender timestamp nor a delivery is invented.
func settleRevokedDevice(tx *sql.Tx, address string) error {
	if err := dropNotify(tx, address); err != nil {
		return err
	}
	if _, err := expireCustody(tx, "(recipient=? OR (sender=? AND "+revokedSenderWorkSQL+"))", address, address); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE invites SET expires_at=0 WHERE created_by=? AND used_by IS NULL`, address)
	return err
}

// Outer kind is signed and checked on admission. Lane is only an availability
// hint: clients still verify the encrypted shape and reject revoked controls.
// Old opaque/corrupt records must not abort the whole upgrade or revocation.
// CASE guards JSON parsing even when SQLite evaluates this before the address
// predicate. An unreadable removed-sender record cannot be retained as proven
// ordinary communication; unrelated records remain untouched.
const revokedSenderWorkSQL = `(lane='sync' OR CASE WHEN json_valid(CAST(envelope AS TEXT))
 THEN json_extract(CAST(envelope AS TEXT),'$.kind') IN ('question','task') ELSE 1 END)`
