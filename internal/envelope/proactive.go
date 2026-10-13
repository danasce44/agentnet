package envelope

// ProactiveAgent is an ordinary unsolicited agent turn, not a request or a
// result. The marker supplies no authority: admission checks its exact host,
// participation, topic and captured audience against current signed evidence.
func ProactiveAgent(in Inner) bool {
	return in.Kind == KindMessage && in.Sub == "" && in.Status == "" && in.ReplyTo == "" && in.Target == nil && in.Conv != "" && in.PID != "" && in.Origin == OriginAgentPrefix+"proactive" && in.ReceiverRoute == nil
}
