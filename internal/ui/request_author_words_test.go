package ui

import (
	"os/exec"
	"testing"
)

func TestRequestAuthorWordsKeepVerifiedAgent(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	if out, err := exec.CommandContext(t.Context(), node, "testdata/request_author_words_check.mjs").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
