package client

import (
	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// OrganizationLink reads the exact stored result. It never changes authority,
// guesses a newly created topic from the request, or contacts the relay.
func (a *Agent) OrganizationLink(sent ConvSent) (string, error) {
	realm, err := a.RealmID()
	if err != nil {
		return "", err
	}
	var conv, topic string
	err = a.store.db.QueryRow(`SELECT conv,coalesce(topic,'') FROM outbox WHERE id=? AND lid=? AND origin=?`, sent.ID, sent.LID, envelope.OriginAgentOrganization).Scan(&conv, &topic)
	if err != nil {
		return "", err
	}
	if topic == "" {
		topic = "main"
	}
	return protocol.ConversationLink(realm, conv, topic)
}
