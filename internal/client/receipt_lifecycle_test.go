package client

import (
	"path/filepath"
	"testing"

	"github.com/misunders2d/agentnet/internal/protocol"
)

func testLifecycleReceiptRequiresPossibleRequestHandover(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	for _, kind := range []string{"question", "task"} {
		for _, started := range []int{0, 1} {
			id := kind + string(rune('0'+started))
			if _, err = s.db.Exec(`INSERT INTO outbox(id,recipient,recipient_fp,kind,body,envelope,state,created_at,handover_started) VALUES(?,'person/phone','key',?,'original','ciphertext','queued',1,?)`, id, kind, started); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = s.endRecipient("person/phone", "key", true); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"question", "task"} {
		for _, started := range []int{0, 1} {
			id := kind + string(rune('0'+started))
			if err = s.applyReceipt(protocol.ReceiptEvent{ID: id, State: protocol.StateDelivered, Seq: 1}); err != nil {
				t.Fatal(err)
			}
			want := stateNotDelivered
			if started == 1 {
				want = protocol.StateDelivered
			}
			var state, body string
			if err = s.db.QueryRow(`SELECT state,body FROM outbox WHERE id=?`, id).Scan(&state, &body); err != nil || state != want || body != "original" {
				t.Fatalf("%s state=%s want=%s err=%v", id, state, want, err)
			}
		}
	}
}
