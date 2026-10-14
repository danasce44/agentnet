package ui

import "slices"

// Only locally actionable stopped agent children read their causal references.
// Human, running and remote rows perform no extra reads here. This negative
// hint never establishes permission: Accept still does full click-time checks.
func (l *Live) agentRequestActions(conv, id, kind, state, detail string, agentAuthor bool) ([]string, string) {
	actions := AgentActions(kind, state)
	if !agentAuthor || (state != "interrupted" && state != "needs_human") || !slices.Contains(actions, DoAccept) {
		return actions, detail
	}
	why, err := l.a.AgentRetryBlocker(conv, id)
	if err != nil {
		why = "The original request evidence is unavailable; retry cannot be confirmed"
	}
	if why != "" {
		actions = slices.DeleteFunc(actions, func(action string) bool { return action == DoAccept })
		if detail != "" {
			detail += "\n"
		}
		detail += "Cannot run this request again: " + why + ". Mark it as handled if no more work is needed."
	}
	return actions, detail
}
