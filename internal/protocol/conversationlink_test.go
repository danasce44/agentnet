package protocol

import (
	"strings"
	"testing"
)

func TestConversationLink(t *testing.T) {
	r, c, topic := strings.Repeat("a", 32), strings.Repeat("b", 64), strings.Repeat("c", 32)
	for _, target := range []string{"main", topic} {
		link, err := ConversationLink(r, c, target)
		if err != nil || link != "agentnet://open#realm="+r+"&conv="+c+"&topic="+target {
			t.Fatalf("%q: %q %v", target, link, err)
		}
	}
	for _, bad := range []string{"", "../main", "MAIN", topic + "&token=secret"} {
		if _, err := ConversationLink(r, c, bad); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
}
