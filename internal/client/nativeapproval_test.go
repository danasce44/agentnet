package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func nativeTestCallback(t *testing.T, method string, extra map[string]any, item string) nativeCallback {
	t.Helper()
	p := map[string]any{"threadId": "owned-thread", "turnId": "owned-turn", "itemId": "item", "cwd": "/owned/work"}
	for k, v := range extra {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	c, e := codexNativeCallback(method, json.RawMessage(`"rpc"`), raw, json.RawMessage(item), "/owned/work", "owned-thread", "owned-turn")
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestNativeApprovalProtocolDisclosureAndResponses(t *testing.T) {
	command := "item/commandExecution/requestApproval"
	for _, tt := range []struct {
		name  string
		extra map[string]any
		item  string
		allow bool
	}{
		{"command", map[string]any{"command": "printf exact > result.md"}, "", true},
		{"matching-item", nil, `{"type":"commandExecution","id":"item","command":"printf exact > result.md","cwd":"/owned/work"}`, true},
		{"unusable-item", nil, `{"id":"item"}`, false},
		{"other-item", nil, `{"type":"commandExecution","id":"other","command":"exact"}`, false},
		{"missing-operation", nil, "", false},
		{"network-only", map[string]any{"networkApprovalContext": map[string]string{"host": "fixture.invalid", "protocol": "https"}}, "", true},
		{"missing-network-host", map[string]any{"networkApprovalContext": map[string]string{"protocol": "https"}}, "", false},
		{"oversized-command", map[string]any{"command": strings.Repeat("x", nativeOperationMax)}, "", false},
		{"no-one-action-grant", map[string]any{"command": "exact", "availableDecisions": []string{"acceptForSession"}}, "", false},
		{"stdin-is-not-command", map[string]any{"kind": "writeStdin"}, `{"type":"commandExecution","id":"item","command":"parent command"}`, false},
		{"unknown-kind", map[string]any{"kind": "unknown", "command": "exact"}, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := nativeTestCallback(t, command, tt.extra, tt.item)
			if got := slices.Contains(c.View.Choices, "allow"); got != tt.allow {
				t.Fatalf("allow=%v view=%+v", got, c.View)
			}
			result, e := nativeCallbackResult(c, nativeResponse{Choice: "allow"})
			if (e == nil) != tt.allow {
				t.Fatal(string(result), e)
			}
			if tt.allow && string(result) != `{"decision":"accept"}` {
				t.Fatal(string(result))
			}
		})
	}
	for _, extra := range []map[string]any{
		{"availableDecisions": []string{"accept"}},
		{"command": strings.Repeat("x", nativeOperationMax), "availableDecisions": []string{"acceptForSession"}},
	} {
		c := nativeTestCallback(t, command, extra, "")
		if !c.View.CancelRun || !slices.Equal(c.View.Choices, []string{"cancel"}) {
			t.Fatal("undisclosed operation invented a native decision", c.View)
		}
		result, e := nativeCallbackResult(c, nativeResponse{Choice: "cancel"})
		if e != nil || result != nil {
			t.Fatal("Cancel must stop owned worker", string(result), e)
		}
	}
	c := nativeTestCallback(t, command, map[string]any{"command": "exact", "approvalId": "distinct-native-approval"}, "")
	if c.View.ApprovalID != "distinct-native-approval" {
		t.Fatal(c.View)
	}
	for choice, want := range map[string]string{"deny": `{"decision":"decline"}`, "cancel": `{"decision":"cancel"}`} {
		got, e := nativeCallbackResult(c, nativeResponse{Choice: choice})
		if e != nil || string(got) != want {
			t.Fatal(choice, string(got), e)
		}
	}
	permissions := nativeTestCallback(t, "item/permissions/requestApproval", map[string]any{"permissions": map[string]any{"network": map[string]bool{"enabled": true}}}, "")
	allow, e := nativeCallbackResult(permissions, nativeResponse{Choice: "allow"})
	if e != nil || !strings.Contains(string(allow), `"scope":"turn"`) || strings.Contains(string(allow), "session") {
		t.Fatal(string(allow), e)
	}
	deny, e := nativeCallbackResult(permissions, nativeResponse{Choice: "deny"})
	if e != nil || string(deny) != `{"permissions":{},"scope":"turn"}` {
		t.Fatal(string(deny), e)
	}
	cancel, e := nativeCallbackResult(permissions, nativeResponse{Choice: "cancel"})
	if e != nil || cancel != nil {
		t.Fatal("permissions cancel must stop the owned worker, not return empty grant", string(cancel), e)
	}
	file := nativeTestCallback(t, "item/fileChange/requestApproval", nil, `{"id":"item","type":"fileChange","changes":[{"path":"/owned/work/result.md","kind":{"type":"add"},"diff":"+exact content"}]}`)
	if !strings.Contains(file.View.Operation, "+exact content") || !slices.Contains(file.View.Choices, "allow") {
		t.Fatal(file.View)
	}
	missing := nativeTestCallback(t, "item/fileChange/requestApproval", nil, "")
	if slices.Contains(missing.View.Choices, "allow") {
		t.Fatal("undisclosed file operation allowed")
	}
	root := nativeTestCallback(t, "item/fileChange/requestApproval", map[string]any{"grantRoot": "/owned/work"}, `{"id":"item","type":"fileChange","changes":[{"path":"/owned/work/a","kind":{"type":"add"},"diff":"+a"}]}`)
	if slices.Contains(root.View.Choices, "allow") || !strings.Contains(root.View.Operation, "Proposed session write root: /owned/work") {
		t.Fatal("session write-root proposal was not disclosed/refused", root.View)
	}
	for _, kind := range []string{"add", "delete", "update"} {
		c := nativeTestCallback(t, "item/fileChange/requestApproval", nil, `{"id":"item","type":"fileChange","changes":[{"path":"/owned/empty.txt","kind":{"type":"`+kind+`"},"diff":""}]}`)
		if !slices.Contains(c.View.Choices, "allow") {
			t.Fatal("valid empty native diff refused", kind)
		}
	}
	for _, change := range []string{`{"path":"/owned/a","diff":"+a"}`, `{"path":"/owned/a","kind":null,"diff":"+a"}`, `{"path":"/owned/a","kind":{"type":"unknown"},"diff":"+a"}`, `{"path":"/owned/a","kind":{"type":"add","unknown":true},"diff":"+a"}`, `{"path":"/owned/a","kind":{"type":"add"}}`, `{"path":"/owned/a","kind":{"type":"add"},"diff":null}`, `{"path":"/owned/a","kind":{"type":"add"},"diff":"+a","unknown":true}`} {
		c := nativeTestCallback(t, "item/fileChange/requestApproval", nil, `{"id":"item","type":"fileChange","changes":[`+change+`]}`)
		if slices.Contains(c.View.Choices, "allow") {
			t.Fatal("malformed file operation allowed", change)
		}
	}
}

func TestNativeApprovalReceivedOwnerAttentionTokensAndRoles(t *testing.T) {
	w := newWorld(t, "")
	if _, e := w.alice.CreatePerson(tctx(t), "Owner"); e != nil {
		t.Fatal(e)
	}
	runWith(t, w, w.alice, RunOptions{HumanOnly: true})
	phone, awaited, _ := linkPhone(t, w.alice, "phone")
	if e := w.alice.DecideLink(tctx(t), pendingLink(t, w.alice).ID, true); e != nil {
		t.Fatal(e)
	}
	if linked := <-awaited; linked.err != nil {
		t.Fatal(linked.err)
	}
	if _, e := phone.sendKey(tctx(t), w.alice.Address); e != nil {
		t.Fatal(e)
	}
	var alerts []NativeNotification
	phone.nativeNotifyContent = func(n NativeNotification) { alerts = append(alerts, n) }
	it := ReportItem{ID: protocol.NewID(), From: w.bob.Address, Key: w.bob.Self().Fingerprint(), Kind: envelope.KindTask, State: stateRunning, Blocker: BlockerRunning, Since: 1, Attempt: 1}
	it.Native = &NativeRequest{Token: protocol.NewID(), Harness: "codex", Host: w.alice.Address, Request: it.ID, Key: it.Key, Attempt: 1, Thread: "owned", Turn: "current", Choices: []string{"allow", "deny", "cancel"}, Operation: "PRIVATE_NATIVE_OPERATION", Expires: 0}
	if ok, e := operatorHolds(phone.store.db, phone.Address, phone.Self().Fingerprint()); e != nil || ok {
		t.Fatal("fixture unexpectedly grants operator authority", e)
	}
	receive := func(from *Agent, item ReportItem) string {
		body := reviewAlertBody(t, from.Address, item)
		recipient, _ := phone.Self().Recipient()
		in := envelope.Inner{V: envelope.Version, ID: protocol.NewID(), From: from.Address, To: phone.Address, TS: time.Now().Unix(), Kind: envelope.KindMessage, Status: envelope.StatusReviewNotice, Body: body}
		env, e := envelope.Seal(in, from.id.Sign, recipient)
		if e != nil {
			t.Fatal(e)
		}
		if e = phone.verifyAndStore(tctx(t), env); e != nil {
			t.Fatal(e)
		}
		phone.notifyReview()
		return in.ID
	}
	received := receive(w.alice, it)
	if len(alerts) != 1 {
		t.Fatalf("owner with no operator grant received %d alerts", len(alerts))
	}
	if strings.Contains(alerts[0].Body, "PRIVATE") || alerts[0].Fragment != "review" {
		t.Fatal("native attention leaked operation or wrong route", alerts[0])
	}
	project := func(item ReportItem) *NativeRequest {
		r, ok := phone.NoticeReport(Message{ID: received, From: w.alice.Address, Body: reviewAlertBody(t, w.alice.Address, item)})
		if !ok || len(r.Items) != 1 {
			t.Fatal("real host report lost", r)
		}
		return r.Items[0].Native
	}
	if project(it) == nil {
		t.Fatal("current owner-human native projection missing")
	}
	for _, invalid := range []string{"ended", "token", "thread", "harness", "attempt"} {
		copyItem := it
		copyNative := *it.Native
		copyItem.Native = &copyNative
		switch invalid {
		case "ended":
			copyItem.State = stateAnswered
		case "token":
			copyNative.Token = "invalid"
		case "thread":
			copyNative.Thread = ""
		case "harness":
			copyNative.Harness = "other"
		case "attempt":
			copyNative.Attempt = 0
			copyItem.Attempt = 0
		}
		if project(copyItem) != nil {
			t.Fatal("invalid native projection became actionable", invalid)
		}
	}
	_, _ = phone.store.db.Exec(`UPDATE inbox SET replica=1 WHERE id=?`, received)
	if project(it) != nil {
		t.Fatal("historical native report became actionable")
	}
	_, _ = phone.store.db.Exec(`UPDATE inbox SET replica=0 WHERE id=?`, received)
	_, _ = phone.store.db.Exec(`INSERT INTO config(k,v) VALUES(?,?)`, revokedDevicePrefix+w.alice.Address+"/"+w.alice.Self().Fingerprint(), w.alice.Address)
	if project(it) != nil {
		t.Fatal("revoked host's private native projection remained actionable")
	}
	_, _ = phone.store.db.Exec(`DELETE FROM config WHERE k=?`, revokedDevicePrefix+w.alice.Address+"/"+w.alice.Self().Fingerprint())
	receive(w.alice, it)
	if len(alerts) != 1 {
		t.Fatal("duplicate native token alerted again")
	}
	it.Native.Token = protocol.NewID()
	receive(w.alice, it)
	if len(alerts) != 2 {
		t.Fatal("sequential native callback did not alert")
	}
	expired := it
	copyN := *it.Native
	copyN.Token = protocol.NewID()
	copyN.Expires = 1
	expired.Native = &copyN
	receive(w.alice, expired)
	if len(alerts) != 2 {
		t.Fatal("expired callback alerted")
	}
	foreign := it
	foreignCopy := *it.Native
	foreignCopy.Host = w.bob.Address
	foreignCopy.Token = protocol.NewID()
	foreign.Native = &foreignCopy
	receive(w.bob, foreign)
	if len(alerts) != 2 {
		t.Fatal("foreign report granted native owner attention")
	}
	if _, e := phone.store.db.Exec(`UPDATE persons SET state='conflict' WHERE state='self'`); e != nil {
		t.Fatal(e)
	}
	it.Native.Token = protocol.NewID()
	receive(w.alice, it)
	if len(alerts) != 2 {
		t.Fatal("frozen owner got native approval attention")
	}
}

func TestNativeApprovalInputAndMCPBoundaries(t *testing.T) {
	input := nativeTestCallback(t, "item/tool/requestUserInput", map[string]any{"questions": []NativeQuestion{{ID: "account", Header: "Account", Question: "Which account?", IsSecret: true, Options: []NativeQuestionOption{{Label: "Company", Description: "company account"}}}}}, "")
	result, e := nativeCallbackResult(input, nativeResponse{Choice: "submit", Answers: map[string]string{"account": "Company"}})
	if e != nil || string(result) != `{"answers":{"account":{"answers":["Company"]}}}` {
		t.Fatal(string(result), e)
	}
	for _, answers := range []map[string]string{{}, {"other": "Company"}, {"account": "Fabricated"}, {"account": "Company", "other": "x"}} {
		if _, e = nativeCallbackResult(input, nativeResponse{Choice: "submit", Answers: answers}); e == nil {
			t.Fatal("unbound answer accepted", answers)
		}
	}
	if result, e = nativeCallbackResult(input, nativeResponse{Choice: "cancel"}); e != nil || result != nil {
		t.Fatal("cancel fabricated a native answer")
	}
	form := nativeTestCallback(t, "mcpServer/elicitation/request", map[string]any{"mode": "form", "serverName": "owned-server", "message": "Pick scope", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"account": map[string]any{"type": "string", "enum": []string{"Company", "Personal"}}, "ready": map[string]string{"type": "boolean"}}, "required": []string{"account"}}}, "")
	result, e = nativeCallbackResult(form, nativeResponse{Choice: "submit", Values: map[string]json.RawMessage{"account": json.RawMessage(`"Company"`), "ready": json.RawMessage(`false`)}})
	if e != nil || !strings.Contains(string(result), `"action":"accept"`) || !strings.Contains(string(result), `"ready":false`) {
		t.Fatal(string(result), e)
	}
	for _, values := range []map[string]json.RawMessage{{}, {"account": json.RawMessage(`"Unknown"`)}, {"account": json.RawMessage(`7`)}, {"account": json.RawMessage(`"Company"`), "other": json.RawMessage(`true`)}} {
		if _, e = nativeCallbackResult(form, nativeResponse{Choice: "submit", Values: values}); e == nil {
			t.Fatal("invalid MCP form accepted", values)
		}
	}
	for _, mode := range []string{"openai/userVerification", "url", "openai/form", "openaiForm"} {
		c := nativeTestCallback(t, "mcpServer/elicitation/request", map[string]any{"mode": mode, "serverName": "owned-server", "message": "Native authenticated interaction"}, "")
		if slices.Contains(c.View.Choices, "submit") || c.View.Unsupported == "" {
			t.Fatal("fabricated authenticated native proof", mode)
		}
		if _, e = nativeCallbackResult(c, nativeResponse{Choice: "deny"}); e != nil {
			t.Fatal(e)
		}
	}
	unknownSchema := nativeTestCallback(t, "mcpServer/elicitation/request", map[string]any{"mode": "form", "serverName": "owned-server", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"secret": map[string]any{"type": "string", "pattern": ".*"}}}}, "")
	if slices.Contains(unknownSchema.View.Choices, "submit") {
		t.Fatal("unimplemented validation was ignored")
	}
}

