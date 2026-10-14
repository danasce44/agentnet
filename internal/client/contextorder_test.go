package client

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Local storage milliseconds are presentation data, never an invitation's
// access boundary. Exercise both public context and the native partial path.
func TestRetainedContextClockOrder(t *testing.T) {
	for _, boundary := range []string{"rollback", "equal_millisecond"} {
		t.Run(boundary, func(t *testing.T) {
			w, conv, lids, _ := agentWorld(t)
			early, err := w.bob.SendConv(tctx(t), conv, ConvOutgoing{Body: "EARLIER_LOCAL_UNSHARED"})
			if err != nil {
				t.Fatal(err)
			}
			pid := participate(t, w, conv, lids[:1], nil)
			p := stateAt(t, w.bob, pid)
			later, err := w.bob.SendConv(tctx(t), conv, ConvOutgoing{Body: "PROVEN_LATER_LOCAL_TURN"})
			if err != nil {
				t.Fatal(err)
			}
			// A late physical copy cannot redate its older logical original.
			if _, err = w.bob.store.db.Exec(`INSERT INTO outbox(id,recipient,body,envelope,state,created_at,conv,lid,kind,created_ms) SELECT ?,recipient,body,envelope,'not_delivered',created_at,conv,lid,kind,3000 FROM outbox WHERE id=?`, protocol.NewID(), early.ID); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				`UPDATE inbox SET received_ms=100 WHERE conv=?`,
				`DELETE FROM room_context WHERE conv=?`,
			} {
				if _, err = w.bob.store.db.Exec(query, conv); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = w.bob.store.db.Exec(`UPDATE outbox SET created_ms=1000 WHERE conv=? AND pid=? AND sub=?`, conv, pid, envelope.SubEvent); err != nil {
				t.Fatal(err)
			}
			if _, err = w.bob.store.db.Exec(`UPDATE outbox SET created_ms=1000 WHERE lid=?`, later.LID); err != nil {
				t.Fatal(err)
			}
			if boundary == "rollback" {
				if _, err = w.bob.store.db.Exec(`UPDATE inbox SET received_ms=2000 WHERE conv=? AND lid=?`, conv, lids[1]); err != nil {
					t.Fatal(err)
				}
			}
			for _, partial := range []bool{false, true} {
				var excluded [][]protocol.GrantRef
				if partial {
					excluded = [][]protocol.GrantRef{{}}
				}
				c, err := w.bob.agentContext(p, "", 0, excluded...)
				if err != nil {
					t.Fatal(err)
				}
				text := strings.Join(c.lines, "\n")
				if strings.Contains(text, "logs are in the ticket") {
					t.Errorf("partial=%v: rollback admitted unshared pre-invitation turn", partial)
				}
				if strings.Contains(text, "EARLIER_LOCAL_UNSHARED") {
					t.Errorf("partial=%v: late copy admitted its older unshared original", partial)
				}
				if !strings.Contains(text, "PROVEN_LATER_LOCAL_TURN") {
					t.Errorf("partial=%v: equal-ms later native outbox turn disappeared", partial)
				}
			}
		})
	}
}

