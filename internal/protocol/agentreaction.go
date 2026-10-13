package protocol

// CapAgentReaction means a session reads an assistant's reaction (a
// version 3 reaction naming its agent or participation) as the assistant's,
// never as its host person's.
const CapAgentReaction = "agr1"

// An extension, never implied by older rm1: ordinary mentions can carry an
// admitted agent's social reaction without an incoming question or task.
const CapMentionReaction = "amr1"
