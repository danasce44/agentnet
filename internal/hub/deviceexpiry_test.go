package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/googleauth"
	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
	"github.com/misunders2d/agentnet/internal/sqlitedb"
)

func TestPermanentDeviceRemovalExpiresCustody(t *testing.T) {
	for _, mode := range []string{"linked roster", "self removal", "admin revoke", "person revoke", "person id revoke", "plain person id revoke"} {
		t.Run(mode, func(t *testing.T) {
			h, adminID, adminAddress := testHub(t)
			owner, sender, other := joinMember(t, h, "owner"), joinMember(t, h, "sender"), joinMember(t, h, "other")
			first := personOf(t, h, owner)
			phoneID, _ := identity.Generate()
			phone := member{phoneID, "owner/android-2"}
			offer := protocol.NewID()
			secret := deviceInvite(t, h, owner, offer)
			if code, e := joinLinked(t, h, secret, phone.addr, offer, first, phoneID); code != http.StatusCreated {
				t.Fatalf("link: %d %+v", code, e)
			}
			current := step(first, owner, &phone, owner.id.Public(owner.addr), phone.id.Public(phone.addr))
			if code, e := put(t, h, owner, current); code != http.StatusNoContent {
				t.Fatalf("activate: %d %+v", code, e)
			}
			pending := []string{sendMessage(t, h, sender, phone), sendMessage(t, h, sender, phone), sendMessage(t, h, other, phone)}
			if _, err := h.store.db.Exec(`UPDATE messages SET lane='sync',session='old',fallback=1 WHERE id=?`, pending[1]); err != nil {
				t.Fatal(err)
			}
			delivered, quarantined := sendMessage(t, h, sender, phone), sendMessage(t, h, sender, phone)
			for id, state := range map[string]string{delivered: protocol.StateDelivered, quarantined: protocol.StateQuarantined} {
				if code, b := phone.call(t, h, "POST", "/v1/messages/"+id+"/ack", protocol.AckRequest{State: state}); code != 200 {
					t.Fatalf("ack: %d %s", code, b)
				}
			}
			unaffected, outgoing := sendMessage(t, h, sender, other), sendMessage(t, h, phone, sender)
			saved := map[string][]byte{}
			for _, id := range append(append([]string{}, pending...), delivered, quarantined, unaffected, outgoing) {
				var data []byte
				if err := h.store.db.QueryRow(`SELECT envelope FROM messages WHERE id=?`, id).Scan(&data); err != nil {
					t.Fatal(err)
				}
				saved[id] = data
			}
			sub := h.streams.add(sender.addr, func() {})
			defer h.streams.remove(sender.addr, sub)
			phoneDisconnected, ownerDisconnected := false, false
			phoneSub := h.streams.add(phone.addr, func() { phoneDisconnected = true })
			defer h.streams.remove(phone.addr, phoneSub)
			ownerSub := h.streams.add(owner.addr, func() { ownerDisconnected = true })
			defer h.streams.remove(owner.addr, ownerSub)
			pendingAddress := ""
			pendingDisconnected := false
			if strings.Contains(mode, "person") {
				pendingID, _ := identity.Generate()
				pendingAddress = "owner/pending"
				offer := protocol.NewID()
				secret := deviceInvite(t, h, owner, offer)
				if code, e := joinLinked(t, h, secret, pendingAddress, offer, current, pendingID); code != 201 {
					t.Fatalf("pending link: %d %+v", code, e)
				}
				pendingSub := h.streams.add(pendingAddress, func() { pendingDisconnected = true })
				defer h.streams.remove(pendingAddress, pendingSub)
				if _, err := h.store.db.Exec(`INSERT INTO google_domains(domain) VALUES('example.com')`); err != nil {
					t.Fatal(err)
				}
			}
			remove := func() {
				switch mode {
				case "linked roster", "self removal":
					signer := owner
					if mode == "self removal" {
						signer = phone
					}
					if code, e := put(t, h, signer, step(current, signer, nil, owner.id.Public(owner.addr))); code != http.StatusNoContent {
						t.Fatalf("remove: %d %+v", code, e)
					}
				case "admin revoke":
					if _, err := h.store.db.Exec(`UPDATE agents SET admin=1 WHERE address=?`, adminAddress); err != nil {
						t.Fatal(err)
					}
					if code, b := (member{adminID, adminAddress}).call(t, h, "POST", "/v1/admin/revoke", protocol.RevokeRequest{Address: phone.addr}); code != 200 {
						t.Fatalf("revoke: %d %s", code, b)
					}
				case "person revoke", "person id revoke", "plain person id revoke":
					if _, err := h.store.db.Exec(`UPDATE agents SET admin=1 WHERE address=?`, adminAddress); err != nil {
						t.Fatal(err)
					}
					if mode != "plain person id revoke" {
						if _, err := h.store.db.Exec(`INSERT OR IGNORE INTO google_people(email,subject,person) VALUES('owner@example.com','fixture',?)`, current.Person); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "person revoke" {
						if code, b := (member{adminID, adminAddress}).call(t, h, "PUT", "/v1/google/access", protocol.GoogleAccessChange{Email: "owner@example.com", Remove: true}); code != 204 {
							t.Fatalf("email offboard: %d %s", code, b)
						}
					} else {
						if code, b := (member{adminID, adminAddress}).call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: current.Person}); code != 200 {
							t.Fatalf("person id offboard: %d %s", code, b)
						}
					}
				}
			}
			remove()
			if !phoneDisconnected {
				t.Fatal("removed phone's open stream not disconnected")
			}
			if strings.Contains(mode, "person") {
				if !ownerDisconnected || !pendingDisconnected {
					t.Fatal("person removal left current/pending streams open")
				}
				for _, address := range []string{owner.addr, phone.addr, pendingAddress} {
					a, err := h.store.agent(address)
					if err != nil || !a.Revoked {
						t.Fatalf("person key not revoked: %s %v", address, err)
					}
				}
				if mode != "plain person id revoke" {
					_, err := googleAllowed(h.store.db, googleauth.Claims{Email: "owner@example.com", Domain: "example.com", Subject: "fixture"})
					if !errors.Is(err, errGoogleAccess) {
						t.Fatalf("denied email allowed through domain: %v", err)
					}
				}
			}
			for _, state := range []string{protocol.StateDelivered, protocol.StateQuarantined} {
				if code, b := phone.call(t, h, "POST", "/v1/messages/"+pending[0]+"/ack", protocol.AckRequest{State: state}); code != 403 {
					t.Fatalf("revoked ack admitted: %d %s", code, b)
				}
				if _, _, changed, err := h.store.setDisposition(pending[0], phone.addr, state); !errors.Is(err, errRevoked) || changed {
					t.Fatalf("in-flight ack admitted: changed=%v %v", changed, err)
				}
			}
			for _, id := range pending {
				_, _, state, err := h.store.messageState(id, sender.addr)
				if id == pending[2] {
					_, _, state, err = h.store.messageState(id, other.addr)
				}
				if err != nil || state != protocol.StateExpired {
					t.Fatalf("pending copy %s: %q %v", id, state, err)
				}
			}
			for id, want := range map[string]string{delivered: protocol.StateDelivered, quarantined: protocol.StateQuarantined, unaffected: protocol.StateCustody, outgoing: protocol.StateCustody} {
				var state string
				if err := h.store.db.QueryRow(`SELECT state FROM messages WHERE id=?`, id).Scan(&state); err != nil || state != want {
					t.Fatalf("other disposition %s: %s %v", id, state, err)
				}
			}
			for id, want := range saved {
				var data []byte
				if err := h.store.db.QueryRow(`SELECT envelope FROM messages WHERE id=?`, id).Scan(&data); err != nil || !bytes.Equal(data, want) {
					t.Fatalf("ciphertext changed/deleted %s: %v", id, err)
				}
			}
			receipts, err := h.store.receiptsFor(sender.addr, 0)
			if err != nil || len(receipts) != 4 {
				t.Fatalf("sender receipts: %+v %v", receipts, err)
			}
			if receipts[2].State != protocol.StateExpired || receipts[3].State != protocol.StateExpired || receipts[2].Seq != 3 || receipts[3].Seq != 4 {
				t.Fatalf("expiry receipts: %+v", receipts)
			}
			foreign, err := h.store.receiptsFor(other.addr, 0)
			if err != nil || len(foreign) != 1 || foreign[0].ID != pending[2] || foreign[0].State != protocol.StateExpired {
				t.Fatalf("foreign receipts: %+v %v", foreign, err)
			}
			select {
			case <-sub.wake:
			default:
				t.Fatal("removal did not wake sender's existing push stream")
			}
			if mode == "linked roster" || strings.Contains(mode, "person id") {
				remove()
				again, _ := h.store.receiptMax(sender.addr)
				if again != 4 {
					t.Fatalf("replay appended expiry receipts: %d", again)
				}
			}
		})
	}
}

