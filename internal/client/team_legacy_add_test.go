package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestLegacyListManagerAddSyncAndNoGrants(t *testing.T) {
	w := teamWorld(t)
	addRealmHistoryAndGrants(t, w.bob)
	before := realmRows(t, w.bob.store.db)
	legacy := mustTeamChange(t, w.alice, TeamChange{Op: protocol.TeamCreate, Name: "Retained legacy"})
	person, _, _ := w.bob.Person()
	next := mustTeamChange(t, w.alice, TeamChange{Team: legacy.ID, Op: protocol.TeamAdd, Target: person.Person})
	if next.Version != 3 || next.ID != legacy.ID || next.Name != legacy.Name || !reflect.DeepEqual(next.Managers, legacy.Managers) || !next.Member(person.Person) {
		t.Fatalf("legacy add lost identity or semantics: %+v", next)
	}
	view, err := w.bob.Teams(tctx(t))
	if err != nil || !view.Current || !view.LegacyAdd || !view.Tags || len(view.Teams) != 1 || !view.Teams[0].Member {
		t.Fatalf("other device sync: %+v %v", view, err)
	}
	if _, err = w.bob.ChangeTeam(tctx(t), TeamChange{Team: legacy.ID, Op: protocol.TeamAdd, Target: protocol.NewID()}); !errors.Is(err, protocol.ErrTeamManager) {
		t.Fatalf("member edited list: %v", err)
	}
	mustTeamChange(t, w.bob, TeamChange{Team: legacy.ID, Op: protocol.TeamLeave})
	mustTeamChange(t, w.bob, TeamChange{Team: legacy.ID, Op: protocol.TeamJoin})
	snap, err := w.bob.TeamSnapshot(tctx(t), []string{legacy.ID})
	if err != nil || len(snap.Persons) != 2 || len(snap.Sources) != 1 || snap.Sources[0].Seq != 3 {
		t.Fatalf("true-head snapshot: %+v %v", snap, err)
	}
	after := realmRows(t, w.bob.store.db)
	for _, table := range []string{"approvals", "task_grants", "inbox", "outbox"} {
		if !reflect.DeepEqual(before[table], after[table]) {
			t.Fatalf("adding people changed %s", table)
		}
	}
	for _, table := range []string{"conversations", "participation_events", "history_jobs", "operators", "attachments"} {
		var n int
		if err = w.bob.store.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("list change created %s: %d %v", table, n, err)
		}
	}
	home := w.bob.home
	w.bob.Close()
	again, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	view, err = again.Teams(tctx(t))
	if err != nil || !view.Current || len(view.Teams) != 1 || view.Teams[0].Version != 3 || !view.Teams[0].Member {
		t.Fatalf("retained after restart: %+v %v", view, err)
	}
	again.hub.http.Transport = legacyPrefixTransport{base: again.hub.http.Transport, directory: protocol.TeamDirectory{Version: 2, RealmID: legacy.RealmID, Teams: []protocol.TeamRef{{ID: legacy.ID, Seq: legacy.Seq, Hash: legacy.Hash}}}}
	if _, err = again.TeamSnapshot(tctx(t), []string{legacy.ID}); !errors.Is(err, ErrTeamStale) {
		t.Fatalf("compatible prefix authorized a newer selection: %v", err)
	}

}

// A relay/read path that knows only the compatible prefix cannot authorize
// expanding a cached newer list for group selection.
type legacyPrefixTransport struct {
	base      http.RoundTripper
	directory protocol.TeamDirectory
}

func (rt legacyPrefixTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/teams" {
		raw, _ := json.Marshal(rt.directory)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	}
	return rt.base.RoundTrip(r)
}

func TestTeamSnapshotFreshnessDuringRosterRefresh(t *testing.T) {
	for _, mode := range []string{"disconnect", "reconnected", "capability-downgrade", "selected-head", "unrelated-head"} {
		t.Run(mode, func(t *testing.T) {
			w := teamWorld(t)
			selected := mustTeamChange(t, w.alice, TeamChange{Op: protocol.TeamCreate, Name: "Selected legacy"})
			member, _, _ := w.bob.Person()
			selected = mustTeamChange(t, w.alice, TeamChange{Team: selected.ID, Op: protocol.TeamAdd, Target: member.Person})
			unrelated := mustTeamChange(t, w.alice, TeamChange{Op: protocol.TeamCreate, Name: "Other list"})
			if _, err := w.bob.Teams(tctx(t)); err != nil {
				t.Fatal(err)
			}
			base := w.bob.hub.http.Transport
			var fired atomic.Bool
			w.bob.hub.http.Transport = personLabelTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/persons/") && fired.CompareAndSwap(false, true) {
					switch mode {
					case "disconnect", "reconnected":
						w.bob.teamsDisconnected()
						if mode == "reconnected" {
							if _, err := w.bob.Teams(tctx(t)); err != nil {
								t.Fatal(err)
							}
						}
					case "capability-downgrade":
						w.bob.teams.mu.Lock()
						w.bob.teams.directory.Version = 2
						w.bob.teams.mu.Unlock()
					case "selected-head", "unrelated-head":
						id := selected.ID
						if mode == "unrelated-head" {
							id = unrelated.ID
						}
						mustTeamChange(t, w.alice, TeamChange{Team: id, Op: protocol.TeamRename, Name: "Changed during roster refresh"})
						if _, err := w.bob.Teams(tctx(t)); err != nil {
							t.Fatal(err)
						}
					}
				}
				return base.RoundTrip(r)
			})
			snapshot, err := w.bob.TeamSnapshot(tctx(t), []string{selected.ID})
			if !fired.Load() {
				t.Fatal("roster refresh hook did not execute")
			}
			if mode == "unrelated-head" {
				if err != nil || len(snapshot.Sources) != 1 || snapshot.Sources[0] != (protocol.TeamRef{ID: selected.ID, Seq: selected.Seq, Hash: selected.Hash}) {
					t.Fatalf("unrelated verified change disturbed selected list: %+v %v", snapshot, err)
				}
			} else if !errors.Is(err, ErrTeamStale) {
				t.Fatalf("%s during roster refresh returned a stale snapshot: %+v %v", mode, snapshot, err)
			}
			// A rejected in-flight selection neither freezes nor changes another list.
			if _, err = w.bob.TeamSnapshot(tctx(t), []string{unrelated.ID}); err != nil {
				t.Fatalf("fresh unrelated selection was blocked: %v", err)
			}
		})
	}
}
