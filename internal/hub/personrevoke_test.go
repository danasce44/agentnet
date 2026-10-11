package hub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestPersonRevokeReportsExactDevices(t *testing.T) {
	h, boss := adminHub(t)
	owner := joinMember(t, h, "owner")
	person := personOf(t, h, owner)
	phone := adminLinkedDevice(t, h, owner, &person, "owner/phone")
	// A separately enrolled laptop can leave its person without losing its
	// workspace membership. A later person offboard must not claim to revoke it.
	if code, body := put(t, h, phone, step(person, phone, nil, phone.id.Public(phone.addr))); code != 204 {
		t.Fatalf("detach standalone: %d %+v", code, body)
	}
	var audit string
	h.cfg.Logf = func(format string, args ...any) { audit = fmt.Sprintf(format, args...) }
	for i, want := range [][]string{{phone.addr}, {}} {
		code, body := boss.call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: person.Person})
		var response protocol.RevokePersonResponse
		if code != 200 || json.Unmarshal(body, &response) != nil || !slices.Equal(response.Revoked, want) || response.Revoked == nil {
			t.Fatalf("exact response attempt %d: %d %s", i, code, body)
		}
		if !strings.Contains(audit, person.Person) || !strings.Contains(audit, boss.addr) || !strings.Contains(audit, fmt.Sprintf("(%d devices)", len(want))) {
			t.Fatalf("missing bounded audit: %q", audit)
		}
	}
	standalone, err := h.store.agent(owner.addr)
	if err != nil || standalone.Revoked || standalone.Person != "" {
		t.Fatalf("unclaimed standalone changed: %+v %v", standalone, err)
	}
	if code, body := boss.call(t, h, "POST", "/v1/admin/revoke", protocol.RevokeRequest{Address: owner.addr}); code != 200 {
		t.Fatalf("explicit standalone removal: %d %s", code, body)
	}
}

func TestPersonRevokeConflictingIdentity(t *testing.T) {
	h, boss := adminHub(t)
	owner := joinMember(t, h, "owner")
	person := personOf(t, h, owner)
	if _, err := h.store.db.Exec(`UPDATE persons SET record=? WHERE person=?`, []byte(`{}`), person.Person); err != nil {
		t.Fatal(err)
	}
	if code, body := boss.call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: person.Person}); code != 409 || !strings.Contains(string(body), "no devices were revoked") {
		t.Fatalf("conflicting identity: %d %s", code, body)
	}
	member, err := h.store.agent(owner.addr)
	if err != nil || member.Revoked {
		t.Fatalf("refused transaction changed device: %+v %v", member, err)
	}
}