func TestStandaloneRosterRemovalKeepsCustody(t *testing.T) {
	h, _, _ := testHub(t)
	owner, sender := joinMember(t, h, "owner"), joinMember(t, h, "sender")
	first := personOf(t, h, owner)
	phoneID, _ := identity.Generate()
	phone := member{phoneID, "owner/phone"}
	offer := protocol.NewID()
	secret := deviceInvite(t, h, owner, offer)
	if code, e := joinLinked(t, h, secret, phone.addr, offer, first, phoneID); code != 201 {
		t.Fatalf("link: %d %+v", code, e)
	}
	current := step(first, owner, &phone, owner.id.Public(owner.addr), phone.id.Public(phone.addr))
	if code, e := put(t, h, owner, current); code != 204 {
		t.Fatalf("activate: %d %+v", code, e)
	}
	id := sendMessage(t, h, sender, owner)
	if code, e := put(t, h, phone, step(current, phone, nil, phone.id.Public(phone.addr))); code != 204 {
		t.Fatalf("detach: %d %+v", code, e)
	}
	_, _, state, err := h.store.messageState(id, sender.addr)
	if err != nil || state != protocol.StateCustody {
		t.Fatalf("standalone recipient custody: %s %v", state, err)
	}
	a, err := h.store.agent(owner.addr)
	if err != nil || a.Revoked || a.Person != "" {
		t.Fatalf("standalone recipient: %+v %v", a, err)
	}
	receipts, err := h.store.receiptsFor(sender.addr, 0)
	if err != nil || len(receipts) != 0 {
		t.Fatalf("phantom expiry receipts: %+v %v", receipts, err)
	}
}

