package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
)

// Opt-in native installation check. It keeps the real owner environment and
// tools, uses disposable Hub/identities/workspace, and never resumes an open
// session or old request. The output file proves a native tool actually ran.
func TestLiveNativeHarnessGroupTools(t *testing.T) {
	name := os.Getenv("AGENTNET_LIVE_NATIVE")
	if name == "" {
		t.Skip("opt-in: AGENTNET_LIVE_NATIVE=pi|claude|omp|codex|agy")
	}
	if _, ok := Harnesses[name]; !ok {
		t.Fatal("unknown native harness")
	}
	launch, err := resolveNativeLaunch(Harnesses[name].bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native %s via %s (%s)", name, launch.Path, launch.Installation)
	if os.Getenv("AGENTNET_LIVE_NATIVE_ACTION") == "delegate" {
		switch name {
		case "pi", "claude", "omp", "codex", "agy":
		default:
			t.Fatal("native task delegation is not qualified for this harness")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
		defer cancel()
		actualNativeRoomQuestionSmoke(t, ctx, name, envelope.KindTask)
		return
	}
	w, carol, packet, _ := groupTurnsFixture(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "read-fixture.txt")
	result := filepath.Join(dir, "written-fixture.txt")
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		t.Fatal(err)
	}
	expected := "native-tool-" + hex.EncodeToString(token)
	if err := os.WriteFile(source, []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.alice.SetResponder(&Responder{Harness: name, Dir: dir, Timeout: 150 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if err := w.alice.Approve(carol.Address); err != nil {
		t.Fatal(err)
	}
	participant := p6Member(t, w.alice, w.alice, packet.State.Conv)
	eventually(t, "requester sees native participant", func() bool { return stateAt(t, carol, participant.PID).Claimable() })
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	action := os.Getenv("AGENTNET_LIVE_NATIVE_ACTION")
	prompt := fmt.Sprintf("Isolated native tool verification. Use your normal filesystem tools to read %q and copy its exact contents to %q, then reply with those exact contents. These two files are the complete task scope. Do not alter any other file, contact anyone, change setup or permission settings, or use external services other than your normal model provider. If your native permissions require approval, report that honestly. Do not guess the file contents.", source, result)
	if action == "read" {
		prompt = fmt.Sprintf("Isolated native tool verification. Read %q using your normal filesystem tools, then reply with its exact contents. Do not modify files or contact anyone. Do not guess the contents.", source)
	}
	sent, err := carol.AskAgent(ctx, participant.PID, envelope.KindTask, prompt)
	if err != nil {
		t.Fatal(err)
	}
	// Cross-person fixture acceptance is explicit; own-device consent is covered
	// by its separate phone/second-person journeys. No live grant is modified.
	waitState(t, w.alice, sent.LID, stateAwaiting)
	if err := w.alice.Accept(sent.LID); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := jobState(t, w.alice, sent.LID)
		if state == stateAnswered {
			if action == "approval" {
				t.Fatal("expected this native setup to require approval; no approval-path evidence")
			}
			break
		}
		if state == stateJobFailed || state == stateNeedHuman || state == stateInterrupt || state == stateDeclined {
			row := inboxRow(t, w.alice, sent.LID)
			if action == "approval" && state == stateNeedHuman && strings.Contains(row.Detail, "native approval") && strings.Contains(row.Detail, "read-only") {
				if _, err := os.Stat(result); !os.IsNotExist(err) {
					t.Fatalf("unapproved tool changed output: %v", err)
				}
				t.Log("Actual native read-only approval request preserved as needs-human; no approval/config override or filesystem write. Reason:", row.Detail)
				return
			}
			t.Fatalf("native task ended %s without verified completion: %s", state, row.Detail)
		}
		select {
		case <-ctx.Done():
			t.Fatal("native fixture budget exceeded; no retry")
		case <-ticker.C:
		}
	}
	data, err := os.ReadFile(result)
	if action != "read" && (err != nil || string(data) != expected) {
		t.Fatalf("native filesystem effect missing: %v", err)
	}
	eventually(t, "signed attributed result reaches requester", func() bool {
		messages, err := carol.ConversationMessages(packet.State.Conv)
		if err != nil {
			return false
		}
		for _, m := range messages {
			if m.ReplyTo == sent.LID && m.Kind == envelope.KindResult && m.PID == participant.PID && m.From == w.alice.Address && m.Key == w.alice.Self().Fingerprint() && strings.Contains(m.Body, expected) {
				return true
			}
		}
		return false
	})
	t.Logf("native filesystem %s and signed attributed group result verified; other tool families remain separate gates", map[bool]string{true: "read", false: "read/write"}[action == "read"])
}