func TestRetainedContextNativePromptAndRefresh(t *testing.T) {
	if binary := os.Getenv("AGENTNET_LIVE_CODEX_RETAINED_CONTEXT"); binary != "" {
		actualNativeRetainedDMContext(t, binary)
		return
	}
	stub := installAgentStub(t)
	w, conv, lids, stopBob := agentWorld(t)
	setResponder(t, w.alice, "agentstub", stub.dir, time.Minute)
	if err := w.alice.Approve(w.bob.Address); err != nil {
		t.Fatal(err)
	}
	peer, err := w.alice.InviteAgent(tctx(t), conv, w.alice.Address, nil, nil, "peer")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "peer known on reader host", func() bool { return stateAt(t, w.bob, peer.PID).Claimable() })
	oldRequest, err := w.bob.AskAgent(tctx(t), peer.PID, envelope.KindQuestion, "EARLIER_NATIVE_REQUEST")
	if err != nil {
		t.Fatal(err)
	}
	oldReply := replyAt(t, w.bob, conv, oldRequest.ID)
	pid := participate(t, w, conv, lids[:1], nil)
	askPeer := func(body string) ConvMessage {
		t.Helper()
		q, err := w.bob.AskAgent(tctx(t), peer.PID, envelope.KindQuestion, body)
		if err != nil {
			t.Fatal(err)
		}
		return replyAt(t, w.bob, conv, q.ID)
	}
	firstReply := askPeer("PROVEN_INITIAL_PEER_REQUEST")
	if _, err = w.bob.SendConv(tctx(t), conv, ConvOutgoing{Body: "PROVEN_INITIAL_LOCAL_CONTEXT"}); err != nil {
		t.Fatal(err)
	}
	root, err := w.bob.AskAgent(tctx(t), pid, envelope.KindQuestion, "OWNED_READER_REQUEST")
	if err != nil {
		t.Fatal(err)
	}
	var target string
	parent := job{From: w.bob.Address, Key: w.bob.Self().Fingerprint(), Conv: conv, PID: pid, Kind: envelope.KindQuestion, Body: "OWNED_READER_REQUEST", Local: true}
	if err = w.bob.store.db.QueryRow(`SELECT id,target FROM inbox WHERE conv=? AND lid=? AND local=1`, conv, root.LID).Scan(&parent.ID, &target); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(target), &parent.Target); err != nil {
		t.Fatal(err)
	}
	prepare := func() {
		t.Helper()
		if _, err := w.bob.store.db.Exec(`UPDATE inbox SET state=?,attempts=1 WHERE id=?`, stateRunning, parent.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := w.bob.store.db.Exec(`DELETE FROM room_context WHERE conv=? AND pid=?`, conv, pid); err != nil {
			t.Fatal(err)
		}
	}
	prepare()
	parent.run = w.bob.newRun(parent, false)
	defer parent.run.remove()
	prompt, err := w.bob.agentPrompt(parent, &Responder{Dir: t.TempDir()}, "", tctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, firstReply.LID) || !strings.Contains(prompt, "PROVEN_INITIAL_LOCAL_CONTEXT") || strings.Contains(prompt, oldReply.LID) || strings.Contains(prompt, "logs are in the ticket") {
		t.Fatal("retained initial native prompt scope", prompt)
	}
	secondReply := askPeer("PROVEN_REFRESH_PEER_REQUEST")
	if _, err = w.bob.SendConv(tctx(t), conv, ConvOutgoing{Body: "PROVEN_REFRESH_LOCAL_CONTEXT"}); err != nil {
		t.Fatal(err)
	}
	stopBob()
	prepare()
	// DM copies have physical IDs distinct from their logical turn. Observe
	// the actual owned bridge and acknowledge that exact stored correction.
	ctx, cancel := context.WithCancel(tctx(t))
	input, output := io.Pipe()
	ready := make(chan struct{})
	close(ready)
	bridge := &codexRoomBridge{ctx: ctx, input: output, thread: "owned", turn: "active", ready: ready}
	messages := make(chan map[string]any, 2)
	go func() {
		decoder := json.NewDecoder(input)
		for {
			var message map[string]any
			if decoder.Decode(&message) != nil {
				return
			}
			messages <- message
		}
	}()
	done := make(chan struct{})
	go func() { defer close(done); w.bob.watchNativeFollowups(ctx, parent, bridge, nil) }()
	defer func() { cancel(); <-done; bridge.close(); input.Close() }()
	handover := func() string {
		t.Helper()
		lid := protocol.NewID()
		if _, err := w.bob.QueueRequestFollowup(tctx(t), RequestFollowup{Conv: conv, Ref: envelope.Ref{ID: root.LID, Fingerprint: parent.Key}, ID: lid, Body: "Review current evidence"}); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := w.bob.store.db.QueryRow(`SELECT id FROM inbox WHERE conv=? AND lid=? AND local=1`, conv, lid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var message map[string]any
		select {
		case message = <-messages:
		case <-time.After(3 * time.Second):
			t.Fatal("no owned DM native handover")
		}
		params := message["params"].(map[string]any)
		if params["clientUserMessageId"] != id {
			t.Fatal("handover did not name the exact stored DM correction", params)
		}
		text := params["input"].([]any)[0].(map[string]any)["text"].(string)
		ack, _ := json.Marshal(map[string]any{"id": message["id"], "result": map[string]string{"turnId": "active"}})
		bridge.event(ack)
		waitState(t, w.bob, id, stateSteered)
		return text
	}
	text := handover()
	if !strings.Contains(text, secondReply.LID) || !strings.Contains(text, "PROVEN_REFRESH_LOCAL_CONTEXT") || strings.Contains(text, firstReply.LID) || strings.Contains(text, "PROVEN_INITIAL_LOCAL_CONTEXT") || strings.Contains(text, oldReply.LID) {
		t.Fatal("retained explicit native refresh scope/dedup", text)
	}
	if text = handover(); strings.Contains(text, secondReply.LID) || strings.Contains(text, "PROVEN_REFRESH_LOCAL_CONTEXT") {
		t.Fatal("retained native refresh replayed supplied evidence", text)
	}
	info := stateAt(t, w.bob, pid)
	refused := func(label string) {
		t.Helper()
		c, err := w.bob.agentContext(info, "", 0, []protocol.GrantRef{})
		if err != nil || strings.Contains(strings.Join(c.lines, "\n"), secondReply.LID) {
			t.Fatalf("%s admitted an unproved peer reply: %+v %v", label, c, err)
		}
	}
	for _, change := range []struct{ set, restore string }{
		{`replica=1`, `replica=0`},
		{`via='history-only'`, `via=NULL`},
	} {
		if _, err = w.bob.store.db.Exec(`UPDATE inbox SET `+change.set+` WHERE id=?`, secondReply.ID); err != nil {
			t.Fatal(err)
		}
		refused(change.set)
		if _, err = w.bob.store.db.Exec(`UPDATE inbox SET `+change.restore+` WHERE id=?`, secondReply.ID); err != nil {
			t.Fatal(err)
		}
	}
	// LID alone cannot replace the original's verified author key. The
	// ordering predicate still finds the local request; final binding rejects
	// this conflicting admitted-parent tuple under another member's key.
	conflict := protocol.NewID()
	if _, err = w.bob.store.db.Exec(`INSERT INTO inbox(id,sender,verified_by,ts,kind,body,received_at,received_ms,state,conv,lid,pid,target) SELECT ?,?,?,ts,kind,'conflicting parent',received_at,received_ms,'',conv,lid,pid,target FROM inbox WHERE id=?`, conflict, w.alice.Address, w.alice.Self().Fingerprint(), parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = w.bob.store.db.Exec(`UPDATE inbox SET lid=?,pid=?,target=? WHERE id=?`, secondReply.ReplyTo, peer.PID, targetJSON(&envelope.Target{Address: w.alice.Address, Fingerprint: w.alice.Self().Fingerprint()}), conflict); err != nil {
		t.Fatal(err)
	}
	refused("conflicting original author key")
	if _, err = w.bob.store.db.Exec(`DELETE FROM inbox WHERE id=?`, conflict); err != nil {
		t.Fatal(err)
	}
	// An acceptance retained only as participation metadata is not a native
	// ordering anchor. Do not manufacture chronology when its original is gone.
	if _, err = w.bob.store.db.Exec(`UPDATE outbox SET body='' WHERE conv=? AND pid=? AND sub=?`, conv, pid, envelope.SubEvent); err != nil {
		t.Fatal(err)
	}
	c, err := w.bob.agentContext(info, "", 0, []protocol.GrantRef{})
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Join(c.lines, "\n")
	if strings.Contains(text, firstReply.LID) || strings.Contains(text, secondReply.LID) || strings.Contains(text, "PROVEN_REFRESH_LOCAL_CONTEXT") {
		t.Fatal("missing native acceptance created an order", text)
	}
}
