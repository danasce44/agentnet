package hub

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// TeamSchema is appended once by the store schema owner; no existing step
// changes. These directory records contain no conversation or worker grants.
const TeamSchema = `
CREATE TABLE teams(
 realm_id TEXT NOT NULL, team TEXT NOT NULL, seq INTEGER NOT NULL,
 hash TEXT NOT NULL, state TEXT NOT NULL, PRIMARY KEY(realm_id, team));
CREATE TABLE team_chain(
 realm_id TEXT NOT NULL, team TEXT NOT NULL, seq INTEGER NOT NULL,
 hash TEXT NOT NULL, record TEXT NOT NULL, PRIMARY KEY(realm_id, team, seq),
 UNIQUE(realm_id, team, hash));
`

var errTeamStale = errors.New("team changed meanwhile; fetch its current chain and retry")
var errTeamRefused = errors.New("team operation refused")

func (h *Hub) teamDirectory() (protocol.TeamDirectory, error) {
	return h.teamDirectoryVersion(false)
}

func (h *Hub) teamDirectoryVersion(tags bool) (protocol.TeamDirectory, error) {
	version := 1
	if tags {
		version = 2
	}
	return h.teamDirectoryFor(version)
}

// Current readers use the existing head query. Only old readers of an
// extended legacy list need a compatible signed prefix; omission would be
// mistaken for permanent deletion by their retained-directory logic.
func (h *Hub) teamDirectoryFor(version int) (protocol.TeamDirectory, error) {
	out := protocol.TeamDirectory{RealmID: h.RealmID(), Teams: []protocol.TeamRef{}}
	if version >= 2 {
		out.Version = version
	}

	rows, err := h.store.db.Query(`SELECT team,seq,hash,CASE WHEN ?<3 AND coalesce(json_extract(state,'$.version'),1)=3 THEN coalesce(json_extract(state,'$.legacy_head'),'invalid') ELSE '' END FROM teams WHERE realm_id=? AND (? >= 2 OR coalesce(json_extract(state,'$.version'),1) != 2) ORDER BY team LIMIT ?`, version, h.RealmID(), version, protocol.MaxTeams+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref protocol.TeamRef
		var prefix string
		if err := rows.Scan(&ref.ID, &ref.Seq, &ref.Hash, &prefix); err != nil {
			return out, err
		}
		if prefix != "" {
			var legacy protocol.TeamRef
			if err := json.Unmarshal([]byte(prefix), &legacy); err != nil || !validLegacyHead(&legacy, ref.ID, ref.Seq) {
				return out, errors.New("team: invalid legacy predecessor reference")
			}
			ref = legacy
		}
		out.Teams = append(out.Teams, ref)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Teams) > protocol.MaxTeams {
		out.Teams = out.Teams[:protocol.MaxTeams]
		out.Truncated = true
	}

	return out, nil
}

func validLegacyHead(ref *protocol.TeamRef, team string, seq int64) bool {
	return ref != nil && ref.ID == team && ref.Seq >= 0 && ref.Seq < seq && protocol.ValidHash(ref.Hash)
}

func teamReaderVersion(r *http.Request, key string) int {
	if r.URL.Query().Get(key) == "3" || r.URL.Query().Get("legacy_teams") == "3" {
		return 3
	}
	if r.URL.Query().Get(key) == "2" {
		return 2
	}
	return 1
}

