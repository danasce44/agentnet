package client

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestRevokedRelayRetainsPreviouslyAcceptedOrdinary(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown_key", true: "pinned_key"}[pinned], func(t *testing.T) {
			w := newWorld(t, "")
			a, b := w.alice, w.bob
			if pinned {
				if err := a.store.pin(b.Self()); err != nil {
					t.Fatal(err)
				}
			}
			env := sealTo(t, b, a, envelope.Inner{Kind: envelope.KindMessage, Body: "previously accepted"})
			request := envelope.Inner{Kind: envelope.KindQuestion, Body: "request", Target: &envelope.Target{Address: b.Address, Fingerprint: b.Self().Fingerprint(), AgentID: protocol.NewID()}}
			req := sealTo(t, a, b, request)
			request.ID, request.From, request.To, request.TS = req.ID, req.From, req.To, req.TS
			if err := a.store.addOutbox(req, request, "", nil); err != nil {
				t.Fatal(err)
			}
			answer := sealTo(t, b, a, envelope.Inner{Kind: envelope.KindAnswer, ReplyTo: req.ID, AgentID: request.Target.AgentID, Status: envelope.StatusDone, Body: "completed answer"})
			if !pinned {
				if _, err := a.store.db.Exec(`DELETE FROM peers WHERE address=?`, b.Address); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Revoke(tctx(t), b.Address); err != nil {
				t.Fatal(err)
			}
			if pinned {
				if err := a.store.endRecipient(b.Address, b.Self().Fingerprint(), true); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); a.Run(ctx, RunOptions{HumanOnly: true}) }()
			defer func() { cancel(); <-done }()
			eventually(t, "preaccepted ordinary relay carriers", func() bool { return inboxHas(t, a, env.ID) && inboxHas(t, a, answer.ID) })
			known, err := revokedDeviceKnown(a.store.db, b.Address, b.Self().Fingerprint())
			if err != nil || !known {
				t.Fatalf("revocation evidence missing: %v %v", known, err)
			}
			// Identical content delivered directly has no custody provenance.
			in := envelope.Inner{ID: protocol.NewID(), From: b.Address, To: a.Address, TS: time.Now().Unix(), Kind: envelope.KindMessage, Body: "direct after removal"}
			recipient, _ := a.Self().Recipient()
			direct, err := envelope.Seal(in, b.id.Sign, recipient)
			if err != nil {
				t.Fatal(err)
			}
			if err = a.verifyAndStore(tctx(t), direct); err != nil {
				t.Fatal(err)
			}
			if inboxHas(t, a, direct.ID) || heldReason(t, a, direct.ID) != reasonInvalid {
				t.Fatal("direct revoked carrier admitted")
			}
		})
	}
}

