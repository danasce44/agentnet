package ui

import (
	"os/exec"
	"strings"
	"testing"
)

// The skin gallery reads only its fixed sources, verifies every byte and the
// package digest before storing, refuses packages that need a newer AgentNet
// and fails closed on bad catalogs (static/skin-gallery.mjs).
func TestSkinGallery(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "testdata/skin_gallery_check.mjs").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "skin gallery check PASS") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The gallery end to end in Chromium on the production host and Comic, with
// GitHub faked: verified previews, the version gate, a tampered package
// refused, install → Use → trust → mounted, and the installed mark.
func TestSkinGalleryRendered(t *testing.T) {
	out := browserCheck(t, "testdata/skin_gallery_rendered.cjs", demoPage(t, ""))
	if !strings.Contains(out, "skin gallery rendered PASS") {
		t.Fatalf("no pass line:\n%s", out)
	}
	t.Logf("%s", out)
}
