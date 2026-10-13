package envelope

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestCompactLocalTopicRequestProof(t *testing.T) {
	id, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	key := id.Public("owner/host")
	proof := TopicRequestProof{V: 1, Conv: strings.Repeat("a", 64), LID: protocol.NewID(), PID: protocol.NewID(), Author: protocol.EventAuthor{Person: protocol.NewID(), Roster: strings.Repeat("b", 64), Address: key.Address, Fingerprint: key.Fingerprint()}, Target: Target{Address: key.Address, Fingerprint: key.Fingerprint(), AgentID: protocol.NewID()}, Hash: strings.Repeat("c", 64), Kind: KindTask}
	proof.Sign(id.Sign)
	if err := proof.Verify(key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(proof.Canonical()), TopicRequestDomain) {
		t.Fatal("signature domain absent")
	}
	for name, mutate := range map[string]func(*TopicRequestProof){
		"chat":          func(p *TopicRequestProof) { p.Conv = strings.Repeat("d", 64) },
		"request":       func(p *TopicRequestProof) { p.LID = protocol.NewID() },
		"participation": func(p *TopicRequestProof) { p.PID = protocol.NewID() },
		"content":       func(p *TopicRequestProof) { p.Hash = strings.Repeat("d", 64) },
		"target":        func(p *TopicRequestProof) { p.Target.AgentID = protocol.NewID() },
		"kind":          func(p *TopicRequestProof) { p.Kind = KindQuestion },
		"topic":         func(p *TopicRequestProof) { p.Topic = protocol.NewID() },
		"foreign human": func(p *TopicRequestProof) { p.Author.Address = "other/phone" },
	} {
		t.Run(name, func(t *testing.T) {
			p := proof
			mutate(&p)
			if p.Verify(key) == nil {
				t.Fatal("forged request accepted")
			}
		})
	}
	other, _ := identity.Generate()
	p := proof
	p.Sign(other.Sign)
	if p.Verify(key) == nil {
		t.Fatal("another host re-signed as human")
	}
	raw, _ := json.Marshal(proof)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"body", "files", "attachments", "quote", "history"} {
		if _, ok := fields[private]; ok {
			t.Fatalf("private field %s disclosed", private)
		}
	}
	if len(raw) > 1200 {
		t.Fatalf("compact proof grew to %d", len(raw))
	}
}
