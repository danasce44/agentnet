package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/client"
)

func TestRoomSendWithoutIncomingRequest(t *testing.T) {
	a, b, _, conv, wait := dmWorld(t)
	if err := a.SetResponder(&client.Responder{Harness: "claude", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	p, err := a.InviteAgent(context.Background(), conv, a.Address, nil, nil, "")
	if err != nil || !p.Claimable() {
		t.Fatalf("own active agent: %+v %v", p, err)
	}
	runEnv(t, "1", "", "", "")
	t.Setenv(client.RoomRequestEnv, "")
	var out bytes.Buffer
	if err := runRoom(context.Background(), a, []string{"send", "--pid", p.PID, "Proactive CLI update"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(out.String())) < 2 {
		t.Fatalf("missing persisted message ID/delivery state: %s", &out)
	}
	wait("ordinary agent message", func() bool {
		msgs, err := b.ConversationMessages(conv)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Body == "Proactive CLI update" {
				return m.VerifiedAgent && m.PID == p.PID && m.ReplyTo == ""
			}
		}
		return false
	})
	t.Setenv(replyBindingEnv, "exact-delegated-binding")
	if err := runRoom(context.Background(), nil, []string{"send", "--pid", p.PID, "escape binding"}, &out); err == nil || !strings.Contains(err.Error(), refusedText) {
		t.Fatalf("delegated routing escaped: %v", err)
	}
}

func TestP6RoomCLIRequiresRunningRequest(t *testing.T) {
	t.Setenv(client.RoomRequestEnv, "")
	for _, args := range [][]string{{"ask", "--pid", "unused", "question"}, {"wait", "unused"}} {
		err := runRoom(context.Background(), nil, args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "running group agent request") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
