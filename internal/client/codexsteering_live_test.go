package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Real installed Codex, one ordinary synthetic daemon, one selected executor,
// two fresh conversations and an actual turn/steer acknowledgment. Opt-in only:
// config/model/reasoning/tools/permissions stay inherited, with no approval reply
// or native retry. Nonce files prove native tools ran; stored refs, reservations,
// attempts, staged bytes and the recipient's verified answer prove the journey.
func TestCodexActualIndependentSteering(t *testing.T) {
	configured := os.Getenv("AGENTNET_LIVE_CODEX_STEERING")
	if configured == "" {
		t.Skip("opt-in: AGENTNET_LIVE_CODEX_STEERING=/absolute/path/to/codex")
	}
	if !filepath.IsAbs(configured) {
		t.Fatal("use an absolute installed Codex binary")
	}
	if runtime.GOOS == "windows" {
		t.Fatal("this native gate uses Unix shell sleep; Windows qualification is separate")
	}
	binary, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		t.Fatal("installed Codex binary unavailable", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	versionCtx, stopVersion := context.WithTimeout(ctx, 5*time.Second)
	version, err := exec.CommandContext(versionCtx, binary, "--version").Output()
	stopVersion()
	if err != nil {
		t.Fatal("installed Codex version unavailable", err)
	}
	t.Logf("installed %s; normal native config/model/reasoning/permissions inherited", strings.TrimSpace(string(version)))
	original := Harnesses["codex"]
	selected := original
	selected.bin = binary
	Harnesses["codex"] = selected
	// Fixture daemons/processes stop before the native adapter is restored.
	t.Cleanup(func() { Harnesses["codex"] = original })
	w, observer, packet, _ := groupTurnsFixture(t)
	dir := t.TempDir()
	agent, err := w.alice.CreateLocalAgent("native steering qualification", Responder{Harness: "codex", Dir: dir, Timeout: 150 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.alice.PublishAgentCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	dm := newDM(t, w.alice, w.bob)
	convs := []string{dm, packet.State.Conv}
	ids, pids, ready, tokens := make([]string, 2), make([]string, 2), make([]string, 2), make([]string, 2)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	wait := func(what string, condition func() bool) {
		t.Helper()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s: fixed 180-second native budget exhausted; no retry", what)
			case <-ticker.C:
			}
		}
	}
	type runProof struct {
		state, thread, executor string
		attempt                 int
	}
	read := func(id string) runProof {
		t.Helper()
		var proof runProof
		if err := w.alice.store.db.QueryRowContext(ctx, `SELECT state,attempts,coalesce(json_extract(session_ref,'$.id'),''),coalesce(json_extract(executor,'$.agent_id'),'') FROM inbox WHERE id=?`, id).Scan(&proof.state, &proof.attempt, &proof.thread, &proof.executor); err != nil {
			t.Fatal("native run proof unavailable", err)
		}
		if pending := w.alice.nativeRequestFor(id); pending != nil {
			t.Fatalf("native request %s needs %s approval; no approval/config override", id, pending.Method)
		}
		if proof.state != stateRunning && proof.state != stateAgentWaiting && proof.state != stateAnswered && proof.state != stateCancelReq && proof.state != stateCancelled {
			t.Fatalf("native request %s ended or refused as %s; no retry", id, proof.state)
		}
		return proof
	}
	for i, conv := range convs {
		part, err := w.alice.InviteNamedAgent(ctx, conv, w.alice.Address, agent.ID, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		pids[i] = part.PID
		wait("own named agent admission", func() bool { return stateAt(t, w.alice, part.PID).Claimable() })
		ready[i], tokens[i] = filepath.Join(dir, fmt.Sprintf("native-ready-%d", i)), "NATIVE_READY_"+protocol.NewID()
		command := fmt.Sprintf("printf '%%s' %q > %q; sleep 60", tokens[i], ready[i])
		prompt := fmt.Sprintf("Isolated native compatibility journey authorized by the local human. Use your normal shell tool exactly once to run this harmless command in this owned temporary directory: %s . It writes only the readiness nonce file and sleeps for 60 seconds. Wait for the command to return. Process any explicit corrections to this request before answering; without a correction, give only INITIAL_ONLY_%s. Do not change setup or permissions, contact anyone, use unrelated files/tools/services or automatically retry a denied operation. If native permission requires approval or the operation is unavailable, report it honestly.", command, tokens[i])
		request, err := w.alice.AskAgent(ctx, part.PID, envelope.KindQuestion, prompt)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = request.LID
	}
	var proofs [2]runProof
	wait("simultaneous actual native ready ownership", func() bool {
		for i, id := range ids {
			proofs[i] = read(id)
			if proofs[i].state == stateAnswered {
				t.Fatal("native turn completed before simultaneous ready ownership")
			}
			data, err := os.ReadFile(ready[i])
			if err != nil || string(data) != tokens[i] || proofs[i].state != stateRunning || proofs[i].thread == "" {
				return false
			}
		}
		w.alice.workerLanes.Lock()
		first, firstOwned := w.alice.workerLanes.jobs[ids[0]]
		second, secondOwned := w.alice.workerLanes.jobs[ids[1]]
		w.alice.workerLanes.Unlock()
		return firstOwned && secondOwned && first.native == proofs[0].thread && second.native == proofs[1].thread
	})
	if proofs[0].thread == proofs[1].thread || proofs[0].attempt != 1 || proofs[1].attempt != 1 || proofs[0].executor != agent.ID || proofs[1].executor != agent.ID {
		t.Fatalf("actual independent thread/attempt/executor proof missing: %+v", proofs)
	}
	t.Logf("overlap: same executor %s; requests %s/%s; actual threads %s/%s; attempts 1/1; native tool nonce files verified", agent.ID, ids[0], ids[1], proofs[0].thread, proofs[1].thread)
	if err = w.alice.Cancel(ids[0]); err != nil {
		t.Fatal(err)
	}
	wait("first cancellation and owned cleanup", func() bool {
		first, second := read(ids[0]), read(ids[1])
		if second.state != stateRunning || second.thread != proofs[1].thread || second.attempt != 1 {
			t.Fatal("canceling one conversation affected the surviving native run")
		}
		w.alice.workerLanes.Lock()
		_, owned := w.alice.workerLanes.jobs[ids[0]]
		w.alice.workerLanes.Unlock()
		return first.state == stateCancelled && !owned
	})
	marker, evidence, fileBytes := "STEERED_"+protocol.NewID(), "FRESH_EVIDENCE_"+protocol.NewID(), "FRESH_FILE_"+protocol.NewID()
	file := filepath.Join(dir, "fresh-steering-evidence.txt")
	if err = os.WriteFile(file, []byte(fileBytes), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.alice.SendConv(ctx, convs[1], ConvOutgoing{Body: evidence, Files: []OutgoingFile{{Path: file}}}); err != nil {
		t.Fatal(err)
	}
	ref := envelope.Ref{ID: ids[1], Fingerprint: w.alice.Self().Fingerprint()}
	correction := protocol.NewID()
	// This is the ordinary exact followup API, never a direct bridge call/ACK.
	if _, err = w.alice.QueueRequestFollowup(ctx, RequestFollowup{Conv: convs[1], Ref: ref, ID: correction, Body: fmt.Sprintf("Explicit correction: preserve this request and its current tool operation. Replace the INITIAL_ONLY answer with three lines: %s, the exact newly supplied FRESH_EVIDENCE marker, and the exact newly supplied FRESH_FILE contents. Use the authorized evidence/file actually supplied with this correction; do not guess or repeat effects.", marker)}); err != nil {
		t.Fatal(err)
	}
	wait("exact correction native acknowledgment", func() bool {
		state := jobState(t, w.alice, correction)
		if state == stateNeedHuman || state == stateJobFailed || state == stateNotRun {
			t.Fatalf("real native steering refused or acceptance unknown: %s; no retry", state)
		}
		return state == stateSteered
	})
	stored, err := storedRequestFollowup(w.alice.store.db, "in", correction)
	if err != nil || stored == nil || *stored != ref {
		t.Fatal("accepted correction differs from exact original ref", err)
	}
	if proof := read(ids[1]); proof.state != stateRunning || proof.thread != proofs[1].thread || proof.attempt != 1 {
		t.Fatal("accepted correction replaced or restarted the native parent")
	}
	entries, err := os.ReadDir(filepath.Join(w.alice.home, "runs", correction, "in"))
	if err != nil || len(entries) != 1 {
		t.Fatal("accepted correction has no exact staged evidence file", err)
	}
	staged := filepath.Join(w.alice.home, "runs", correction, "in", entries[0].Name())
	data, err := os.ReadFile(staged)
	info, statErr := os.Stat(staged)
	if err != nil || string(data) != fileBytes || statErr != nil || info.Mode().Perm()&0222 != 0 {
		t.Fatal("actual staged evidence bytes/readonly mode missing", err, statErr)
	}
	wait("surviving original completion", func() bool { return read(ids[1]).state == stateAnswered })
	var answer ConvMessage
	wait("recipient verified signed corrected result", func() bool {
		messages, err := observer.ConversationMessages(convs[1])
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range messages {
			if msg.ReplyTo == ids[1] && msg.PID == pids[1] && msg.Kind == envelope.KindAnswer && msg.VerifiedAgent && !msg.Replica && !msg.History && msg.From == w.alice.Address && msg.Key == w.alice.Self().Fingerprint() {
				answer = msg
				return true
			}
		}
		return false
	})
	if !strings.Contains(answer.Body, marker) || !strings.Contains(answer.Body, evidence) || !strings.Contains(answer.Body, fileBytes) || strings.Contains(answer.Body, "INITIAL_ONLY_") {
		t.Fatal("signed original answer does not contain the actually supplied correction/evidence/file facts")
	}
	if proof := read(ids[1]); proof.attempt != 1 || proof.executor != agent.ID {
		t.Fatal("original native attempt/executor changed")
	}
	var admitted admittedAgentContext
	var raw string
	if err = w.alice.store.db.QueryRowContext(ctx, `SELECT agent_context FROM inbox WHERE id=?`, ids[1]).Scan(&raw); err != nil || json.Unmarshal([]byte(raw), &admitted) != nil || admitted.Attempt != 1 {
		t.Fatal("exact native attempt context checkpoint missing", err)
	}
	t.Logf("native steering accepted for %s into original %s/thread %s/attempt 1; staged bytes and recipient verified signed corrected answer %s proven", correction, ids[1], proofs[1].thread, answer.LID)
}