func TestDeviceRevocationPushesExpiredReceipt(t *testing.T) {
	h, sender, phone, _ := blobHub(t, 1<<30)
	id := sendMessage(t, h, sender, phone)
	if _, err := h.store.db.Exec(`UPDATE agents SET admin=1 WHERE address=?`, sender.addr); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pr, pw := io.Pipe()
	defer pr.Close()
	ad := protocol.SessionAd{Address: sender.addr, Session: protocol.NewID()}
	protocol.SignAd(&ad, sender.id.Sign)
	req := signed(t, sender.id, sender.addr, "GET", "/v1/stream?ad="+ad.Encode()+"&receipts=0", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.routes().ServeHTTP(&pipeResponse{header: http.Header{}, w: pw}, req)
		pw.Close()
	}()
	defer func() { cancel(); pr.Close(); <-done }()
	guard := time.AfterFunc(3*time.Second, func() { pr.Close() })
	defer guard.Stop()
	scanner := bufio.NewScanner(pr)
	// Read the first release frame: subscription is registered before revoke.
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			break
		}
	}
	if code, b := sender.call(t, h, "POST", "/v1/admin/revoke", protocol.RevokeRequest{Address: phone.addr}); code != 200 {
		t.Fatalf("revoke: %d %s", code, b)
	}
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if event == "receipt" && strings.HasPrefix(line, "data: ") {
			var receipt protocol.ReceiptEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &receipt); err != nil || receipt.ID != id || receipt.State != protocol.StateExpired || receipt.Seq != 1 {
				t.Fatalf("pushed receipt: %+v %v", receipt, err)
			}
			return
		}
	}
	t.Fatalf("no live terminal receipt: %v", scanner.Err())
}

func TestRevokedDeviceAdmissionRecheckedInTransaction(t *testing.T) {
	for _, who := range []string{"sender", "recipient"} {
		t.Run(who, func(t *testing.T) {
			h, sender, recipient, _ := blobHub(t, 1<<30)
			// An already parsed and signature-verified envelope models an HTTP
			// handler that authenticated just before the removal transaction.
			env := laneTestEnvelope(t, sender, recipient)
			canonical, _ := json.Marshal(env)
			target, want := recipient.addr, errRecipientRevoked
			if who == "sender" {
				target, want = sender.addr, errRevoked
			}
			if err := h.store.revoke(target); err != nil {
				t.Fatal(err)
			}
			if _, err := h.store.putMessage(env, canonical, sender.id.Public(sender.addr).Fingerprint(), time.Now(), protocol.MessageLaneLive); !errors.Is(err, want) {
				t.Fatalf("racing admission: %v", err)
			}
			var n int
			if err := h.store.db.QueryRow(`SELECT count(*) FROM messages WHERE id=?`, env.ID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("revoked copy stored: %d %v", n, err)
			}
		})
	}
}

