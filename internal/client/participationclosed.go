package client

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/misunders2d/agentnet/internal/protocol"
)

// closedParticipation preserves terminal history after an author's phone is
// retired. This separate projection can only return a fully verified dismissal
// or decline; it never returns a live invitation, consent or task grant. The
// normal resolver continues to require current keys for every live operation.
func closedParticipation(q dbq, conv, pid string, events []protocol.ParticipationEvent, current dmMembers, fallback ParticipationInfo) (ParticipationInfo, error) {
	if current.group == nil || fallback.Held == 0 || !slices.ContainsFunc(events, func(e protocol.ParticipationEvent) bool {
		return e.Type == protocol.EventDismiss || e.Type == protocol.EventDecline
	}) {
		return fallback, nil
	}
	past := current
	past.persons = maps.Clone(current.persons)
	past.hosts = nil
	for _, ev := range events {
		p, member := past.persons[ev.Author.Person]
		if !member || current.persons[ev.Author.Person].has(ev.Author.Address, ev.Author.Fingerprint) {
			continue
		}
		// Only an original step of this still-pinned person's chain can
		// identify a former device. A fork, unknown key or changed pin cannot.
		var raw []byte
		err := q.QueryRow(`SELECT record FROM person_chain WHERE person=? AND hash=?`, ev.Author.Person, ev.Author.Roster).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		if err != nil {
			return fallback, err
		}
		var roster protocol.PersonRoster
		if json.Unmarshal(raw, &roster) != nil || roster.Hash() != ev.Author.Roster {
			return fallback, nil
		}
		key, found := roster.Device(ev.Author.Fingerprint)
		if !found || key.Address != ev.Author.Address || ev.Verify(key.SignKey) != nil {
			return fallback, nil
		}
		pin, found, err := pinnedKey(q, ev.Author.Address)
		if err != nil {
			return fallback, err
		}
		var pending int
		if err = q.QueryRow(`SELECT count(*) FROM peers WHERE address=? AND pending IS NOT NULL`, ev.Author.Address).Scan(&pending); err != nil {
			return fallback, err
		}
		if pending != 0 || found && pin.Fingerprint() != ev.Author.Fingerprint {
			return fallback, nil
		}
		if !p.has(key.Address, key.Fingerprint()) {
			p.roster.Devices = append(slices.Clone(p.roster.Devices), key)
		}
		past.persons[ev.Author.Person] = p
	}
	if err := past.loadHosts(q, events); err != nil {
		return fallback, err
	}
	closed := resolve(conv, pid, events, past)
	if closed.Held != 0 || closed.State != PartDismissed && closed.State != PartDeclined || closed.Dismissal == "" && closed.Decision == "" {
		return fallback, nil
	}
	closed.Grant, closed.TaskKeys, closed.Shares = nil, nil, nil
	return closed, nil
}
