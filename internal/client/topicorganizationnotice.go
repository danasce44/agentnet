package client

import (
	"database/sql"
	"errors"
	"strings"
)

// Names are presentation. The signed Topic remains the destination identity.
func TopicOrganizationNotice(topic, title string) string {
	if topic == "" {
		return "Moved selected messages to Main flow."
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "Moved selected messages to topic " + topic + "."
	}
	return "Moved selected messages to “" + title + "”."
}

func (a *Agent) TopicOrganizationNotice(conv, topic, title string) (string, error) {
	if topic == "" {
		return TopicOrganizationNotice(topic, ""), nil
	}
	if title == "" {
		err := a.store.db.QueryRow(`SELECT coalesce(title,'') FROM topic_state WHERE peer=? AND topic=?`, conv, topic).Scan(&title)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return TopicOrganizationNotice(topic, title), nil
}
