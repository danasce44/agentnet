package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/client"
	"github.com/misunders2d/agentnet/internal/protocol"
	"github.com/misunders2d/agentnet/internal/testhub"
)

// Browser-authored signed/encrypted invitation and inert excerpts through a
// localhost relay into the actual native receiver. No request/model is run.
func TestBrowserMovedTopicInvitationNativeReceiver(t *testing.T) {
	t.Setenv("AGENTNET_NOTIFY", "off")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "hub")
	hub := testhub.Start(t, dir, "127.0.0.1:0", "")
	a, err := client.Join(ctx, filepath.Join(t.TempDir(), "native"), testhub.BootstrapCode(t, dir), "desk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err = a.CreatePerson(ctx, "Native receiver"); err != nil {
		t.Fatal(err)
	}
	if err = a.SetResponder(&client.Responder{Harness: "claude", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	runDaemon(t, a)
	w := startEngineNode(t, dir)
	w.ok(map[string]any{"op": "init", "base": "https://" + hub.Addr})
	code, err := a.Invite(ctx, "browser", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	w.ok(map[string]any{"op": "join", "code": browserCode(t, code), "name": "phone"})
	w.api("/api/person", map[string]any{"label": "Browser sender"})
	w.ok(map[string]any{"op": "start"})
	conv := w.api("/api/dm/new", map[string]any{"address": a.Address})["id"].(string)
	sent := w.api("/api/dm/send", map[string]any{"id": protocol.NewID(), "conv": conv, "body": "Exact reviewed browser source"})
	w.ok(map[string]any{"op": "flush"})
	waitFor(t, "native original received before review", func() bool {
		messages, e := a.ConversationMessages(conv)
		if e != nil {
			return false
		}
		for _, m := range messages {
			if m.LID == sent["lid"].(string) {
				return true
			}
		}
		return false
	})
	topic := protocol.NewID()
	review := w.api("/api/topic/organization/preview", map[string]any{"conv": conv, "ids": []string{sent["lid"].(string)}, "topic": topic, "new": true, "title": "Reviewed destination"})
	w.api("/api/topic/organization/apply", review)
	invited := w.api("/api/dm/agent/invite", map[string]any{"conv": conv, "host": a.Address, "share": []string{sent["id"].(string)}, "topic": topic})
	pid := invited["pid"].(string)
	waitFor(t, "browser exact snapshot admitted by native receiver", func() bool {
		c, e := a.ParticipationContext(pid, 0)
		return e == nil && c.Missing == 0 && len(c.Messages) == 1 && c.Messages[0].Body == "Exact reviewed browser source"
	})
	p, err := a.Participation(pid)
	if err != nil || len(p.Snapshots) != 1 || p.Topic == nil || *p.Topic != topic {
		t.Fatalf("native signed scope/snapshot=%+v %v", p, err)
	}
	c, err := a.ParticipationContext(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Messages[0].Topic != "" || !c.Messages[0].History || c.Addressed != 0 {
		t.Fatalf("original scope or inert context changed: %+v", c)
	}
}
