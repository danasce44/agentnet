package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Uses the installed binary and normal config in a fresh owned process. The
// sole authorized effect is a fixture file; no native permission is overridden.
func TestNativeApprovalActualConfiguredWrite(t *testing.T) {
	binary := os.Getenv("AGENTNET_LIVE_NATIVE_APPROVAL")
	if binary == "" {
		t.Skip("opt-in: absolute installed Codex binary")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("use the exact installed binary")
	}
	if info, e := os.Stat(binary); e != nil || !info.Mode().IsRegular() {
		t.Fatal("installed binary unavailable", e)
	}
	a, j, _, _ := nativeApprovalFixture(t)
	dir := t.TempDir()
	expected := "NATIVE_OPERATION_PROOF_" + j.ID
	command := fmt.Sprintf("printf '%%s' '%s' > native-result.txt", expected)
	// The installed native shell displays its fixed wrapper and local
	// environment too. Accept only these two exact fixture operations.
	wrapped := "/usr/bin/bash -lc " + strconv.Quote(command) + "\nNative execution environment: local"
	prompt := fmt.Sprintf("Isolated native compatibility test authorized by the local human. In the current working folder, run exactly this native shell command once: %s . This creates only native-result.txt here. Do not write anywhere else, send any messages, or use external tools or services. If your native configuration asks for approval, keep that exact operation pending for the human. Do not replace native permissions or retry a denied action. Then report the exact file content.", command)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := newCodexRoomBridge(ctx, cmd, prompt, &stdout, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	b.native = func(ctx context.Context, c nativeCallback) (json.RawMessage, error) {
		return a.awaitNativeRequest(ctx, j, c)
	}
	b.nativeCheck = func(c nativeCallback) bool { return a.nativeHandoverCurrent(j, c) }
	b.nativeComplete = func(c nativeCallback) { a.endNativeCallback(j, c) }
	cmd.WaitDelay = time.Second
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	callbacks := 0
	seen := map[string]bool{}
	for {
		select {
		case e = <-done:
			b.finish()
			data, readErr := os.ReadFile(filepath.Join(dir, "native-result.txt"))
			if e != nil || readErr != nil || string(data) != expected {
				t.Fatalf("unchanged native write did not complete: process=%v file=%v callbacks=%d native-output=%q", e, readErr, callbacks, stdout.String())
			}
			if !strings.Contains(stdout.String(), expected) {
				t.Fatal("native final answer did not report the actual result")
			}
			t.Logf("actual installed Codex, inherited config, fresh owned process: isolated exact file PASS; %d genuine approval callback(s), no override/new run/open session", callbacks)
			return
		case <-ctx.Done():
			t.Fatal("native fixture exceeded existing 120-second test budget; no retry or policy change")
		case <-ticker.C:
			if n := a.nativeRequestFor(j.ID); n != nil && !seen[n.Token] {
				seen[n.Token] = true
				callbacks++
				// The test human authorizes only the exact displayed fixture command.
				if e = nativeFixtureApprovalError(n, command, wrapped, dir); e != nil {
					t.Fatal(e)
				}
				if e = a.RespondNativeRequest(j.ID, j.Key, 1, nativeResponseText(n.Token, "allow"), j.ID); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
}

func nativeFixtureApprovalError(n *NativeRequest, command, wrapped, dir string) error {
	if n.Method == "item/commandExecution/requestApproval" && (n.Operation == command || n.Operation == wrapped) && n.CWD == dir {
		return nil
	}
	// Even a command containing the expected nonce may include unrelated text.
	return fmt.Errorf("native requested a different operation: method=%s cwd_matches=%v operation=sha256:%x; no response authorized", n.Method, n.CWD == dir, sha256.Sum256([]byte(n.Operation)))
}

func TestNativeApprovalFixtureExactDisplay(t *testing.T) {
	command := "printf '%s' 'NATIVE_OPERATION_PROOF_fixture' > native-result.txt"
	wrapped := "/usr/bin/bash -lc " + strconv.Quote(command) + "\nNative execution environment: local"
	dir := "/owned/fixture"
	for _, tt := range []struct {
		name, method, operation, cwd string
		allow                        bool
	}{
		{"literal", "item/commandExecution/requestApproval", command, dir, true},
		{"native-display", "item/commandExecution/requestApproval", wrapped, dir, true},
		{"extra-command", "item/commandExecution/requestApproval", command + "; echo UNRELATED_NATIVE_CONTENT", dir, false},
		{"wrapped-extra-command", "item/commandExecution/requestApproval", wrapped + "\nUNRELATED_NATIVE_CONTENT", dir, false},
		{"extra-whitespace", "item/commandExecution/requestApproval", command + "\n", dir, false},
		{"network", "item/commandExecution/requestApproval", wrapped + "\nNative network request: UNRELATED_NATIVE_CONTENT", dir, false},
		{"permissions", "item/commandExecution/requestApproval", wrapped + "\nAdditional native permissions: UNRELATED_NATIVE_CONTENT", dir, false},
		{"other-method", "item/permissions/requestApproval", command, dir, false},
		{"other-cwd", "item/commandExecution/requestApproval", command, dir + "/other", false},
		{"other-command", "item/commandExecution/requestApproval", "UNRELATED_NATIVE_CONTENT", dir, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := &NativeRequest{Method: tt.method, Operation: tt.operation, CWD: tt.cwd}
			err := nativeFixtureApprovalError(n, command, wrapped, dir)
			if (err == nil) != tt.allow {
				t.Fatalf("allow=%v, error=%v", tt.allow, err)
			}
			if err != nil {
				wantHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(tt.operation)))
				if !strings.Contains(err.Error(), wantHash) || strings.Contains(err.Error(), "NATIVE_OPERATION_PROOF_fixture") || strings.Contains(err.Error(), "UNRELATED_NATIVE_CONTENT") {
					t.Fatal("rejected operation was not reduced to its exact diagnostic hash")
				}
			}
		})
	}
}
