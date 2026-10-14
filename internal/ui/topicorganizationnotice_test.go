package ui

import (
	"testing"

	"github.com/misunders2d/agentnet/internal/client"
)

func TestLiveTopicOrganizationNoticeDestination(t *testing.T) {
	a, b, _ := liveWorld(t)
	if _, err := a.CreatePerson(t.Context(), "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CreatePerson(t.Context(), "Bob"); err != nil {
		t.Fatal(err)
	}
	l := NewLive(a)
	conv, err := l.NewDM(b.Address)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.SendConv(t.Context(), conv, client.ConvOutgoing{Body: "source"})
	if err != nil {
		t.Fatal(err)
	}
	dest, err := a.SendConv(t.Context(), conv, client.ConvOutgoing{Body: "destination", Topic: "new"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := l.DM(conv)
	if err != nil {
		t.Fatal(err)
	}
	topic := ""
	for _, m := range view.Messages {
		if m.LID == dest.LID {
			topic = m.Topic
		}
	}
	if topic == "" {
		t.Fatal("destination topic absent")
	}
	if _, err = a.ChangeChatTopic(t.Context(), conv, topic, "rename", "Reviewed destination", 0); err != nil {
		t.Fatal(err)
	}
	for _, main := range []bool{false, true} {
		request := client.TopicOrganizationRequest{Conv: conv, IDs: []string{first.LID}, Topic: topic}
		want := "Moved selected messages to “Reviewed destination”."
		if main {
			request.Topic = ""
			request.Main = true
			want = "Moved selected messages to Main flow."
		}
		review, e := a.PreviewTopicOrganization(request)
		if e != nil {
			t.Fatal(e)
		}
		moved, e := a.ApplyTopicOrganization(t.Context(), review)
		if e != nil {
			t.Fatal(e)
		}
		view, e = l.DM(conv)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, m := range view.Messages {
			if m.LID == moved.LID {
				found = true
				if m.Body != want {
					t.Fatalf("move notice=%q want %q", m.Body, want)
				}
			}
		}
		if !found {
			t.Fatal("move notice absent")
		}
		overview, e := l.Overview()
		if e != nil {
			t.Fatal(e)
		}
		for _, d := range overview.DMs {
			if d.ID == conv && d.Last != want {
				t.Fatalf("preview=%q want %q", d.Last, want)
			}
		}
		if !main {
			if _, e = a.ChangeChatTopic(t.Context(), conv, topic, "rename", "Renamed destination", 0); e != nil {
				t.Fatal(e)
			}
			view, e = l.DM(conv)
			if e != nil {
				t.Fatal(e)
			}
			for _, m := range view.Messages {
				if m.LID == moved.LID && m.Body != "Moved selected messages to “Renamed destination”." {
					t.Fatalf("renamed notice=%q", m.Body)
				}
			}
		}
	}
}
