package client

import (
	"database/sql"
	"encoding/json"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Append-only availability/admitted-context metadata, never a received grant.
// These two explicit extension flags survive mixed ciphertext archive/repacking
// even after the local plaintext used to classify a child has been erased.
const agentActionSchema = `
ALTER TABLE inbox ADD COLUMN agent_context TEXT;
ALTER TABLE outbox ADD COLUMN mention_cap INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outbox ADD COLUMN organization_cap INTEGER NOT NULL DEFAULT 0;
`

func storeAgentActionRequirements(tx *sql.Tx, c outCopy) error {
	inner := c.in
	if inner.Sub == envelope.SubHistory && inner.Body != "" {
		var item HistoryItem
		if err := json.Unmarshal([]byte(inner.Body), &item); err != nil {
			return err
		}
		inner = item.inner(inner.Conv)
	}
	mention := c.mentionCap || c.required == protocol.CapMentionReaction || envelope.MentionReaction(inner)
	organization := c.organizationCap || c.required == protocol.CapAgentOrganization || envelope.OrganizationReaderExtension(inner)
	if !mention && !organization {
		return nil
	}
	_, err := tx.Exec(`UPDATE outbox SET mention_cap=?,organization_cap=? WHERE id=?`, mention, organization, c.env.ID)
	return err
}
