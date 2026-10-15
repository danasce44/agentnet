// Disposable company fixture only. Uses one production client; never edits SQL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/misunders2d/agentnet/internal/client"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func read(p string, v any) { raw, err := os.ReadFile(p); must(err); must(json.Unmarshal(raw, v)) }
func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func main() {
	world := flag.String("world", "", "explicit private synthetic company world")
	count := flag.Int("count", 200, "history rows per kind, 1..200")
	flag.Parse()
	require(flag.NArg() == 0 && filepath.IsAbs(*world) && *count >= 1 && *count <= 200, "explicit absolute --world and --count 1..200 required")
	st, err := os.Stat(*world)
	must(err)
	require(st.IsDir() && st.Mode().Perm()&0077 == 0, "world must be a private directory")
	marker := filepath.Join(*world, "skin-history-fixture")
	st, err = os.Lstat(marker)
	must(err)
	require(st.Mode().IsRegular(), "regular synthetic marker required")
	raw, err := os.ReadFile(marker)
	must(err)
	require(string(raw) == "agentnet synthetic company history fixture\n", "wrong synthetic fixture marker")
	var urls map[string]struct {
		Address string `json:"address"`
	}
	var seed struct {
		DM string `json:"dm_anna"`
	}
	read(filepath.Join(*world, "urls.json"), &urls)
	read(filepath.Join(*world, "seed.json"), &seed)
	from, to := urls["sergey"].Address, urls["anna"].Address
	require(from == "admin/laptop" && to == "anna/desk", "only exact synthetic company addresses allowed")
	require(regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(seed.DM), "invalid fixture DM")
	metaPath := filepath.Join(*world, "zoom-touch-history.json")
	_, err = os.Lstat(metaPath)
	require(os.IsNotExist(err), "history metadata already exists or is inaccessible; refuse resend")
	started, err := os.OpenFile(marker+".started", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	must(err)
	must(started.Close())
	a, err := client.Open(filepath.Join(*world, "sergey"))
	must(err)
	defer a.Close()
	require(a.Address == from, "opened identity does not match synthetic author")
	conversations, err := a.Conversations()
	must(err)
	found := false
	for _, c := range conversations {
		if c.ID == seed.DM {
			found = c.Peer.Address == to && c.Kind == "dm" && !c.Deleted && c.Frozen == ""
		}
	}
	require(found, "exact current Anna DM not found")
	ctx := context.Background()
	for i := 1; i <= *count; i++ {
		sent, err := a.SendConv(ctx, seed.DM, client.ConvOutgoing{Kind: "message", Body: fmt.Sprintf("Long history %03d", i)})
		must(err)
		require(sent.ID != "" && sent.LID != "", "missing signed DM IDs")
	}
	root, last := "", ""
	for i := 1; i <= *count; i++ {
		if last != "" {
			must(a.CheckReplyTo(last, to))
		}
		sent, err := a.SendMessage(ctx, client.Outgoing{To: to, Body: fmt.Sprintf("Device history %03d", i), ReplyTo: last, Wait: 5 * time.Second})
		must(err)
		require(regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(sent.ID), "invalid signed device ID")
		if root == "" {
			root = sent.ID
		}
		last = sent.ID
	}
	meta, err := json.Marshal(struct {
		Conv   string `json:"conv"`
		To     string `json:"to"`
		From   string `json:"from"`
		Thread string `json:"thread"`
	}{seed.DM, to, from, root})
	must(err)
	output, err := os.OpenFile(metaPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	must(err)
	_, err = output.Write(meta)
	must(err)
	must(output.Close())
	fmt.Printf("history seed complete: one Open, %d DM + %d chained device sends\n", *count, *count)
}
