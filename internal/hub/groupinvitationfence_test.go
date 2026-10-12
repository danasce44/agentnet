package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/misunders2d/agentnet/internal/identity"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func TestGroupInvitationFenceOfflineOriginAndLegacy(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel-first", false: "publish-first"}[cancelFirst], func(t *testing.T) {
			h, _, _ := testHub(t)
			desk := joinMember(t, h, "fence")
			r0 := personOf(t, h, desk)
			offer := protocol.NewID()
			secret := deviceInvite(t, h, desk, offer)
			phoneID, _ := identity.Generate()
			phone := member{phoneID, "fence/phone"}
			if code, e := joinLinked(t, h, secret, phone.addr, offer, r0, phoneID); code != 201 {
				t.Fatalf("link %d %+v", code, e)
			}
			roster := step(r0, desk, &phone, desk.id.Public(desk.addr), phoneID.Public(phone.addr))
			if code, e := put(t, h, desk, roster); code != 204 {
				t.Fatalf("activate %d %+v", code, e)
			}
			realm := h.RealmID()
			conv := roster.Hash()
			bootstrap := desk.id.Public(desk.addr).Fingerprint()
			commit := func(seq int64, prev string) protocol.GroupCommit {
				c := protocol.GroupCommit{V: 1, Conv: conv, Realm: realm, Bootstrap: bootstrap, Seq: seq, Prev: prev, Hash: firstRoster(desk, protocol.NewID()).Hash(), Admins: []string{roster.Person}, Writer: desk.addr, Actor: roster.Person, ActorRoster: roster.Hash(), Ciphertext: []byte("same encrypted pending bytes")}
				c.Sign(desk.id.Sign)
				return c
			}
			root := commit(0, "")
			if _, e := h.store.putGroupCommit(desk.id.Public(desk.addr), root, realm, ""); e != nil {
				t.Fatal(e)
			}
			id := firstRoster(desk, protocol.NewID()).Hash()
			pending := commit(1, root.Hash)
			original, _ := json.Marshal(pending)
			c := protocol.GroupInvitationCancel{V: 1, Realm: realm, Conv: conv, Bootstrap: bootstrap, Person: roster.Person, Invitation: id, Seq: 1, Prev: root.Hash, Head: root.Hash}
			publishPath := "/v1/groups/" + conv + "/publish?creator=" + bootstrap
			for _, body := range []any{map[string]any{"v": 1, "commit": pending}, map[string]any{"v": 1, "commit": pending, "invitation": nil}, map[string]any{"v": 1, "commit": pending, "invitation": "bad"}} {
				if code, _ := desk.call(t, h, "POST", publishPath, body); code != 400 {
					t.Fatalf("missing/unbound publication accepted: %d", code)
				}
			}
			wrongHead := c
			wrongHead.Head = firstRoster(desk, "wrong-head").Hash()
			if err := h.store.cancelGroupInvitation(phoneID.Public(phone.addr), wrongHead, realm); !errors.Is(err, errGroupStale) {
				t.Fatalf("wrong current head: %v", err)
			}
			wrongRoot := c
			wrongRoot.Bootstrap = phoneID.Public(phone.addr).Fingerprint()
			if err := h.store.cancelGroupInvitation(phoneID.Public(phone.addr), wrongRoot, realm); !errors.Is(err, errNotFound) {
				t.Fatalf("wrong root: %v", err)
			}
			oldPath := "/v1/groups/" + conv + "/chain?creator=" + bootstrap
			// Old and newly forged version headers cannot make this legacy append work.
			if code, _ := desk.call(t, h, "POST", oldPath, pending); code != http.StatusUpgradeRequired {
				t.Fatalf("legacy append %d", code)
			}
			if cancelFirst {
				if err := h.store.cancelGroupInvitation(phoneID.Public(phone.addr), c, realm); err != nil {
					t.Fatal(err)
				}
				if _, err := h.store.putGroupCommit(desk.id.Public(desk.addr), pending, realm, id); !errors.Is(err, errGroupInvitationCancelled) {
					t.Fatalf("offline original published cancelled invitation: %v", err)
				}
				if code, _ := desk.call(t, h, "POST", oldPath, pending); code != http.StatusUpgradeRequired {
					t.Fatalf("legacy bypass %d", code)
				}
				head, _, _ := groupHeadIn(h.store.db, conv, bootstrap)
				if head.hash != root.Hash || head.seq != 0 {
					t.Fatal("cancel changed shared head")
				}
				// A different invitation on the very same head remains publishable.
				otherID := firstRoster(desk, "unrelated").Hash()
				other := commit(1, root.Hash)
				if _, err := h.store.putGroupCommit(desk.id.Public(desk.addr), other, realm, otherID); err != nil {
					t.Fatalf("unrelated invitation invalidated: %v", err)
				}
			} else {
				if _, err := h.store.putGroupCommit(desk.id.Public(desk.addr), pending, realm, id); err != nil {
					t.Fatal(err)
				}
				if err := h.store.cancelGroupInvitation(phoneID.Public(phone.addr), c, realm); !errors.Is(err, errGroupInvitationPublished) {
					t.Fatalf("published invitation cancelled: %v", err)
				}
				if code, _ := desk.call(t, h, "POST", oldPath, pending); code != 200 {
					t.Fatalf("exact old receipt %d", code)
				}
				page, err := h.store.groupChain(desk.addr, conv, bootstrap, 0)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := json.Marshal(page.Records[0])
				if !bytes.Equal(original, got) {
					t.Fatal("publication re-signed or re-encrypted")
				}
			}
			// Actual concurrent calls must produce exactly one immutable winner.
			for i := 0; i < 8; i++ {
				fresh := commit(0, "")
				fresh.Conv = firstRoster(desk, protocol.NewID()).Hash()
				fresh.Sign(desk.id.Sign)
				if _, e := h.store.putGroupCommit(desk.id.Public(desk.addr), fresh, realm, ""); e != nil {
					t.Fatal(e)
				}
				attempt := commit(1, fresh.Hash)
				attempt.Conv = fresh.Conv
				attempt.Sign(desk.id.Sign)
				race := c
				race.Conv = fresh.Conv
				race.Prev = fresh.Hash
				race.Head = fresh.Hash
				start := make(chan struct{})
				var wg sync.WaitGroup
				var cancelErr, publishErr error
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					cancelErr = h.store.cancelGroupInvitation(phoneID.Public(phone.addr), race, realm)
				}()
				go func() {
					defer wg.Done()
					<-start
					_, publishErr = h.store.putGroupCommit(desk.id.Public(desk.addr), attempt, realm, id)
				}()
				close(start)
				wg.Wait()
				if !((cancelErr == nil && errors.Is(publishErr, errGroupInvitationCancelled)) || (publishErr == nil && errors.Is(cancelErr, errGroupInvitationPublished))) {
					t.Fatalf("nonexclusive outcome: cancel=%v publish=%v", cancelErr, publishErr)
				}
			}

			reopened, err := openStore(filepath.Join(h.cfg.DataDir, "hub.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.db.Close()
			err = reopened.cancelGroupInvitation(phoneID.Public(phone.addr), c, realm)
			if cancelFirst && err != nil || !cancelFirst && !errors.Is(err, errGroupInvitationPublished) {
				t.Fatalf("restart/replay outcome %v", err)
			}
			wrong := c
			wrong.Person = protocol.NewID()
			if err = reopened.cancelGroupInvitation(phoneID.Public(phone.addr), wrong, realm); !errors.Is(err, errGroupForbidden) {
				t.Fatalf("other person %v", err)
			}
			if _, err = h.store.db.Exec(`UPDATE agents SET revoked_at=1 WHERE address=?`, phone.addr); err != nil {
				t.Fatal(err)
			}
			if err = h.store.cancelGroupInvitation(phoneID.Public(phone.addr), c, realm); !errors.Is(err, errGroupForbidden) {
				t.Fatalf("revoked replay %v", err)
			}
		})
	}
}