// Phone replacement must not erase something the owner already sent. A
// recipient may receive it after the sender is revoked, but old requests
// cannot enter an older client's worker and new messages are refused.
func TestRevokedSenderRetainsAcceptedCommunication(t *testing.T) {
	h, sender, recipient, _ := blobHub(t, 1<<30)
	type copy struct {
		id, kind string
		raw      []byte
	}
	var copies []copy
	for _, kind := range []string{envelope.KindMessage, envelope.KindAnswer, envelope.KindResult, envelope.KindQuestion, envelope.KindTask} {
		r, _ := recipient.id.Public(recipient.addr).Recipient()
		in := envelope.Inner{ID: protocol.NewID(), From: sender.addr, To: recipient.addr, TS: time.Now().Unix(), Kind: kind, Body: "accepted before removal"}
		if kind == envelope.KindAnswer || kind == envelope.KindResult {
			in.ReplyTo, in.Status = protocol.NewID(), "ok"
		}
		env, err := envelope.Seal(in, sender.id.Sign, r)
		if err != nil {
			t.Fatal(err)
		}
		if c, b := sender.call(t, h, "POST", "/v1/messages", env); c != http.StatusAccepted {
			t.Fatalf("%s post: %d %s", kind, c, b)
		}
		raw, _ := json.Marshal(env)
		copies = append(copies, copy{env.ID, kind, raw})
	}
	syncID := sendMessage(t, h, sender, recipient)
	if _, err := h.store.db.Exec(`UPDATE messages SET lane='sync' WHERE id=?`, syncID); err != nil {
		t.Fatal(err)
	}
	if err := h.store.revoke(sender.addr); err != nil {
		t.Fatal(err)
	}
	for _, c := range copies {
		_, _, state, err := h.store.messageState(c.id, recipient.addr)
		want := protocol.StateCustody
		if c.kind == envelope.KindQuestion || c.kind == envelope.KindTask {
			want = protocol.StateExpired
		}
		if err != nil || state != want {
			t.Fatalf("%s: %s want %s (%v)", c.kind, state, want, err)
		}
		if want == protocol.StateCustody {
			if code, b := recipient.call(t, h, "POST", "/v1/messages/"+c.id+"/ack", protocol.AckRequest{State: protocol.StateDelivered}); code != 200 {
				t.Fatalf("late delivery: %d %s", code, b)
			}
		}
		var raw []byte
		if err := h.store.db.QueryRow(`SELECT envelope FROM messages WHERE id=?`, c.id).Scan(&raw); err != nil || !bytes.Equal(raw, c.raw) {
			t.Fatalf("accepted bytes changed: %v", err)
		}
	}
	_, _, state, err := h.store.messageState(syncID, recipient.addr)
	if err != nil || state != protocol.StateExpired {
		t.Fatalf("revoked sync: %s %v", state, err)
	}
	if code, b := sender.call(t, h, "POST", "/v1/messages", laneTestEnvelope(t, sender, recipient)); code != http.StatusForbidden {
		t.Fatalf("new send after removal: %d %s", code, b)
	}
}