func TestRevokedRelayExceptionRejectsControlAndChildTokens(t *testing.T) {
	w := newWorld(t, "")
	a, b := w.alice, w.bob
	if err := a.store.pin(b.Self()); err != nil {
		t.Fatal(err)
	}
	if err := a.store.endRecipient(b.Address, b.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	recipient, _ := a.Self().Recipient()
	for _, in := range []envelope.Inner{
		{Kind: envelope.KindTask, Body: "must not run"},
		{Kind: envelope.KindMessage, Status: "review_notice", Body: "control"},
		{Kind: envelope.KindAnswer, ReplyTo: protocol.NewID(), Status: envelope.StatusProposal, Body: "proposal"},
		{Kind: envelope.KindAnswer, Status: envelope.StatusDone, TopicDone: true, Body: "topic action"},
	} {
		in.ID, in.From, in.To, in.TS = protocol.NewID(), b.Address, a.Address, time.Now().Unix()
		env, err := envelope.Seal(in, b.id.Sign, recipient)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.storeReceived(tctx(t), env); err != nil {
			t.Fatal(err)
		}
		if inboxHas(t, a, env.ID) || heldReason(t, a, env.ID) != reasonInvalid {
			t.Fatalf("control admitted: %s/%s", in.Kind, in.Status)
		}
	}
	in := envelope.Inner{V: envelope.Version, Kind: envelope.KindMessage}
	allowed, err := a.revokedIngressAllowed(withRelayIngress(tctx(t), "parent"), envelope.Envelope{ID: "child", From: b.Address}, in, b.Self())
	if err != nil || allowed {
		t.Fatalf("parent provenance leaked: %v %v", allowed, err)
	}
}

func TestRelayRedeliveryUpgradesOnlyExactSavedHold(t *testing.T) {
	w := newWorld(t, "")
	a, b := w.alice, w.bob
	env := sealTo(t, b, a, envelope.Inner{Kind: envelope.KindMessage, Body: "held"})
	raw, _ := json.Marshal(env)
	if err := a.store.quarantineDiagnostic(env.ID, env.From, reasonProof, raw, "proof unavailable", ""); err != nil {
		t.Fatal(err)
	}
	changed := env
	changed.TS++
	if err := a.storeReceived(tctx(t), changed); err != nil {
		t.Fatal(err)
	}
	var marked bool
	if err := a.store.db.QueryRow(`SELECT relay_ingress FROM quarantine WHERE id=?`, env.ID).Scan(&marked); err != nil || marked {
		t.Fatalf("different ciphertext upgraded hold: %v %v", marked, err)
	}
	if err := a.storeReceived(tctx(t), env); err != nil {
		t.Fatal(err)
	}
	if err := a.store.db.QueryRow(`SELECT relay_ingress FROM quarantine WHERE id=?`, env.ID).Scan(&marked); err != nil || !marked {
		t.Fatalf("exact relay redelivery not saved: %v %v", marked, err)
	}
}

func TestRevokedRelayHeldProofRetryPreservesProvenance(t *testing.T) {
	w, a, turn := dmToFresh(t)
	// The fixture is manually driven after its stream stops; join all HTTP
	// workers before installing the deterministic proof failure transport.
	a.stopBackgroundPosts()
	a.stopArchivePosts()
	answer := answerHub(a, 404, `{"error":"proof unavailable"}`)
	_, name, _ := protocol.SplitAddress(w.bob.Address)
	answer("/" + name + "/profile")
	relay, direct := turn("retained relay turn"), turn("direct turn")
	if err := a.storeReceived(tctx(t), relay); err != nil {
		t.Fatal(err)
	}
	if err := a.verifyAndStore(tctx(t), direct); err != nil {
		t.Fatal(err)
	}
	if heldReason(t, a, relay.ID) != reasonProof || heldReason(t, a, direct.ID) != reasonProof {
		t.Fatal("fixture did not hold both for proof")
	}
	answer("")
	if _, err := a.personOfKey(tctx(t), w.bob.Address, w.bob.Self()); err != nil {
		t.Fatal(err)
	}
	if err := w.alice.Revoke(tctx(t), w.bob.Address); err != nil {
		t.Fatal(err)
	}
	if err := a.store.endRecipient(w.bob.Address, w.bob.Self().Fingerprint(), true); err != nil {
		t.Fatal(err)
	}
	// Read provenance from the durable hold, rather than retaining either
	// caller's context. The retry itself has no relay token.
	a.retryProof(tctx(t))
	if !inboxHas(t, a, relay.ID) {
		t.Fatalf("relay proof retry lost accepted turn: %s", heldReason(t, a, relay.ID))
	}
	if inboxHas(t, a, direct.ID) || heldReason(t, a, direct.ID) != reasonInvalid {
		t.Fatal("direct hold gained relay authority on retry")
	}
}

func TestRecipientLifecycleActiveReportOnlyDiscoversRevokedHost(t *testing.T) {
	w := newWorld(t, "")
	a, b := w.alice, w.bob
	body := reviewAlertBody(t, b.Address, reviewAlertItem(b))
	id := insertReviewAlert(t, a, b.Address, b.Self().Fingerprint(), body)
	if err := a.Revoke(tctx(t), b.Address); err != nil {
		t.Fatal(err)
	}
	members, err := a.Members(tctx(t))
	if err != nil {
		t.Fatal(err)
	}
	members.Members = []protocol.Member{{Address: a.Address, Presence: protocol.PresenceOffline}}
	view, _ := json.Marshal(members)
	a.onMembers(view)
	if err := a.reconcileRecipientLifecycle(tctx(t)); err != nil {
		t.Fatal(err)
	}
	known, err := revokedDeviceKnown(a.store.db, b.Address, b.Self().Fingerprint())
	if err != nil || !known {
		t.Fatalf("report-only host not discovered: %v %v", known, err)
	}
	notices, err := a.Notices()
	if err != nil || len(notices) != 0 {
		t.Fatalf("removed report still active: %d %v", len(notices), err)
	}
	page, err := a.PageReview()
	if err != nil || len(page.Device) != 0 {
		t.Fatalf("removed report still in review: %d %v", len(page.Device), err)
	}
	var saved string
	if err = a.store.db.QueryRow(`SELECT body FROM inbox WHERE id=?`, id).Scan(&saved); err != nil || saved != body {
		t.Fatal("history erased")
	}
	seq, _ := a.Changed()
	if err = a.reconcileRecipientLifecycle(tctx(t)); err != nil {
		t.Fatal(err)
	}
	if after, _ := a.Changed(); after != seq {
		t.Fatal("retained report caused repeated lifecycle write")
	}
}
