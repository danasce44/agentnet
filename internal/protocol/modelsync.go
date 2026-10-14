package protocol

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const CapModelSync = "mdl1"
const CapModelSyncV2 = "mdl2" // reasoning metadata; new readers also read mdl1

// AgentModel is a host's last report, not a configuration or permission.
// Executor is a digest of the exact locally selected responder, never its paths.
type AgentModel struct {
	AgentID   string `json:"agent_id"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning,omitempty"`
	Harness   string `json:"harness"`
	Executor  string `json:"executor"`
	At        int64  `json:"at"`
	Revision  int64  `json:"revision"`
}
type ModelSync struct {
	V       int          `json:"v"`
	Person  string       `json:"person"`
	Roster  string       `json:"roster"`
	Reports []AgentModel `json:"reports"`
}

func ValidReportedModel(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 120 && name == strings.TrimSpace(name) && !strings.ContainsFunc(name, unicode.IsControl)
}
func ValidReportedReasoning(value string) bool {
	return ValidReportedModel(value) && utf8.RuneCountInString(value) <= 32
}
func ParseModelSync(raw []byte) (ModelSync, error) {
	var r ModelSync
	if len(raw) > 65536 || decodeStrictJSON(raw, &r) != nil || r.V != 1 && r.V != 2 || !ValidID(r.Person) || !ValidHash(r.Roster) || len(r.Reports) < 1 || len(r.Reports) > 64 {
		return r, errors.New("model report: invalid owner or snapshot")
	}
	if r.V == 1 {
		// Preserve the shipped strict schema, including rejection of an
		// explicitly present empty/null reasoning field. mdl1 never read it.
		var legacy struct {
			V       int    `json:"v"`
			Person  string `json:"person"`
			Roster  string `json:"roster"`
			Reports []struct {
				AgentID  string `json:"agent_id"`
				Model    string `json:"model"`
				Harness  string `json:"harness"`
				Executor string `json:"executor"`
				At       int64  `json:"at"`
				Revision int64  `json:"revision"`
			} `json:"reports"`
		}
		if decodeStrictJSON(raw, &legacy) != nil {
			return r, errors.New("model report: invalid legacy snapshot")
		}
	}
	seen := map[string]bool{}
	for _, m := range r.Reports {
		if m.AgentID != "" && !ValidID(m.AgentID) || !ValidReportedModel(m.Model) || m.Reasoning != "" && !ValidReportedReasoning(m.Reasoning) || !ValidReportedModel(m.Harness) || len(m.Harness) > 32 || !ValidHash(m.Executor) || m.At < 1 || m.At > MaxTopicTitleRevision || m.Revision < 1 || m.Revision > MaxTopicTitleRevision || seen[m.AgentID] {
			return r, errors.New("model report: invalid or duplicate agent")
		}
		seen[m.AgentID] = true
	}
	return r, nil
}
