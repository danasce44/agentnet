package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestLegacyListAddOldReaderPrefixAndIsolation(t *testing.T) {
	h, owner, other := hubTeamFixture(t)
	create := hubTeamStep(h, nil, owner, protocol.TeamCreate, "", "Legacy")
	if w := putTeamHTTP(t, h, owner.member, create); w.Code != 204 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	root, _, _ := teamHeadIn(h.store.db, h.RealmID(), create.Team)
	add := hubTeamStep(h, &root, owner, protocol.TeamAdd, other.roster.Person, "")
	add.V = 3
	add.Sign(owner.member.id.Sign)
	if w := putTeamHTTP(t, h, owner.member, add); w.Code != 204 {
		t.Fatalf("legacy manager add: %d %s", w.Code, w.Body)
	}
	// A retained old-reader ref stays at the signed predecessor, never vanishes
	// and never poisons reading another list with an unsupported chain suffix.
	for _, tags := range []bool{false, true} {
		d, err := h.teamDirectoryVersion(tags)
		if err != nil || len(d.Teams) != 1 || d.Teams[0].Seq != root.Seq || d.Teams[0].Hash != root.Hash {
			t.Fatalf("old directory tags=%v: %+v %v", tags, d, err)
		}
	}
	for _, version := range []int{0, 3} {
		path := fmt.Sprintf("/v1/teams/%s/chain?after=-1", root.ID)
		if version == 3 {
			path += "&version=3"
		}
		req := signed(t, owner.member.id, owner.member.addr, http.MethodGet, path, nil)
		req.SetPathValue("id", root.ID)
		out := httptest.NewRecorder()
		h.handleTeamChain(out, req)
		var chain protocol.TeamChain
		if err := json.Unmarshal(out.Body.Bytes(), &chain); out.Code != 200 || err != nil {
			t.Fatalf("chain v%d: %d %s %v", version, out.Code, out.Body, err)
		}
		want := 1
		if version == 3 {
			want = 2
		}
		if len(chain.Records) != want || chain.More {
			t.Fatalf("chain v%d: %+v", version, chain)
		}
	}
	oldWrite := hubTeamStep(h, &root, owner, protocol.TeamRename, "", "Old writer")
	if w := putTeamHTTP(t, h, owner.member, oldWrite); w.Code != 409 {
		t.Fatalf("old prefix write must fail CAS: %d %s", w.Code, w.Body)
	}
	next, _, _ := teamHeadIn(h.store.db, h.RealmID(), root.ID)
	if next.Version != 3 || next.Name != root.Name || !next.Member(other.roster.Person) || !next.Manager(owner.roster.Person) {
		t.Fatalf("extension lost existing state: %+v", next)
	}
	for range 2 {
		if w := putTeamHTTP(t, h, owner.member, add); w.Code != 204 {
			t.Fatalf("add replay: %d %s", w.Code, w.Body)
		}
	}
	outsider := hubTeamStep(h, &next, other, protocol.TeamAdd, protocol.NewID(), "")
	outsider.V = 3
	outsider.Sign(other.member.id.Sign)
	if w := putTeamHTTP(t, h, other.member, outsider); w.Code != 403 {
		t.Fatalf("non-manager add: %d %s", w.Code, w.Body)
	}
	foreign := hubTeamStep(h, &next, owner, protocol.TeamAdd, protocol.NewID(), "")
	foreign.V = 3
	foreign.RealmID = protocol.NewID()
	foreign.Sign(owner.member.id.Sign)
	if w := putTeamHTTP(t, h, owner.member, foreign); w.Code != 403 {
		t.Fatalf("foreign realm add: %d %s", w.Code, w.Body)
	}
	unrelated := hubTeamStep(h, nil, other, protocol.TeamCreate, "", "Unrelated")
	if w := putTeamHTTP(t, h, other.member, unrelated); w.Code != 204 {
		t.Fatalf("old reader stopped unrelated list: %d %s", w.Code, w.Body)
	}
	req := signed(t, owner.member.id, owner.member.addr, http.MethodGet, "/v1/teams?version=3", nil)
	out := httptest.NewRecorder()
	h.handleTeams(out, req)
	var all protocol.TeamDirectory
	if err := json.Unmarshal(out.Body.Bytes(), &all); out.Code != 200 || err != nil || all.Version != 3 || len(all.Teams) != 2 {
		t.Fatalf("current directory: %d %s %v", out.Code, out.Body, err)
	}
}