func (h *Hub) handleTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	d, err := h.teamDirectoryFor(teamReaderVersion(r, "version"))
	if err != nil {
		writeError(w, 500, "", "team directory unavailable")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func teamHeadIn(q querier, realm, team string) (protocol.TeamState, bool, error) {
	var state protocol.TeamState
	var raw string
	err := q.QueryRow(`SELECT state FROM teams WHERE realm_id=? AND team=?`, realm, team).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	err = json.Unmarshal([]byte(raw), &state)
	return state, true, err
}

// putTeamStep authenticates both publisher and signed author, then applies
// one operation at the current predecessor in the same immediate transaction.
func (s *store) putTeamStep(realm string, caller identity.Public, step protocol.TeamStep, raw []byte) (same bool, err error) {
	if step.RealmID != realm {
		return false, fmt.Errorf("%w: workspace identity mismatch", errTeamRefused)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	me, err := agentIn(tx, caller.Address)
	if err != nil {
		return false, err
	}
	if me.Revoked || me.Pending || me.Person != step.Author.Person || step.Author.Address != caller.Address || step.Author.Fingerprint != caller.Fingerprint() {
		return false, fmt.Errorf("%w: publisher is not the author person's current device", errTeamRefused)
	}
	head, ok, err := personHeadIn(tx, me.Person)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errTeamRefused
	}
	current, err := protocol.ParsePersonRoster(head.record)
	if err != nil {
		return false, err
	}
	if !current.Has(caller.Address, caller.Fingerprint()) {
		return false, errTeamRefused
	}
	var rosterRaw string
	if err := tx.QueryRow(`SELECT record FROM person_chain WHERE person=? AND hash=?`, me.Person, step.Author.Roster).Scan(&rosterRaw); errors.Is(err, sql.ErrNoRows) {
		return false, errRosterStale
	} else if err != nil {
		return false, err
	}
	roster, err := protocol.ParsePersonRoster([]byte(rosterRaw))
	if err != nil {
		return false, err
	}
	dev, ok := roster.Device(step.Author.Fingerprint)
	if !ok || dev.Address != caller.Address || !ed25519.Verify(dev.SignKey, step.Canonical(), step.Sig) {
		return false, fmt.Errorf("%w: invalid author signature", errTeamRefused)
	}
	var stored string
	err = tx.QueryRow(`SELECT hash FROM team_chain WHERE realm_id=? AND team=? AND seq=?`, realm, step.Team, step.Seq).Scan(&stored)
	if err == nil {
		if stored == step.Hash() {
			return true, nil
		}
		return false, errTeamStale
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	prev, found, err := teamHeadIn(tx, realm, step.Team)
	if err != nil {
		return false, err
	}
	var before *protocol.TeamState
	if found {
		if step.Seq != prev.Seq+1 || step.Prev != prev.Hash {
			return false, errTeamStale
		}
		before = &prev
	} else if step.Seq != 0 {
		return false, errTeamStale
	}
	if !found {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM teams WHERE realm_id=?`, realm).Scan(&n); err != nil {
			return false, err
		}
		if n >= protocol.MaxTeams {
			return false, fmt.Errorf("%w: team directory limit reached", errTeamRefused)
		}
	}
	next, err := step.Apply(before, roster)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errTeamRefused, err)
	}
	state, _ := json.Marshal(next)
	if _, err := tx.Exec(`INSERT INTO team_chain(realm_id,team,seq,hash,record) VALUES(?,?,?,?,?)`, realm, step.Team, step.Seq, step.Hash(), string(raw)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO teams(realm_id,team,seq,hash,state) VALUES(?,?,?,?,?) ON CONFLICT(realm_id,team) DO UPDATE SET seq=excluded.seq,hash=excluded.hash,state=excluded.state`, realm, step.Team, step.Seq, step.Hash(), string(state)); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (h *Hub) handlePutTeam(w http.ResponseWriter, r *http.Request) {
	caller, body, ok := h.authenticateBody(w, r)
	if !ok {
		return
	}
	step, err := protocol.ParseTeamStep(body)
	if err != nil {
		writeError(w, 400, "", err.Error())
		return
	}
	member, err := h.store.agent(caller)
	if err != nil {
		writeError(w, 500, "", "team storage unavailable")
		return
	}
	same, err := h.store.putTeamStep(h.RealmID(), member.Public, step, body)
	switch {
	case errors.Is(err, errTeamStale):
		writeError(w, 409, protocol.CodeTeamStale, err.Error())
		return
	case errors.Is(err, errRosterStale):
		writeError(w, 409, protocol.CodeRosterStale, err.Error())
		return
	case errors.Is(err, errTeamRefused):
		writeError(w, 403, protocol.CodeTeamRefused, err.Error())
		return
	case err != nil:
		writeError(w, 500, "", "team storage unavailable")
		return
	}
	if !same {
		h.lookAgain()
	} // reuse directory push generation (both lists, as before), never a job notification
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) handleTeamChain(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	team := r.PathValue("id")
	if !protocol.ValidID(team) {
		writeError(w, 400, "", "invalid team id")
		return
	}
	after := int64(-1)
	var err error
	if text := r.URL.Query().Get("after"); text != "" {
		after, err = strconv.ParseInt(text, 10, 64)
		if err != nil || after < -1 {
			writeError(w, 400, "", "invalid chain offset")
			return
		}
	}
	head, found, err := teamHeadIn(h.store.db, h.RealmID(), team)
	if err != nil {
		writeError(w, 500, "", "team storage unavailable")
		return
	} else if !found {
		writeError(w, 404, "", "team not found")
		return
	}
	out := protocol.TeamChain{RealmID: h.RealmID(), Team: team, Records: []json.RawMessage{}}
	query := `SELECT record FROM team_chain WHERE realm_id=? AND team=? AND seq>?`
	args := []any{h.RealmID(), team, after}
	if head.Version == 3 && teamReaderVersion(r, "version") < 3 {
		if !validLegacyHead(head.LegacyHead, team, head.Seq) {
			writeError(w, 500, "", "team chain unavailable")
			return
		}
		query += ` AND seq<=?`
		args = append(args, head.LegacyHead.Seq)
	}
	args = append(args, protocol.TeamChainPage+1)
	rows, err := h.store.db.Query(query+` ORDER BY seq LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "", "team chain unavailable")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			writeError(w, 500, "", "team chain unavailable")
			return
		}
		out.Records = append(out.Records, json.RawMessage(raw))
	}
	if rows.Err() != nil {
		writeError(w, 500, "", "team chain unavailable")
		return
	}
	if len(out.Records) > protocol.TeamChainPage {
		out.Records = out.Records[:protocol.TeamChainPage]
		out.More = true
	}
	writeJSON(w, 200, out)
}

// Lists are workspace directory metadata, not conversation authority. Deletion
// uses the Hub's existing signed-request/admin checks; no caller-supplied role
// becomes part of the signed manager chain. Retain that chain as a tombstone.
func (h *Hub) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !protocol.ValidID(id) {
		writeError(w, 400, "", "invalid people list id")
		return
	}
	tx, err := h.store.db.Begin()
	if err != nil {
		writeError(w, 500, "", "people list unavailable")
		return
	}
	defer tx.Rollback()
	member, err := agentIn(tx, caller)
	if err != nil {
		writeError(w, 500, "", "people list unavailable")
		return
	}
	if member.Revoked || member.Pending {
		writeError(w, 403, protocol.CodeTeamRefused, "people list deletion refused")
		return
	}
	state, found, err := teamHeadIn(tx, h.RealmID(), id)
	if err != nil {
		writeError(w, 500, "", "people list unavailable")
		return
	}
	if !found {
		writeError(w, 404, "", "people list not found")
		return
	}
	// A removed roster device cannot keep a person's former manager/admin rights.
	if member.Person != "" {
		head, exists, err := personHeadIn(tx, member.Person)
		if err != nil {
			writeError(w, 500, "", "person unavailable")
			return
		}
		roster, err := protocol.ParsePersonRoster(head.record)
		if !exists || err != nil || !roster.Has(caller, member.Public.Fingerprint()) {
			writeError(w, 403, protocol.CodeTeamRefused, "people list deletion refused")
			return
		}
	}
	if !member.Admin && (member.Person == "" || !state.Manager(member.Person)) {
		writeError(w, 403, protocol.CodeTeamRefused, "Only a list manager or workspace admin can delete this list.")
		return
	}
	if _, err := tx.Exec(`DELETE FROM teams WHERE realm_id=? AND team=?`, h.RealmID(), id); err != nil {
		writeError(w, 500, "", "people list deletion failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "", "people list deletion failed")
		return
	}
	h.lookAgain()
	w.WriteHeader(http.StatusNoContent)
}
