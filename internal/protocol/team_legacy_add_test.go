package protocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestTeamLegacyManagerAddPreservesMembershipContract(t *testing.T) {
	owner, member, other := teamPerson(t, "owner"), teamPerson(t, "member"), teamPerson(t, "other")
	root := teamStepFor(nil, owner, TeamCreate, "", "Legacy")
	state, err := root.Apply(nil, owner.r)
	if err != nil {
		t.Fatal(err)
	}
	change := func(by teamTestPerson, version int, op, target string) (TeamState, error) {
		s := teamStepFor(&state, by, op, target, "")
		s.V = version
		s.Sign(by.id.Sign)
		raw, _ := json.Marshal(s)
		parsed, e := ParseTeamStep(raw)
		if e != nil {
			return TeamState{}, e
		}
		return parsed.Apply(&state, by.r)
	}
	if _, err = change(owner, 1, TeamAdd, member.r.Person); err == nil {
		t.Fatal("v1 add must remain invalid")
	}
	before := state
	state, err = change(owner, 3, TeamAdd, member.r.Person)
	if err != nil {
		t.Fatalf("legacy manager add: %v", err)
	}
	if state.LegacyHead == nil || *state.LegacyHead != (TeamRef{ID: before.ID, Seq: before.Seq, Hash: before.Hash}) {
		t.Fatalf("legacy prefix not derived from verified predecessor: %+v", state.LegacyHead)
	}
	// The prefix is derived state, never a caller-supplied signed-step field.
	raw, _ := json.Marshal(teamStepFor(&before, owner, TeamArchive, "", ""))
	raw = append(raw[:len(raw)-1], []byte(`,"legacy_head":{"id":"`+before.ID+`","seq":0,"hash":"`+before.Hash+`"}}`)...)
	if _, e := ParseTeamStep(raw); e == nil {
		t.Fatal("incoming snapshot supplied legacy prefix")
	}
	corrupt := state
	badRef := *state.LegacyHead
	badRef.ID = NewID()
	corrupt.LegacyHead = &badRef
	bad := teamStepFor(&corrupt, owner, TeamArchive, "", "")
	bad.V = 3
	bad.Sign(owner.id.Sign)
	if _, e := bad.Apply(&corrupt, owner.r); e == nil {
		t.Fatal("cross-list derived prefix accepted")
	}
	if state.Version != 3 || state.ID != before.ID || state.RealmID != before.RealmID || state.Name != before.Name || !state.Member(owner.r.Person) || !state.Member(member.r.Person) || !reflect.DeepEqual(state.Managers, before.Managers) || state.Seq != before.Seq+1 {
		t.Fatalf("legacy add changed retained identity or roles: %+v", state)
	}
	if _, e := change(member, 3, TeamAdd, other.r.Person); !errors.Is(e, ErrTeamManager) {
		t.Fatalf("non-manager: %v", e)
	}
	for _, version := range []int{1, 2} {
		if _, e := change(owner, version, TeamArchive, ""); e == nil {
			t.Fatalf("version %d downgrade accepted", version)
		}
	}
	if _, e := change(owner, 3, TeamLeave, ""); !errors.Is(e, ErrTeamLastManager) {
		t.Fatalf("last manager left: %v", e)
	}
	if _, e := change(owner, 3, TeamRemove, owner.r.Person); e == nil {
		t.Fatal("manager membership removed implicitly")
	}
	if _, e := change(owner, 3, TeamManagerAdd, other.r.Person); e == nil {
		t.Fatal("non-member promoted")
	}
	state, err = change(member, 3, TeamLeave, "")
	if err != nil || state.Member(member.r.Person) {
		t.Fatalf("legacy self-leave: %+v %v", state, err)
	}
	state, err = change(other, 3, TeamJoin, "")
	if err != nil || !state.Member(other.r.Person) {
		t.Fatalf("legacy self-join: %+v %v", state, err)
	}
	state, err = change(owner, 3, TeamArchive, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, e := change(owner, 3, TeamAdd, member.r.Person); !errors.Is(e, ErrTeamArchived) {
		t.Fatalf("archived add: %v", e)
	}
}
