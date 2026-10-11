package client

import (
	"context"
	"database/sql"
	"time"
)

type recipientTarget struct{ address, fp string }

// Finish the current round before adopting newer member evidence, so frequent
// presence changes cannot starve targets at the end. Completed rounds are idle.
type recipientDiscovery struct {
	generation uint64
	loaded     bool
	retry      bool
	targets    []recipientTarget
	pos        int
	checked    map[recipientTarget]bool
}

// A missing member is only a discovery hint: offline, suspended, truncated
// lists and detached standalone devices never prove permanent revocation.
func (a *Agent) reconcileRecipientLifecycle(ctx context.Context) (retErr error) {
	defer func() {
		if retryable(retErr) {
			a.convWork.due(convRecipients)
		}
	}()
	v := a.MemberView()
	if !v.Current || v.Listed != MembersListed {
		return nil
	}
	present := map[string]bool{}
	for _, m := range v.Members.Members {
		present[m.Address] = true
	}
	w := &a.convWork
	w.mu.Lock()
	scan := w.recipients
	w.mu.Unlock()
	if scan.loaded && len(scan.targets) == 0 && scan.generation != v.discovery {
		scan = recipientDiscovery{}
	}
	if !scan.loaded || len(scan.targets) == 0 {
		scan.retry = false
		rows, err := a.store.db.QueryContext(ctx, `SELECT DISTINCT recipient,recipient_fp FROM outbox WHERE state IN ('queued','waiting','receiver_waiting','archive_staged','archive_uploading') AND recipient_fp IS NOT NULL AND recipient_fp!='' UNION SELECT device,fingerprint FROM history_jobs WHERE state!='ended' AND fingerprint IS NOT NULL AND fingerprint!='' UNION SELECT DISTINCT sender,verified_by FROM inbox WHERE verified_by IS NOT NULL AND verified_by!='' AND replica=0 AND kind IN ('question','task') AND state IN ('pending','accepted','held','awaiting','conv_held','part_waiting') UNION SELECT DISTINCT sender,verified_by FROM inbox WHERE verified_by IS NOT NULL AND verified_by!='' AND replica=0 AND kind='message' AND status='review_notice' AND state='needs_human' AND NOT EXISTS(SELECT 1 FROM config c WHERE c.k='revoked-exact-device/'||inbox.sender||'/'||inbox.verified_by AND c.v=inbox.sender)`)
		if err != nil {
			return err
		}
		var targets []recipientTarget
		for rows.Next() {
			var t recipientTarget
			if err = rows.Scan(&t.address, &t.fp); err != nil {
				rows.Close()
				return err
			}
			if !present[t.address] {
				targets = append(targets, t)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		checked := scan.checked
		if checked == nil {
			checked = map[recipientTarget]bool{}
			scan.checked = checked
		}
		for _, t := range targets {
			if !checked[t] {
				scan.targets = append(scan.targets, t)
				checked[t] = true
			}
		}
		scan.generation, scan.loaded = v.discovery, true
	}
	end := min(scan.pos+proofPage, len(scan.targets))
	for ; scan.pos < end; scan.pos++ {
		t := scan.targets[scan.pos]
		if present[t.address] {
			continue
		}
		known, err := revokedDeviceKnown(a.store.db, t.address, t.fp)
		if err != nil {
			return err
		}
		if known {
			delete(scan.checked, t)
			var open bool
			if err = a.store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM outbox WHERE recipient=? AND recipient_fp=? AND state IN ('queued','waiting','receiver_waiting','archive_staged','archive_uploading')) OR EXISTS(SELECT 1 FROM history_jobs WHERE device=? AND fingerprint=? AND state!='ended') OR EXISTS(SELECT 1 FROM inbox WHERE sender=? AND verified_by=? AND replica=0 AND kind IN ('question','task') AND state IN ('pending','accepted','held','awaiting','conv_held','part_waiting'))`, t.address, t.fp, t.address, t.fp, t.address, t.fp).Scan(&open); err != nil {
				return err
			}
			if !open {
				continue
			}
			if err = a.store.endRecipient(t.address, t.fp, true); err != nil {
				return err
			}
			continue
		}
		e, err := a.directory(ctx, t.address)
		if err != nil {
			if retryable(err) {
				scan.retry = true
				delete(scan.checked, t)        // failed transport is not a definitive directory response
				a.convWork.due(convRecipients) // next existing ping/event; no immediate self-kick
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if !e.Revoked || e.Public.Fingerprint() != t.fp {
			continue
		}
		delete(scan.checked, t)
		if err = a.store.endRecipient(t.address, t.fp, true); err != nil {
			return err
		}
	}
	retryRound := scan.retry && scan.pos == len(scan.targets)
	w.mu.Lock()
	if scan.pos == len(scan.targets) && scan.generation != a.MemberView().discovery {
		scan = recipientDiscovery{}
	} else if scan.pos == len(scan.targets) {
		scan.targets = nil
		scan.pos = 0
	}
	w.recipients = scan
	more := !scan.loaded || scan.pos < len(scan.targets)
	w.mu.Unlock()
	if retryRound {
		w.due(convRecipients) // preserve failures consumed by later page turns, without self-kick
	}
	if more {
		w.due(convRecipients)
		a.kickNow()
	}
	return nil
}

// End only transport not yet admitted by the relay. Custody has its own
// authoritative terminal receipt; originals, ledgers and executor jobs stay.
func (s *store) endRecipient(address, fp string, revoked bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var changed int64
	count := func(result sql.Result, err error) error {
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		changed += n
		return err
	}
	filter := ""
	reason := "not delivered: the recipient was permanently removed from this workspace"
	if !revoked {
		var departed bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM persons WHERE state=?) AND NOT EXISTS(SELECT 1 FROM person_devices d JOIN persons p ON p.person=d.person WHERE p.state=? AND d.address=? AND d.fingerprint=?)`, personSelf, personSelf, address, fp).Scan(&departed); err != nil {
			return err
		}
		if !departed {
			return nil
		}
		filter = ` AND (sub IN ` + syncSubs + ` OR id IN (SELECT child FROM history_archive_entries))`
		reason = "not delivered: that exact device is no longer part of this person"
	}
	if err = count(tx.Exec(`UPDATE outbox SET state=?,error=? WHERE recipient=? AND recipient_fp=? AND state IN ('queued','waiting','receiver_waiting','archive_staged','archive_uploading')`+filter, stateNotDelivered, reason, address, fp)); err != nil {
		return err
	}
	if err = count(tx.Exec(`UPDATE history_jobs SET state='ended',updated_at=? WHERE device=? AND fingerprint=? AND state!='ended'`, time.Now().Unix(), address, fp)); err != nil {
		return err
	}
	if revoked {
		if err = count(tx.Exec(`INSERT INTO config(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v WHERE config.v!=excluded.v`, revokedDevicePrefix+address+"/"+fp, address)); err != nil {
			return err
		}
		n, e := endRevokedRequests(tx)
		changed += n
		if e != nil {
			return e
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if changed > 0 {
		s.changed()
	}
	return nil
}

const revokedDevicePrefix = "revoked-exact-device/"

func currentDeliveryRecipient(q dbq, address, fp string) error {
	known, err := revokedDeviceKnown(q, address, fp)
	if err != nil {
		return err
	}
	if known {
		return ErrPeerRevoked
	}
	return nil
}

func revokedDeviceKnown(q dbq, address, fp string) (bool, error) {
	var denied bool
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM config WHERE k=? AND v=?)`, revokedDevicePrefix+address+"/"+fp, address).Scan(&denied)
	return denied, err
}

// This runs inside claim transactions too, so a later local Accept cannot
// make a known revoked key eligible. Running and historical replicas stay.
func endRevokedRequests(q *sql.Tx) (int64, error) {
	var known bool
	if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM config WHERE k>=? AND k<? LIMIT 1)`, revokedDevicePrefix, revokedDevicePrefix+"~").Scan(&known); err != nil {
		return 0, err
	}
	if !known {
		return 0, nil
	}
	var pending bool
	if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM inbox WHERE replica=0 AND kind IN ('question','task') AND state IN ('pending','accepted','held','awaiting','conv_held','part_waiting') AND EXISTS(SELECT 1 FROM config c WHERE c.k=?||inbox.sender||'/'||inbox.verified_by AND c.v=inbox.sender))`, revokedDevicePrefix).Scan(&pending); err != nil {
		return 0, err
	}
	if !pending {
		return 0, nil
	}
	result, err := q.Exec(`UPDATE inbox SET state='not_run',detail='not run: the sender was permanently removed from this workspace',status_due=status_due+1 WHERE replica=0 AND kind IN ('question','task') AND state IN ('pending','accepted','held','awaiting','conv_held','part_waiting') AND EXISTS(SELECT 1 FROM config c WHERE c.k=?||inbox.sender||'/'||inbox.verified_by AND c.v=inbox.sender)`, revokedDevicePrefix)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Kept separate from relay revocation: leaving an own roster can leave a
// nonlinked device enrolled, so ordinary DMs to it must remain usable.
func (a *Agent) reconcileDepartedHistory() error {
	own, ok, err := a.store.selfPerson(a.Address)
	if err != nil || !ok {
		return err
	}
	rows, err := a.store.db.Query(`SELECT device,fingerprint FROM history_jobs WHERE state!='ended' UNION SELECT recipient,recipient_fp FROM outbox WHERE sub IN ` + syncSubs + ` AND state IN ('queued','waiting','receiver_waiting','archive_staged','archive_uploading')`)
	if err != nil {
		return err
	}
	type target struct{ address, fp string }
	var ended []target
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.address, &t.fp); err != nil {
			rows.Close()
			return err
		}
		if !own.roster.Has(t.address, t.fp) {
			ended = append(ended, t)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range ended {
		if err = a.store.endRecipient(t.address, t.fp, false); err != nil {
			return err
		}
	}
	return nil
}
