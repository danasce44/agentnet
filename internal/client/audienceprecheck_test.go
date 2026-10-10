package client

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

// copyStates are the states of a's outbox copies to recipient whose column
// (pid or lid) is value, with their details.
func copyStates(t *testing.T, a *Agent, column, value, recipient string) (states, details []string) {
	t.Helper()
	rows, err := a.store.db.Query(`SELECT state, coalesce(error,'') FROM outbox WHERE recipient=? AND `+column+`=? ORDER BY rowid`, recipient, value)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s, d string
		if err := rows.Scan(&s, &d); err != nil {
			t.Fatal(err)
		}
		states, details = append(states, s), append(details, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return states, details
}

// heldForUpdate fails unless the older device's copies are kept and none
// was handed over: each waits for its reader to update, or (a later turn)
// stays queued behind one that does.
func heldForUpdate(t *testing.T, what string, states, details []string) {
	t.Helper()
	if len(states) == 0 {
		t.Fatalf("%s: no copy kept for the older device (it must wait, never be dropped)", what)
	}
	for i, s := range states {
		if s != stateConvWaiting && s != stateQueued || s == stateConvWaiting && !strings.HasPrefix(details[i], WaitPeerUpdate) {
			t.Fatalf("%s: copy %s (%s): an older reader got what it cannot enforce", what, s, details[i])
		}
	}
}

// releaseTo releases and sends a's waiting copies (a's daemon is stopped).
func releaseTo(t *testing.T, a *Agent) {
	t.Helper()
	feats, err := a.relayFeatures(tctx(t))
	if err != nil {
		t.Fatal(err)
	}
	a.releaseConv(tctx(t), feats)
	if err := a.FlushOutbox(tctx(t)); err != nil {
		t.Fatal(err)
	}
}

// CG-4: a topic-scoped invitation asks tps1 of the invited host only. A
// member's device that is suspended until it updates AgentNet, or merely
// older, stops nobody: its copies of the scoped records wait for it (it
// holds no record of the participation and admits nothing from it), and
// they reach it once it updates. The host's requirement stays hard,
// suspended or not.
func TestTopicInviteOlderOrSuspendedMemberDeviceStopsNobody(t *testing.T) {
	w := newWorld(t, "")
	stopAlice := runAgent(t, w.alice)
	runAgent(t, w.bob)
	persons(t, w.alice, w.bob)
	conv := newDM(t, w.alice, w.bob)
	guest := mustJoin(t, filepath.Join(t.TempDir(), "guest"), w.aliceInvites("guest"), "guest")
	runAgent(t, guest)
	persons(t, guest)
	for _, a := range []*Agent{w.alice, w.bob, guest} {
		fakeNotify(a)
		humanTestCaps(t, a)
	}
	ctx := tctx(t)
	topic, other := protocol.NewID(), protocol.NewID()
	selected, err := w.alice.SendConv(ctx, conv, ConvOutgoing{Body: "ONLY_SELECTED_TOPIC", Topic: topic})
	if err != nil {
		t.Fatal(err)
	}
	signCapsAfter(t, w.bob, without(ownCaps, protocol.CapTopicParticipation))
	stopAlice() // no relay member list replaces the ones this test gives alice
	everyone := []string{w.alice.Address, w.bob.Address, guest.Address}

	// The invited host must enforce the topic, suspended or not.
	signCapsAfter(t, guest, without(ownCaps, protocol.CapTopicParticipation))
	listMembers(t, w.alice, map[string]bool{w.bob.Address: true, guest.Address: true}, everyone...)
	var nu *NeedsUpdateError
	if _, err = w.alice.InviteHumanInScope(ctx, conv, guest.Address, []string{selected.LID}, "", &topic); !errors.As(err, &nu) || nu.Address != guest.Address {
		t.Fatalf("older suspended host invited to a topic: %v", err)
	}
	signCapsAfter(t, guest, ownCaps)

	// Bob's device suspended until it updates: the invitation goes.
	listMembers(t, w.alice, map[string]bool{w.bob.Address: true}, everyone...)
	p, err := w.alice.InviteHumanInScope(ctx, conv, guest.Address, []string{selected.LID}, "", &topic)
	if err != nil {
		t.Fatalf("one suspended member device stopped a topic invitation: %v", err)
	}
	eventually(t, "guest invited", func() bool { return stateAt(t, guest, p.PID).State == PartInvited })
	states, details := copyStates(t, w.alice, "pid", p.PID, w.bob.Address)
	heldForUpdate(t, "suspended member", states, details)

	// Bob's device older but listed current: the same, each copy waits.
	listMembers(t, w.alice, nil, everyone...)
	q, err := w.alice.InviteHumanInScope(ctx, conv, guest.Address, nil, "", &other)
	if err != nil {
		t.Fatalf("one older member device stopped a topic invitation: %v", err)
	}
	eventually(t, "guest invited to the other topic", func() bool { return stateAt(t, guest, q.PID).State == PartInvited })
	states, details = copyStates(t, w.alice, "pid", q.PID, w.bob.Address)
	heldForUpdate(t, "older member", states, details)
	if stateAt(t, w.bob, p.PID).Invite != "" || stateAt(t, w.bob, q.PID).Invite != "" {
		t.Fatal("an older member device holds a topic participation it cannot enforce")
	}

	// Bob updates: what waited for him reaches him.
	signCapsAfter(t, w.bob, ownCaps)
	releaseTo(t, w.alice)
	for _, pid := range []string{p.PID, q.PID} {
		eventually(t, "updated member learns the scoped participation", func() bool { return stateAt(t, w.bob, pid).Invite != "" })
	}
}

// CG-5: a topic move or merge asks only its author. A member's device that
// is suspended until it updates AgentNet, or merely older, stops nobody:
// the others see the move now, its copy waits, and it reaches the device
// once it updates. Nothing is handed to a reader without org1.
func TestTopicOrganizationOlderOrSuspendedMemberDeviceStopsNobody(t *testing.T) {
	w, carol, packet, stops := groupTurnsFixture(t)
	ctx, conv := tctx(t), packet.State.Conv
	send := func(body string) (ConvSent, string) {
		t.Helper()
		sent, err := w.alice.SendConv(ctx, conv, ConvOutgoing{Body: body, Topic: "new"})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := convMsg(t, w.alice, conv, func(m ConvMessage) bool { return m.LID == sent.LID })
		return sent, m.Topic
	}
	first, _ := send("Selected while carol is suspended")
	second, _ := send("Selected while carol is older")
	_, to := send("Destination")
	for _, a := range []*Agent{w.bob, carol} {
		eventually(t, "members hold the originals", func() bool {
			m, e := a.ConversationMessages(conv)
			return e == nil && len(m) >= 3
		})
	}
	signCapsAfter(t, carol, without(ownCaps, protocol.CapTopicOrganization))
	stops[w.alice]() // no relay member list replaces the ones this test gives alice
	everyone := []string{w.alice.Address, w.bob.Address, carol.Address}
	move := func(lid string) ConvSent {
		t.Helper()
		review, err := w.alice.PreviewTopicOrganization(TopicOrganizationRequest{Conv: conv, IDs: []string{lid}, Topic: to})
		if err != nil {
			t.Fatal(err)
		}
		moved, err := w.alice.ApplyTopicOrganization(ctx, review)
		if err != nil {
			t.Fatalf("one older or suspended member device stopped a topic move for everyone: %v", err)
		}
		return moved
	}
	moved := func(a *Agent, lid string) bool {
		m, e := a.ConversationMessages(conv)
		return e == nil && ChatTopicAssignments(m)[lid] == to
	}

	listMembers(t, w.alice, map[string]bool{carol.Address: true}, everyone...)
	one := move(first.LID)
	listMembers(t, w.alice, nil, everyone...)
	two := move(second.LID)
	for _, lid := range []string{first.LID, second.LID} {
		eventually(t, "current member sees the move", func() bool { return moved(w.bob, lid) })
	}
	if err := w.alice.FlushOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	states, details := copyStates(t, w.alice, "lid", one.LID, carol.Address)
	if len(states) != 1 || states[0] != stateConvWaiting {
		t.Fatalf("suspended member's copy of the move: %v %v", states, details)
	}
	for _, op := range []ConvSent{one, two} {
		states, details := copyStates(t, w.alice, "lid", op.LID, carol.Address)
		heldForUpdate(t, "older member", states, details)
	}
	if moved(carol, first.LID) || moved(carol, second.LID) {
		t.Fatal("a reader without org1 applied a move")
	}

	signCapsAfter(t, carol, ownCaps)
	releaseTo(t, w.alice)
	for _, lid := range []string{first.LID, second.LID} {
		eventually(t, "updated member sees the move", func() bool { return moved(carol, lid) })
	}
}

// CG-3 (latest-only): a group guest invitation does not ask a member's
// device the relay suspended until it updates AgentNet; its copies wait
// for hgg1. Any other member device without hgg1 still refuses it (and the
// readiness check says so), and the invited host is always asked.
func TestGroupGuestInviteSkipsSuspendedMemberDevice(t *testing.T) {
	w, carol, packet, stops := groupTurnsFixture(t)
	conv := packet.State.Conv
	ctx := tctx(t)
	guest := proofReader(t, w, "guest")
	runAgent(t, guest)
	publishGroupFixtureCaps(t, guest, true)
	for _, a := range []*Agent{w.alice, w.bob, carol, guest} {
		roomReader(t, a)
	}
	oldCaps := slices.DeleteFunc(slices.Clone(ownCaps), func(c string) bool { return c == protocol.CapGroupHumanParticipation })
	signCapsAfter(t, w.bob, oldCaps)
	stops[carol]() // no relay member list replaces the ones this test gives carol
	everyone := []string{w.alice.Address, w.bob.Address, carol.Address, guest.Address}
	ready := func() bool {
		t.Helper()
		support, err := carol.HumanInviteSupport(ctx, conv, guest.Address)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range support {
			if s.State == "update" || s.State == "not set up" {
				return false
			}
		}
		return true
	}

	// Older and listed current: still refused (only suspension is skipped).
	listMembers(t, carol, nil, everyone...)
	if ready() {
		t.Fatal("older current member device marked group-guest ready")
	}
	if _, err := carol.InviteHuman(ctx, conv, guest.Address, nil, "older member"); err == nil {
		t.Fatal("group guest invitation with an older current member device")
	}

	// The invited host is asked, suspended or not.
	signCapsAfter(t, guest, oldCaps)
	listMembers(t, carol, map[string]bool{w.bob.Address: true, guest.Address: true}, everyone...)
	if _, err := carol.InviteHuman(ctx, conv, guest.Address, nil, "older host"); err == nil {
		t.Fatal("older suspended host invited as a group guest")
	}
	roomReader(t, guest)

	// Bob's device suspended until it updates: the invitation goes, and
	// his copies wait for hgg1.
	listMembers(t, carol, map[string]bool{w.bob.Address: true}, everyone...)
	if !ready() {
		t.Fatal("a suspended member device decided group-guest readiness")
	}
	p, err := carol.InviteHuman(ctx, conv, guest.Address, nil, "bob is suspended")
	if err != nil {
		t.Fatalf("one suspended member device stopped a group guest invitation: %v", err)
	}
	eventually(t, "guest invited", func() bool { return stateAt(t, guest, p.PID).State == PartInvited })
	states, details := copyStates(t, carol, "pid", p.PID, w.bob.Address)
	heldForUpdate(t, "suspended member", states, details)
	if stateAt(t, w.bob, p.PID).Invite != "" {
		t.Fatal("a member device without hgg1 holds the guest participation")
	}
}
