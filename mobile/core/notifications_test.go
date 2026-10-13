package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misunders2d/agentnet/internal/client"
	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/testhub"
)

type recordedNativeNotice struct{ workspace, fragment, title, body string }
type mobileNotices struct {
	mu        sync.Mutex
	notices   []recordedNativeNotice
	dismissed []string
	arrived   chan recordedNativeNotice
}

func (n *mobileNotices) Notify(workspace, fragment, title, body string) {
	x := recordedNativeNotice{workspace, fragment, title, body}
	n.mu.Lock()
	n.notices = append(n.notices, x)
	n.mu.Unlock()
	n.arrived <- x
}
func (n *mobileNotices) Dismiss(workspace, fragment string) {
	n.mu.Lock()
	n.dismissed = append(n.dismissed, workspace+":"+fragment)
	n.mu.Unlock()
}

// Real signed/encrypted sender -> synthetic relay -> phone App.Run -> Notifier.
// No reflection/direct invocation of notification producers or model execution.
func TestPhoneRealReceiveNotificationPreviewReviewAndReminder(t *testing.T) {
	t.Setenv("AGENTNET_NOTIFY", "off")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	testhub.Start(t, dir, "127.0.0.1:0", "")
	sender, err := client.Join(ctx, t.TempDir(), testhub.BootstrapCode(t, dir), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if _, err = sender.CreatePerson(ctx, "Fixture sender"); err != nil {
		t.Fatal(err)
	}
	invite, err := sender.Invite(ctx, "phone", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	phone, err := client.Join(ctx, home, invite, "phone")
	if err != nil {
		t.Fatal(err)
	}
	address := phone.Address
	if _, err = phone.CreatePerson(ctx, "Fixture phone"); err != nil {
		t.Fatal(err)
	}
	phone.Close()
	runCtx, stopSender := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = sender.Run(runCtx, client.RunOptions{HumanOnly: true}) }()
	defer func() { stopSender(); <-done }()
	app, err := OpenApp(home, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	notices := &mobileNotices{arrived: make(chan recordedNativeNotice, 16)}
	app.SetNotifier(notices)
	if err = app.session.a.SetAlertPrefs(client.AlertPrefs{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	waitMobile(t, "phone/sender push streams", func() bool { return app.session.a.MemberView().Current && sender.MemberView().Current })
	take := func(label string) recordedNativeNotice {
		t.Helper()
		select {
		case n := <-notices.arrived:
			return n
		case <-time.After(10 * time.Second):
			t.Fatalf("missing %s notification", label)
			return recordedNativeNotice{}
		}
	}
	if _, err = app.session.a.CreateDM(ctx, sender.Address); err != nil {
		t.Fatal(err)
	}
	sent, err := sender.SendMessage(ctx, client.Outgoing{To: address, Kind: envelope.KindMessage, Body: "Locally decrypted real receive preview"})
	if err != nil {
		t.Fatal(err)
	}
	n := take("message")
	if n.workspace != client.DefaultWorkspace || n.fragment != "msg="+sent.ID+"&dir=in" || n.title != "Fixture sender" || n.body != "Locally decrypted real receive preview" {
		t.Fatalf("message notice: %+v", n)
	}
	provider := app.phone(client.DefaultWorkspace, app.session)
	if err = provider.NotifySeen(sent.ID, []string{sent.ID}); err != nil {
		t.Fatal(err)
	}
	notices.mu.Lock()
	dismissed := append([]string(nil), notices.dismissed...)
	notices.mu.Unlock()
	if len(dismissed) != 1 || dismissed[0] != "default:msg="+sent.ID+"&dir=in" {
		t.Fatalf("exact read dismissal: %v", dismissed)
	}
	task, err := sender.SendMessage(ctx, client.Outgoing{To: address, Kind: envelope.KindTask, Body: "Exact request needs the phone owner's decision"})
	if err != nil {
		t.Fatal(err)
	}
	n = take("review")
	if n.fragment != "msg="+task.ID+"&dir=in" || !strings.Contains(n.title, "Decision needed") || !strings.Contains(n.body, "Exact request") {
		t.Fatalf("review notice: %+v", n)
	}
	if _, err = app.session.a.SetReminder(sent.ID, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n = take("reminder")
	if n.fragment != "msg="+sent.ID+"&dir=in" || !strings.Contains(n.title, "Reminder") || n.body != "Locally decrypted real receive preview" {
		t.Fatalf("reminder notice: %+v", n)
	}
	app.Stop()
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	waitMobile(t, "restarted phone stream", func() bool { return app.session.a.MemberView().Current })
	notices.mu.Lock()
	count := len(notices.notices)
	notices.mu.Unlock()
	if count != 3 {
		t.Fatalf("already notified work repeated: %d", count)
	}
	t.Log("Actual isolated core receive -> Notifier with bounded local preview, exact review/reminder/read destinations. Android OS/physical behavior remains separate.")
}
