package protocol

import "errors"

// ConversationLink names a stable destination and grants nothing. Realm is a
// recorded workspace namespace; neither an endpoint nor a local alias is used.
func ConversationLink(realm, conv, topic string) (string, error) {
	if !ValidID(realm) || !ValidHash(conv) || topic != "main" && !ValidID(topic) {
		return "", errors.New("invalid portable conversation destination")
	}
	return "agentnet://open#realm=" + realm + "&conv=" + conv + "&topic=" + topic, nil
}