func TestNativeApprovalBridgeSameRPCAndResolved(t *testing.T) {
	for _, ending := range []string{"approve", "resolve", "expire", "turn-end", "bad-authority"} {
		t.Run(ending, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			in, out := io.Pipe()
			defer in.Close()
			var output bytes.Buffer
			called := make(chan nativeCallback, 2)
			choice := make(chan struct{})
			finished := make(chan struct{})
			b := &codexRoomBridge{ctx: ctx, input: out, output: &output, cwd: "/owned/work", thread: "owned-thread", turn: "owned-turn", nativeEnd: make(chan struct{}), calls: map[string]bool{}}
			defer b.close()
			b.native = func(ctx context.Context, c nativeCallback) (json.RawMessage, error) {
				called <- c
				defer close(finished)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-choice:
					return json.RawMessage(`{"decision":"accept"}`), nil
				}
			}
			b.nativeCheck = func(nativeCallback) bool { return ending != "bad-authority" }
			wire := make(chan json.RawMessage, 2)
			go func() {
				d := json.NewDecoder(in)
				for {
					var raw json.RawMessage
					if d.Decode(&raw) != nil {
						return
					}
					wire <- raw
				}
			}()
			rpc := []byte(`{"id":"exact-rpc","method":"item/commandExecution/requestApproval","params":{"threadId":"owned-thread","turnId":"owned-turn","itemId":"exact-item","approvalId":"distinct","command":"printf exact > result.md","cwd":"/owned/work"}}`)
			b.event(rpc)
			b.event(rpc)
			select {
			case c := <-called:
				if string(c.ID) != `"exact-rpc"` || c.View.ApprovalID != "distinct" {
					t.Fatal(c)
				}
			case <-ctx.Done():
				t.Fatal("native callback missing")
			}
			select {
			case <-called:
				t.Fatal("duplicate native callback")
			default:
			}
			// Ordinary native output remains readable while the RPC awaits a person.
			b.event([]byte(`{"method":"item/completed","params":{"threadId":"owned-thread","turnId":"owned-turn","item":{"type":"agentMessage","text":"same-operation-answer"}}}`))
			switch ending {
			case "resolve":
				b.event([]byte(`{"method":"serverRequest/resolved","params":{"threadId":"owned-thread","requestId":"exact-rpc"}}`))
			case "expire":
				cancel()
			case "turn-end":
				b.event([]byte(`{"method":"turn/completed","params":{"threadId":"owned-thread","turn":{"id":"owned-turn","status":"completed"}}}`))
			default:
				close(choice)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("callback did not end")
			}
			if ending == "approve" {
				select {
				case got := <-wire:
					var packet struct {
						ID     string            `json:"id"`
						Result map[string]string `json:"result"`
					}
					if json.Unmarshal(got, &packet) != nil || packet.ID != "exact-rpc" || packet.Result["decision"] != "accept" {
						t.Fatal(string(got))
					}
				case <-ctx.Done():
					t.Fatal("response did not answer same RPC")
				}
			} else {
				b.close()
				select {
				case got := <-wire:
					t.Fatal("ended callback received response", string(got))
				case <-time.After(20 * time.Millisecond):
				}
			}
		})
	}
}

