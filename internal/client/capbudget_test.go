package client

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestOwnCapabilitySignedByteBudget(t *testing.T) {
	key, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	caps := append(slices.Clone(ownCaps), protocol.CapAgent)
	slices.Sort(caps)
	record := protocol.CapsRecord{Address: strings.Repeat("a", 32) + "/" + strings.Repeat("b", 32), Session: protocol.NewID(), Caps: caps, TS: 1790000000}
	record.Sign(key.Sign)
	if err := record.ValidateAdvertisement(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > protocol.MaxCapsRecord || len(caps) > protocol.MaxAdvertisedCaps || protocol.MaxCaps-len(caps) < 6 {
		t.Fatalf("caps=%d bytes=%d headroom=%d", len(caps), len(raw), protocol.MaxCaps-len(caps))
	}
	parsed, err := protocol.ParseCapsRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = parsed.Verify(key.Public(record.Address).SignKey); err != nil {
		t.Fatal(err)
	}
}
