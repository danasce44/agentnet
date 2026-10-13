package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/misunders2d/agentnet/internal/client"
)

func runRoom(ctx context.Context, a *client.Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: room ask --pid PID [--kind question|task] TEXT | room wait REQUEST_ID")
	}
	if args[0] == "send" {
		if err := receiverGuard(a, "room send", "a proactive message", ""); err != nil {
			return err
		}
		fs := flag.NewFlagSet("room send", flag.ContinueOnError)
		pid := fs.String("pid", "", "this device's active agent participation")
		topic := fs.String("topic", "", "existing topic ID; defaults to the invited scope or Main")
		var files []client.OutgoingFile
		fs.Func("file", "attach a file (repeatable)", func(path string) error { files = append(files, client.OutgoingFile{Path: path}); return nil })
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *pid == "" || len(fs.Args()) == 0 && len(files) == 0 {
			return errors.New("usage: room send --pid PID [--topic ID] [--file PATH]... [TEXT]")
		}
		sent, err := a.SendAgentMessage(ctx, *pid, *topic, strings.Join(fs.Args(), " "), files...)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, sent.ID, sent.State)
		if sent.Detail != "" {
			fmt.Fprintln(out, sent.Detail)
		}
		return nil
	}
	cause := os.Getenv(client.RoomRequestEnv)
	if cause == "" {
		return errors.New("room asks and waits belong to a running group agent request")
	}
	ref := ""
	switch args[0] {
	case "ask":
		fs := flag.NewFlagSet("room ask", flag.ContinueOnError)
		pid := fs.String("pid", "", "other group agent participation")
		kind := fs.String("kind", "question", "question or task")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *pid == "" || len(fs.Args()) == 0 {
			return errors.New("choose --pid and write a question or task")
		}
		sent, err := a.SendRoomAsk(ctx, cause, *pid, *kind, strings.Join(fs.Args(), " "))
		if err != nil {
			return err
		}
		ref = sent.LID
		fmt.Fprintln(out, "request", ref, "stored; waiting for its correlated reply")
	case "wait":
		if len(args) != 2 {
			return errors.New("usage: room wait REQUEST_ID")
		}
		ref = args[1]
	default:
		return errors.New("usage: room ask | room wait")
	}
	// Existing local daemon wakes only; no timer or extra Hub stream.
	reply, err := a.AwaitRoomReply(ctx, cause, ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "reply %s from agent participation %s\n%s\n", reply.ReplyTo, reply.PID, reply.Body)
	return nil
}