func TestNativeApprovalNeverPolicyAndChangedRPC(t *testing.T) {
	for _, ending := range []string{"never", "becomes-never", "changed-rpc"} {
		t.Run(ending, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			in, out := io.Pipe()
			defer in.Close()
			var output bytes.Buffer
			called := make(chan nativeCallback, 1)
			choose := make(chan struct{})
			complete := make(chan struct{})
			b := &codexRoomBridge{ctx: ctx, input: out, output: &output, cwd: "/owned/work", thread: "owned-thread", turn: "owned-turn", nativeEnd: make(chan struct{}), nativePolicy: "on-request"}
			defer b.close()
			if ending == "never" {
				b.nativePolicy = "never"
			}
			b.native = func(ctx context.Context, c nativeCallback) (json.RawMessage, error) {
				called <- c
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-choose:
					return json.RawMessage(`{"decision":"accept"}`), nil
				}
			}
			b.nativeComplete = func(nativeCallback) { close(complete) }
			wire := make(chan json.RawMessage, 1)
			go func() {
				var raw json.RawMessage
				if json.NewDecoder(in).Decode(&raw) == nil {
					wire <- raw
				}
			}()
			rpc := []byte(`{"id":"exact-rpc","method":"item/commandExecution/requestApproval","params":{"threadId":"owned-thread","turnId":"owned-turn","itemId":"item","command":"exact","cwd":"/owned/work","availableDecisions":["accept"]}}`)
			b.event(rpc)
			var callback nativeCallback
			select {
			case callback = <-called:
			case <-ctx.Done():
				t.Fatal("callback missing")
			}
			switch ending {
			case "never":
				if slices.Contains(callback.View.Choices, "allow") || !callback.View.CancelRun || callback.View.Unsupported == "" {
					t.Fatal(callback.View)
				}
			case "becomes-never":
				b.event([]byte(`{"method":"thread/settings/updated","params":{"threadId":"owned-thread","threadSettings":{"approvalPolicy":"never"}}}`))
			case "changed-rpc":
				b.event(bytes.ReplaceAll(rpc, []byte(`"command":"exact"`), []byte(`"command":"different"`)))
			}
			close(choose)
			select {
			case <-complete:
			case <-ctx.Done():
				t.Fatal("callback did not finish")
			}
			select {
			case got := <-wire:
				t.Fatal("permission sent despite current policy or changed RPC", string(got))
			default:
			}
		})
	}
}

