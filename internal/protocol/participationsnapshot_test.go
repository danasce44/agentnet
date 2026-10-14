package protocol

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
)

func TestParticipationSnapshotSignedExactGrant(t *testing.T) {
	e := vecInvite()
	topic := NewID()
	e.Topic = &topic
	for _, g := range e.Grant {
		e.Snapshots = append(e.Snapshots, GrantSnapshot{LID: g.LID, Fingerprint: g.Fingerprint, Hash: strings.Repeat("a", 64)})
	}
	e.Sign(vecKey())
	if err := e.Verify(vecKey().Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(e)
	if _, err := ParseParticipationEvent(raw); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ParticipationEvent){
		"missing": func(e *ParticipationEvent) { e.Snapshots = e.Snapshots[:1] },
		"extra": func(e *ParticipationEvent) {
			e.Snapshots = append(e.Snapshots, GrantSnapshot{LID: NewID(), Fingerprint: e.Grant[0].Fingerprint, Hash: strings.Repeat("b", 64)})
		},
		"duplicate":    func(e *ParticipationEvent) { e.Snapshots[1] = e.Snapshots[0] },
		"invalid hash": func(e *ParticipationEvent) { e.Snapshots[0].Hash = "bad" },
		"whole chat":   func(e *ParticipationEvent) { e.Topic = nil },
		"public scope": func(e *ParticipationEvent) { e.Type = EventScope },
	} {
		t.Run(name, func(t *testing.T) {
			var changed ParticipationEvent
			json.Unmarshal(raw, &changed)
			mutate(&changed)
			if changed.Validate() == nil {
				t.Fatal("malformed snapshot accepted")
			}
		})
	}
	e.Snapshots[0].Hash = strings.Repeat("b", 64)
	if e.Verify(vecKey().Public().(ed25519.PublicKey)) == nil {
		t.Fatal("changed content retained authority")
	}
}
