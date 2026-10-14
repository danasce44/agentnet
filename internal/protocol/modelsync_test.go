package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelSyncStrictReportedMetadata(t *testing.T) {
	report := AgentModel{AgentID: NewID(), Model: "gpt-6.1-sol", Harness: "codex", Executor: strings.Repeat("a", 64), At: 1, Revision: 1}
	record := ModelSync{V: 1, Person: NewID(), Roster: strings.Repeat("b", 64), Reports: []AgentModel{report}}
	raw, _ := json.Marshal(record)
	if _, err := ParseModelSync(raw); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", " leading", "trailing ", "foo\nbar", "foo\x85bar", strings.Repeat("x", 121), string([]byte{0xff})} {
		if ValidReportedModel(name) {
			t.Fatalf("accepted invalid model %q", name)
		}
	}
	for _, name := range []string{"gpt-6.1-sol", "Claude Opus 4.6", strings.Repeat("型", 120)} {
		if !ValidReportedModel(name) {
			t.Fatalf("rejected report %q", name)
		}
	}
	for _, bad := range []string{strings.Replace(string(raw), `"model":"gpt-6.1-sol"`, `"model":"gpt-6.1-sol","effort":"high"`, 1), strings.Replace(string(raw), `"revision":1`, `"revision":0`, 1), strings.Replace(string(raw), `"at":1`, `"at":9007199254740992`, 1)} {
		if _, err := ParseModelSync([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	record.Reports = append(record.Reports, report)
	raw, _ = json.Marshal(record)
	if _, err := ParseModelSync(raw); err == nil {
		t.Fatal("duplicate exact agent")
	}
	record.Reports = []AgentModel{report}
	record.Reports[0].AgentID = ""
	raw, _ = json.Marshal(record)
	if _, err := ParseModelSync(raw); err != nil {
		t.Fatal("default agent", err)
	}
}

func TestModelSyncReasoningCompatibility(t *testing.T) {
	m := AgentModel{Model: "gpt-6.1-sol", Reasoning: "xhigh", Harness: "codex", Executor: strings.Repeat("a", 64), At: 1, Revision: 1}
	r := ModelSync{V: 2, Person: NewID(), Roster: strings.Repeat("b", 64), Reports: []AgentModel{m}}
	raw, _ := json.Marshal(r)
	got, err := ParseModelSync(raw)
	if err != nil || got.Reports[0].Reasoning != "xhigh" {
		t.Fatalf("reasoning: %+v %v", got, err)
	}
	for _, value := range []string{"", " leading", "high\n", strings.Repeat("型", 33), "bad\x85effort"} {
		if ValidReportedReasoning(value) {
			t.Fatalf("accepted reasoning %q", value)
		}
	}
	for _, value := range []string{"xhigh", "none", strings.Repeat("型", 32)} {
		if !ValidReportedReasoning(value) {
			t.Fatalf("rejected reasoning %q", value)
		}
	}
	for _, field := range []string{`"reasoning":"xhigh",`, `"reasoning":"",`, `"reasoning":null,`} {
		legacy := strings.Replace(strings.Replace(string(raw), `"v":2`, `"v":1`, 1), `"reasoning":"xhigh",`, field, 1)
		if _, err := ParseModelSync([]byte(legacy)); err == nil {
			t.Fatalf("legacy accepted %s", legacy)
		}
	}
	r.V = 1
	r.Reports[0].Reasoning = ""
	raw, _ = json.Marshal(r)
	if strings.Contains(string(raw), "reasoning") {
		t.Fatal("legacy canonical vector changed")
	}
	if _, err := ParseModelSync(raw); err != nil {
		t.Fatal(err)
	}
	if !(CapsRecord{Caps: []string{CapModelSyncV2}}).Reads(CapModelSync) || (CapsRecord{Caps: []string{CapModelSync}}).Reads(CapModelSyncV2) {
		t.Fatal("capability boundary")
	}
	for _, raw := range [][]byte{append(make([]byte, 65537), raw...), []byte(strings.Replace(string(raw), `"v":1`, `"v":3`, 1))} {
		if _, err := ParseModelSync(raw); err == nil {
			t.Fatal("accepted invalid size/version")
		}
	}
}
