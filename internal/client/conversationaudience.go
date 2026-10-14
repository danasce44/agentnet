package client

import (
	"encoding/json"
	"strings"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Keep the independent extension beside the original stored requirement.
// Existing required_cap survives child body erasure and archive repacking.
func conversationAudienceRequirement(required string, needed bool) string {
	if !needed {
		return required
	}
	if _, present := splitConversationAudienceRequirement(required); present {
		return required
	}
	if required == "" {
		return protocol.CapConversationAudience
	}
	return required + "+" + protocol.CapConversationAudience
}

func splitConversationAudienceRequirement(required string) (string, bool) {
	if required == protocol.CapConversationAudience {
		return "", true
	}
	base := strings.TrimSuffix(required, "+"+protocol.CapConversationAudience)
	return base, base != required
}

func conversationAudienceCopy(sub, body, human string) bool {
	var h envelope.HumanTurn
	if human != "" && json.Unmarshal([]byte(human), &h) == nil && h.ConversationAudience() {
		return true
	}
	if sub == envelope.SubHistory && body != "" {
		var item HistoryItem
		return json.Unmarshal([]byte(body), &item) == nil && item.Human != nil && item.Human.ConversationAudience()
	}
	return false
}
