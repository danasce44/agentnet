package hub

import (
	"net/http"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestRevocationForcesUnchangedMemberDiscovery(t *testing.T) {
	h, admin := adminHub(t)
	target := joinMember(t, h, "target")
	before := h.againGen.Load()
	code, body := admin.call(t, h, "POST", "/v1/admin/revoke", protocol.RevokeRequest{Address: target.addr})
	if code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, body)
	}
	// The revoked key can be outside the serialized member-list limit. A
	// normal generation wake alone would suppress that unchanged snapshot.
	if h.againGen.Load() <= before {
		t.Fatal("revocation did not force member discovery when serialized members stay unchanged")
	}
}
