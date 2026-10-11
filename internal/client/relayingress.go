package client

import (
	"context"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/identity"
)

type relayIngressKey struct{}

// Bind transport provenance to this exact carrier, not any child imported by
// a handler using the same context. Only the relay ingress boundary sets it.
func withRelayIngress(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, relayIngressKey{}, id)
}
func relayIngress(ctx context.Context, id string) bool {
	got, _ := ctx.Value(relayIngressKey{}).(string)
	return got != "" && got == id
}
func retainedRelayOrdinary(in envelope.Inner) bool {
	if in.V != envelope.Version && in.V != envelope.Version2 {
		return false
	}
	if in.Kind != envelope.KindMessage && in.Kind != envelope.KindAnswer && in.Kind != envelope.KindResult {
		return false
	}
	if in.Kind == envelope.KindMessage && in.Status != "" {
		return false
	}
	if in.Kind == envelope.KindAnswer || in.Kind == envelope.KindResult {
		switch in.Status {
		case "", envelope.StatusDone, envelope.StatusFailed, envelope.StatusTimeout, envelope.StatusCancelled, envelope.StatusDeclined, envelope.StatusInterrupted:
		default:
			return false
		}
	}
	return in.Sub == "" && !in.Replica && in.Ref == nil && in.Target == nil && in.Followup == nil && in.TopicEvent == nil && !in.TopicDone && in.ReceiverRoute == nil
}
func (a *Agent) revokedIngressAllowed(ctx context.Context, env envelope.Envelope, in envelope.Inner, sender identity.Public) (bool, error) {
	revoked, err := revokedDeviceKnown(a.store.db, env.From, sender.Fingerprint())
	if err != nil {
		return false, err
	}
	return !revoked || relayIngress(ctx, env.ID) && retainedRelayOrdinary(in), nil
}