func TestUpgradeRepairsEveryRevokedCustodyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	oldDB, err := sqlitedb.Open(path, schema[:len(schema)-1])
	if err != nil {
		t.Fatal(err)
	}
	old := &Hub{store: &store{db: oldDB}}
	sender, removed, other := enroll(t, old, "sender"), enroll(t, old, "removed"), enroll(t, old, "other")
	const copies = 300
	const retainedWire = `{"kind":"question","ct":"a2VwdA=="}`
	for i := 0; i < copies; i++ {
		from, to := sender.addr, removed.addr
		if i%2 != 0 {
			from, to = removed.addr, other.addr
		}
		if _, err := oldDB.Exec(`INSERT INTO messages(id,sender,recipient,envelope,state,created_at) VALUES(?,?,?,?,'custody',1)`, fmt.Sprintf("old-%d", i), from, to, []byte(retainedWire)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := oldDB.Exec(`INSERT INTO messages(id,sender,recipient,envelope,state,created_at) VALUES('untouched',?,?,?,'custody',1)`, sender.addr, other.addr, []byte(`{"kind":"message","ct":"b3RoZXI="}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := oldDB.Exec(`INSERT INTO messages(id,sender,recipient,envelope,state,created_at) VALUES('accepted-before-removal',?,?,?,'custody',1)`, removed.addr, other.addr, []byte(`{"kind":"message","ct":"a2VwdA=="}`)); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, from string }{{"opaque-unrelated", sender.addr}, {"opaque-removed", removed.addr}} {
		if _, err := oldDB.Exec(`INSERT INTO messages(id,sender,recipient,envelope,state,created_at) VALUES(?,?,?,?,'custody',1)`, row.id, row.from, other.addr, []byte("legacy opaque bytes")); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the shipped version: revoke metadata without settling copies.
	if _, err := oldDB.Exec(`UPDATE agents SET revoked_at=1 WHERE address=?`, removed.addr); err != nil {
		t.Fatal(err)
	}
	oldDB.Close()
	for i := 0; i < 2; i++ {
		repaired, err := openStore(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []member{sender, removed} {
			max, err := repaired.receiptMax(m.addr)
			want := int64(copies / 2)
			if m.addr == removed.addr {
				want++ // an unreadable removed-sender record cannot prove ordinary content
			}
			if i != 0 && m.addr == removed.addr {
				want++
			} // its retained message arrived after removal
			if err != nil || max != want {
				t.Fatalf("all copies/replay for %s: %d %v", m.addr, max, err)
			}
		}
		var expired, preserved int
		if err := repaired.db.QueryRow(`SELECT count(*) FROM messages WHERE state='expired'`).Scan(&expired); err != nil || expired != copies+1 {
			t.Fatalf("repair count: %d %v", expired, err)
		}
		if err := repaired.db.QueryRow(`SELECT count(*) FROM messages WHERE envelope=?`, []byte(retainedWire)).Scan(&preserved); err != nil || preserved != copies {
			t.Fatalf("repair ciphertext: %d %v", preserved, err)
		}
		_, _, state, err := repaired.messageState("untouched", sender.addr)
		if err != nil || state != protocol.StateCustody {
			t.Fatalf("unrelated custody changed: %s %v", state, err)
		}
		for id, want := range map[string]string{"opaque-unrelated": protocol.StateCustody, "opaque-removed": protocol.StateExpired} {
			var raw []byte
			var state string
			if err := repaired.db.QueryRow(`SELECT envelope,state FROM messages WHERE id=?`, id).Scan(&raw, &state); err != nil || state != want || string(raw) != "legacy opaque bytes" {
				t.Fatalf("opaque record %s: state=%s bytes=%q err=%v", id, state, raw, err)
			}
		}
		_, _, state, err = repaired.messageState("accepted-before-removal", other.addr)
		want := protocol.StateCustody
		if i != 0 {
			want = protocol.StateDelivered
		}
		if err != nil || state != want {
			t.Fatalf("accepted sender history lost: %s %v", state, err)
		}
		if i == 0 {
			_, _, changed, err := repaired.setDisposition("accepted-before-removal", other.addr, protocol.StateDelivered)
			if err != nil || !changed {
				t.Fatalf("accepted history cannot arrive: %v %v", changed, err)
			}
		}
		// A terminal ack cannot turn an expired historical copy into delivery.
		_, _, changed, err := repaired.setDisposition("old-1", other.addr, protocol.StateDelivered)
		if err != nil || changed {
			t.Fatalf("expired state flipped: %v %v", changed, err)
		}
		repaired.db.Close()
	}
}

func TestPersonOffboardingProtectsAuthorityAndInvalidatesLinks(t *testing.T) {
	h, _, _ := testHub(t)
	boss := enrollAdmin(t, h, "boss")
	own := personOf(t, h, boss)
	granted := adminLinkedDevice(t, h, boss, &own, "boss/phone")
	if err := h.store.setDeviceAdmin(boss.addr, granted.addr, true); err != nil {
		t.Fatal(err)
	}
	otherAdmin := enrollAdmin(t, h, "protected")
	protected := personOf(t, h, otherAdmin)
	victim := joinMember(t, h, "victim")
	person := personOf(t, h, victim)
	pending := adminLinkedDevice(t, h, victim, &person, "victim/phone")
	offer := protocol.NewID()
	unused := deviceInvite(t, h, victim, offer)
	for _, tc := range []struct {
		caller member
		person string
		code   int
	}{
		{victim, person.Person, 403}, {boss, own.Person, 403}, {granted, protected.Person, 403}, {boss, "display name", 400}, {boss, protocol.NewID(), 404},
	} {
		if code, b := tc.caller.call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: tc.person}); code != tc.code {
			t.Fatalf("protected offboard: %d want%d %s", code, tc.code, b)
		}
	}
	for _, m := range []member{boss, granted, otherAdmin, victim, pending} {
		a, err := h.store.agent(m.addr)
		if err != nil || a.Revoked {
			t.Fatalf("refused action changed %s: %v", m.addr, err)
		}
	}
	if code, b := boss.call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: person.Person}); code != 200 {
		t.Fatalf("offboard: %d %s", code, b)
	}
	fresh, _ := identity.Generate()
	if code, _ := joinLinked(t, h, unused, "victim/new", offer, person, fresh); code == 201 {
		t.Fatal("revoked person's outstanding device invite admitted a new key")
	}
	// Even an invite whose expiry was read before offboarding must recheck its
	// exact author inside the enrollment transaction.
	if _, err := h.store.db.Exec(`UPDATE invites SET expires_at=? WHERE secret_hash=?`, time.Now().Add(time.Minute).Unix(), protocol.HashSecret(unused)); err != nil {
		t.Fatal(err)
	}
	if code, _ := joinLinked(t, h, unused, "victim/racing", offer, person, fresh); code != 403 {
		t.Fatalf("in-flight revoked-author invite: %d", code)
	}
	if err := h.store.revoke(boss.addr); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.revokePerson(boss.addr, protected.Person); !errors.Is(err, errRevoked) {
		t.Fatalf("transaction caller revocation: %v", err)
	}
	a, _ := h.store.agent(otherAdmin.addr)
	if a.Revoked {
		t.Fatal("revoked admin raced a person offboard")
	}
}

func TestPersonIDRevocationIncludesDetachedGoogleEnrollment(t *testing.T) {
	h, _, _ := testHub(t)
	admin := enrollAdmin(t, h, "boss")
	claims := googleauth.Claims{Email: "legacy@example.com", Domain: "example.com", Subject: "fixture"}
	if _, err := h.store.changeGoogleAccess(protocol.GoogleAccessChange{Domain: claims.Domain}); err != nil {
		t.Fatal(err)
	}
	key, _ := identity.Generate()
	pub := key.Public(protocol.GoogleLabel(claims.Email) + "/laptop")
	roster := protocol.PersonRoster{Person: protocol.NewID(), Label: "Legacy", Email: claims.Email, Devices: []identity.Public{pub}}
	roster.Sign(key.Sign)
	if _, _, err := h.store.enrollGoogle(claims, protocol.GoogleRequest{Public: pub, First: &roster}); err != nil {
		t.Fatal(err)
	}
	phone := member{key, pub.Address}
	id := sendMessage(t, h, admin, phone)
	if _, err := h.store.db.Exec(`UPDATE agents SET person_id=NULL,linked=0 WHERE address=?`, pub.Address); err != nil {
		t.Fatal(err)
	}
	if code, b := admin.call(t, h, "POST", "/v1/admin/person/revoke", protocol.RevokePersonRequest{Person: roster.Person}); code != 200 {
		t.Fatalf("offboard: %d %s", code, b)
	}
	target, err := h.store.agent(pub.Address)
	if err != nil || !target.Revoked {
		t.Fatalf("detached Google key stayed active: %+v %v", target, err)
	}
	_, _, state, err := h.store.messageState(id, admin.addr)
	if err != nil || state != protocol.StateExpired {
		t.Fatalf("detached key custody: %s %v", state, err)
	}
	fresh, _ := identity.Generate()
	newPub := fresh.Public(protocol.GoogleLabel(claims.Email) + "/new")
	newRoster := protocol.PersonRoster{Person: protocol.NewID(), Label: "Legacy", Email: claims.Email, Devices: []identity.Public{newPub}}
	newRoster.Sign(fresh.Sign)
	if _, _, err := h.store.enrollGoogle(claims, protocol.GoogleRequest{Public: newPub, First: &newRoster}); !errors.Is(err, errGoogleAccess) {
		t.Fatalf("fired email re-enrolled through allowed domain: %v", err)
	}
}