func nativeApprovalFixture(t *testing.T) (*Agent, job, identity.Public, identity.Public) {
	t.Helper()
	s := securityStore(t)
	host, _ := identity.Generate()
	phone, _ := identity.Generate()
	bot, _ := identity.Generate()
	foreign, _ := identity.Generate()
	a := &Agent{Address: "owner/host", id: host, store: s, home: t.TempDir(), changes: newChangeFeed(), wakeWorker: func() {}, Logf: func(string, ...any) {}}
	s.onChange = a.changes.bump
	p := phone.Public("owner/phone")
	agentHost := bot.Public("owner/agent-host")
	requester := foreign.Public("coworker/laptop")
	roster := protocol.PersonRoster{Person: protocol.NewID(), Label: "Owner", Devices: []identity.Public{a.Self(), p, agentHost}, HumanKeys: []string{p.Fingerprint()}, By: p.Fingerprint()}
	roster.Sign(phone.Sign)
	raw, _ := json.Marshal(roster)
	if _, e := s.db.Exec(`INSERT INTO persons(person,label,seq,hash,record,state,pinned_at) VALUES(?,'Owner',0,?,?,'self',1)`, roster.Person, roster.Hash(), string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, pub := range roster.Devices {
		if _, e := s.db.Exec(`INSERT INTO person_devices(address,person,fingerprint,added) VALUES(?,?,?,0)`, pub.Address, roster.Person, pub.Fingerprint()); e != nil {
			t.Fatal(e)
		}
		if e := s.pin(pub); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.pin(requester); e != nil {
		t.Fatal(e)
	}
	j := job{ID: protocol.NewID(), From: requester.Address, Key: requester.Fingerprint(), Kind: envelope.KindTask, Body: "PRIVATE ORIGINAL REQUEST"}
	if _, e := s.db.Exec(`INSERT INTO inbox(id,sender,ts,kind,body,received_at,state,verified_by,attempts) VALUES(?,?,1,'task',?,1,'running',?,1)`, j.ID, j.From, j.Body, j.Key); e != nil {
		t.Fatal(e)
	}
	return a, j, p, agentHost
}

func nativeResponseText(token, choice string) string {
	raw, _ := json.Marshal(nativeResponse{Token: token, Choice: choice})
	return nativeResponseDomain + string(raw)
}

func TestNativeApprovalStoreCurrentOwnerNoRequeue(t *testing.T) {
	a, j, phone, bot := nativeApprovalFixture(t)
	c := nativeTestCallback(t, "item/commandExecution/requestApproval", map[string]any{"command": "printf exact > result.md"}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan json.RawMessage, 1)
	errs := make(chan error, 1)
	go func() { raw, e := a.awaitNativeRequest(ctx, j, c); result <- raw; errs <- e }()
	var n *NativeRequest
	eventually(t, "genuine pending native callback", func() bool { n = a.nativeRequestFor(j.ID); return n != nil })
	if strings.Contains(n.Operation, j.Body) || n.Request != j.ID || n.Attempt != 1 {
		t.Fatal(n)
	}
	reportID := protocol.NewID()
	report := a.reportBodyFor([]ReportItem{{ID: j.ID, Key: j.Key, State: stateRunning, Attempt: 1, Native: n}}, nil, true, false, true)
	if _, e := a.store.db.Exec(`INSERT INTO outbox(id,recipient,recipient_fp,body,envelope,state,created_at,status) VALUES(?,?,?,?,'{}','queued',1,?)`, reportID, phone.Address, phone.Fingerprint(), report, envelope.StatusReviewNotice); e != nil {
		t.Fatal(e)
	}
	claim := func(pub identity.Public, report string, attempt int64, token, choice string) error {
		tx, e := a.store.db.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		e = a.claimNativeResponse(tx, j.ID, j.Key, attempt, nativeResponseText(token, choice), protocol.NewID(), pub.Address, pub.Fingerprint(), report, false)
		if e == nil {
			e = a.store.done(tx.Commit())
		}
		return e
	}
	for _, pub := range []identity.Public{bot, a.Self()} {
		if e := claim(pub, reportID, 1, n.Token, "allow"); e == nil {
			t.Fatal("agent-host authorized as human", pub.Address)
		}
	}
	if e := claim(phone, reportID, 2, n.Token, "allow"); e == nil {
		t.Fatal("wrong attempt authorized")
	}
	if e := claim(phone, protocol.NewID(), 1, n.Token, "allow"); e == nil {
		t.Fatal("unreported callback authorized")
	}
	if e := claim(phone, reportID, 1, protocol.NewID(), "allow"); e == nil {
		t.Fatal("forged token authorized")
	}
	if e := claim(phone, reportID, 1, n.Token, "allow"); e != nil {
		t.Fatal(e)
	}
	select {
	case raw := <-result:
		if string(raw) != `{"decision":"accept"}` {
			t.Fatal(string(raw))
		}
	case <-ctx.Done():
		t.Fatal("native response was not consumed")
	}
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if !a.nativeHandoverCurrent(j, c) {
		t.Fatal("genuine current owner cannot hand over")
	}
	if e := claim(phone, reportID, 1, n.Token, "deny"); e == nil {
		t.Fatal("second decision was accepted")
	}
	var state string
	var continuations int
	_ = a.store.db.QueryRow(`SELECT state FROM inbox WHERE id=?`, j.ID).Scan(&state)
	_ = a.store.db.QueryRow(`SELECT count(*) FROM request_continuations`).Scan(&continuations)
	if state != stateRunning || continuations != 0 {
		t.Fatal("native response scheduled a new run", state, continuations)
	}
	a.endNativeCallback(j, c)
	if a.nativeRequestFor(j.ID) != nil {
		t.Fatal("ended native action remained visible")
	}
	if e := a.ContinueRequest(j.ID, protocol.NewID(), 1, nativeResponseText(n.Token, "allow"), j.Key); e == nil {
		t.Fatal("old host generic continuation accepted running native response")
	}
	_ = a.store.db.QueryRow(`SELECT state FROM inbox WHERE id=?`, j.ID).Scan(&state)
	_ = a.store.db.QueryRow(`SELECT count(*) FROM request_continuations`).Scan(&continuations)
	if state != stateRunning || continuations != 0 {
		t.Fatal("old host requeued native response")
	}
}

func TestNativeApprovalLocalReceiptRetryAfterHandover(t *testing.T) {
	a, j, _, _ := nativeApprovalFixture(t)
	c := nativeTestCallback(t, "item/tool/requestUserInput", map[string]any{"questions": []map[string]any{{"id": "secret", "question": "Private input", "isSecret": true}}}, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ended := make(chan error, 1)
	go func() { _, e := a.awaitNativeRequest(ctx, j, c); ended <- e }()
	var n *NativeRequest
	eventually(t, "pending input", func() bool { n = a.nativeRequestFor(j.ID); return n != nil })
	answer, _ := json.Marshal(nativeResponse{Token: n.Token, Choice: "submit", Answers: map[string]string{"secret": "isolated-secret"}})
	text, send := nativeResponseDomain+string(answer), protocol.NewID()
	if e := a.RespondNativeRequest(j.ID, j.Key, 1, text, send); e != nil {
		t.Fatal(e)
	}
	if e := <-ended; e != nil {
		t.Fatal(e)
	}
	a.endNativeCallback(j, c)
	if e := a.RespondNativeRequest(j.ID, j.Key, 1, text, send); e != nil {
		t.Fatal("exact stored response receipt lost after handover", e)
	}
	if e := a.RespondNativeRequest(j.ID, j.Key, 1, text, protocol.NewID()); e == nil {
		t.Fatal("new decision reauthorized ended callback")
	}
	if e := a.RespondNativeRequest(j.ID, j.Key, 1, strings.ReplaceAll(text, "isolated-secret", "different"), send); e == nil {
		t.Fatal("changed input reauthorized ended callback")
	}
	var state, params, response, view string
	if e := a.store.db.QueryRow(`SELECT state,params,response,view FROM native_requests WHERE token=?`, n.Token).Scan(&state, &params, &response, &view); e != nil {
		t.Fatal(e)
	}
	if state != "ended" || params != "" || response != "" || view != "" {
		t.Fatal("receipt recovery retained input or reanimated callback", state)
	}
}

func TestNativeApprovalSignedOwnPhoneSameProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix native executable fixture")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "codex-fixture")
	if e = os.WriteFile(wrapper, []byte("#!/bin/sh\nexec \"$AGENTNET_CODEX_FIXTURE_EXE\" -test.run '^TestCodexRoomFixtureProcess$'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENTNET_CODEX_FIXTURE_EXE", exe)
	t.Setenv("AGENTNET_CODEX_ROOM_FIXTURE", "native-approval")
	log := filepath.Join(dir, "process.log")
	file := filepath.Join(dir, "result.txt")
	t.Setenv("AGENTNET_NATIVE_PROCESS_LOG", log)
	t.Setenv("AGENTNET_NATIVE_RESULT", file)
	old := Harnesses["codex"]
	selected := old
	selected.bin = wrapper
	Harnesses["codex"] = selected
	t.Cleanup(func() { Harnesses["codex"] = old })
	w := newWorld(t, "")
	if _, e = w.alice.CreatePerson(tctx(t), "Owner"); e != nil {
		t.Fatal(e)
	}
	setResponder(t, w.alice, "codex", dir, 0)
	runWith(t, w, w.alice, RunOptions{})
	phone := linkedVia(t, w.alice, "phone", func(ctx context.Context, id string) error { return w.alice.DecideLink(ctx, id, true) })
	q, e := phone.SendMessage(tctx(t), Outgoing{To: w.alice.Address, Kind: envelope.KindQuestion, Body: "Perform the native fixture operation"})
	if e != nil {
		t.Fatal(e)
	}
	var n *NativeRequest
	var reportID string
	eventually(t, "owner phone receives genuine pending native callback", func() bool {
		messages, _ := phone.Inbox(false, true)
		for _, m := range messages {
			if m.Status != envelope.StatusReviewNotice {
				continue
			}
			r, ok := phone.NoticeReport(m)
			if !ok {
				continue
			}
			for _, it := range r.Items {
				if it.ID == q.ID && it.Native != nil {
					n = it.Native
					reportID = m.ID
					return true
				}
			}
		}
		return false
	})
	if n.Expires != 0 || n.ApprovalID != "native-distinct-approval" {
		t.Fatal("native callback binding lost", n)
	}
	if _, e = os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("native operation executed before human response")
	}
	if allowed, e := operatorHolds(w.alice.store.db, phone.Address, phone.Self().Fingerprint()); e != nil || allowed {
		t.Fatal("owner needed an operator grant", e)
	}
	decisionID := protocol.NewID()
	sent, e := phone.Decide(WithQueuedSend(tctx(t), decisionID), w.alice.Address, q.ID, phone.Self().Fingerprint(), "continue", stateRunning, 1, nativeResponseText(n.Token, "allow"), reportID)
	if e != nil || sent.ID != decisionID {
		t.Fatal(sent, e)
	}
	waitState(t, w.alice, q.ID, stateAnswered)
	data, e := os.ReadFile(file)
	if e != nil || string(data) != "ONE_NATIVE_OPERATION" {
		t.Fatal("same native operation not completed", e)
	}
	processes, _ := os.ReadFile(log)
	if strings.Count(string(processes), "started\n") != 1 {
		t.Fatal("native response started another process", string(processes))
	}
	var attempts, continuations int
	_ = w.alice.store.db.QueryRow(`SELECT attempts FROM inbox WHERE id=?`, q.ID).Scan(&attempts)
	_ = w.alice.store.db.QueryRow(`SELECT count(*) FROM request_continuations`).Scan(&continuations)
	if attempts != 1 || continuations != 0 {
		t.Fatal("native callback replayed original work", attempts, continuations)
	}
	env, e := phone.store.outboxEnvelope(decisionID)
	if e != nil {
		t.Fatal(e)
	}
	in, e := envelope.Open(env, w.alice.id, w.alice.Address, phone.Self())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.alice.admitDecision(tctx(t), env, in, phone.Self()); e != nil {
		t.Fatal(e)
	}
	processes, _ = os.ReadFile(log)
	if strings.Count(string(processes), "started\n") != 1 {
		t.Fatal("decision replay started another process")
	}
	eventually(t, "native callback action clears on owner phone", func() bool {
		reports, _ := phone.Notices()
		for _, m := range reports {
			r, ok := phone.NoticeReport(m)
			if !ok {
				continue
			}
			for _, it := range r.Items {
				if it.Native != nil && it.Native.Token == n.Token {
					return false
				}
			}
		}
		return true
	})
}

func TestNativeApprovalAuthorityExpiryAndPrivateReport(t *testing.T) {
	for _, ending := range []string{"removed", "pending-key", "changed-pin", "frozen", "revoked", "requester-revoked", "requester-changed", "cancel", "restart", "expiry"} {
		t.Run(ending, func(t *testing.T) {
			a, j, phone, _ := nativeApprovalFixture(t)
			c := nativeTestCallback(t, "item/permissions/requestApproval", map[string]any{"permissions": map[string]any{"network": map[string]bool{"enabled": true}}}, "")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ended := make(chan error, 1)
			go func() { _, e := a.awaitNativeRequest(ctx, j, c); ended <- e }()
			var n *NativeRequest
			eventually(t, "native permission appears", func() bool { n = a.nativeRequestFor(j.ID); return n != nil })
			it := ReportItem{ID: j.ID, Key: j.Key, State: stateRunning, Attempt: 1, Native: n}
			reportID := protocol.NewID()
			body := a.reportBodyFor([]ReportItem{it}, nil, true, false, true)
			if _, e := a.store.db.Exec(`INSERT INTO outbox(id,recipient,recipient_fp,body,envelope,state,created_at,status) VALUES(?,?,?,?,'{}','queued',1,?)`, reportID, phone.Address, phone.Fingerprint(), body, envelope.StatusReviewNotice); e != nil {
				t.Fatal(e)
			}
			for _, tt := range []struct{ full, operator bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
				body := a.reportBodyFor([]ReportItem{it}, nil, tt.full, tt.operator)
				if strings.Contains(body, `"native"`) || strings.Contains(body, "Native permissions") {
					t.Fatal("private native operation leaked to operator/steward/agent report", body)
				}
			}
			if ending == "removed" {
				_, _ = a.store.db.Exec(`DELETE FROM person_devices WHERE address=?`, phone.Address)
			}
			if ending == "requester-revoked" {
				_, _ = a.store.db.Exec(`INSERT INTO config(k,v) VALUES(?,?)`, "revoked-exact-device/"+j.From+"/"+j.Key, j.From)
			}
			if ending == "requester-changed" {
				_, _ = a.store.db.Exec(`UPDATE peers SET pending='changed-key' WHERE address=?`, j.From)
			}
			if ending == "pending-key" || ending == "changed-pin" {
				changed, _ := identity.Generate()
				if ending == "pending-key" {
					_ = a.store.setPending(changed.Public(phone.Address))
				} else {
					_ = a.store.pin(changed.Public(phone.Address))
				}
			}
			if ending == "frozen" {
				_, _ = a.store.db.Exec(`UPDATE persons SET state='conflict'`)
			}
			if ending == "revoked" {
				_, _ = a.store.db.Exec(`INSERT INTO config(k,v) VALUES(?,?)`, revokedDevicePrefix+phone.Address+"/"+phone.Fingerprint(), phone.Address)
			}
			if ending == "cancel" {
				_, _ = a.store.db.Exec(`UPDATE inbox SET state=? WHERE id=?`, stateCancelReq, j.ID)
			}
			if ending == "restart" {
				_ = a.store.interruptRunning()
			}
			if ending == "expiry" {
				_, _ = a.store.db.Exec(`UPDATE native_requests SET deadline=1 WHERE token=?`, n.Token)
			}
			tx, e := a.store.db.Begin()
			if e != nil {
				t.Fatal(e)
			}
			e = a.claimNativeResponse(tx, j.ID, j.Key, 1, nativeResponseText(n.Token, "allow"), protocol.NewID(), phone.Address, phone.Fingerprint(), reportID, false)
			_ = tx.Rollback()
			if e == nil {
				t.Fatal("stale/ended native permission authorized", ending)
			}
			cancel()
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("native pending waiter survived ending")
			}
			if a.nativeRequestFor(j.ID) != nil {
				t.Fatal("ended callback remains actionable")
			}
		})
	}
}

func TestNativeApprovalDefaultUnlimitedAndConfiguredExpiry(t *testing.T) {
	for _, choice := range []string{"allow", "deny", "input", "end", "bounded-expiry"} {
		t.Run(choice, func(t *testing.T) {
			a, j, _, _ := nativeApprovalFixture(t)
			c := nativeTestCallback(t, "item/commandExecution/requestApproval", map[string]any{"command": "printf exact > result.md"}, "")
			if choice == "input" {
				c = nativeTestCallback(t, "item/tool/requestUserInput", map[string]any{"questions": []NativeQuestion{{ID: "language", Header: "Language", Question: "Which language?", IsOther: true}}}, "")
			}
			ctx, cancel := context.WithCancel(context.Background())
			if choice == "bounded-expiry" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			result := make(chan json.RawMessage, 1)
			errs := make(chan error, 1)
			go func() { raw, e := a.awaitNativeRequest(ctx, j, c); result <- raw; errs <- e }()
			var n *NativeRequest
			eventually(t, "pending default-lifetime native request", func() bool { n = a.nativeRequestFor(j.ID); return n != nil })
			if choice != "bounded-expiry" && n.Expires != 0 {
				t.Fatal("invented worker timeout", n.Expires)
			}
			if choice == "bounded-expiry" {
				deadline, _ := ctx.Deadline()
				if n.Expires != deadline.UnixMilli() {
					t.Fatal("configured deadline changed")
				}
			}
			if choice == "end" {
				cancel()
			} else if choice != "bounded-expiry" {
				r := nativeResponse{Token: n.Token, Choice: choice}
				if choice == "input" {
					r.Choice = "submit"
					r.Answers = map[string]string{"language": "English"}
				}
				raw, _ := json.Marshal(r)
				text := nativeResponseDomain + string(raw)
				if e := a.RespondNativeRequest(j.ID, j.Key, 1, text, protocol.NewID()); e != nil {
					t.Fatal(e)
				}
			}
			select {
			case raw := <-result:
				e := <-errs
				if choice == "end" || choice == "bounded-expiry" {
					if e == nil || raw != nil {
						t.Fatal("ended callback replied", string(raw), e)
					}
				} else if e != nil || len(raw) == 0 {
					t.Fatal("default unlimited callback refused", e)
				}
			case <-time.After(time.Second):
				t.Fatal("native callback remained held")
			}
			a.endNativeCallback(j, c)
			if a.nativeRequestFor(j.ID) != nil {
				t.Fatal("ending retained native action")
			}
		})
	}
}
