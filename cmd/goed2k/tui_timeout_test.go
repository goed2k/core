package main

import (
	"testing"
	"time"

	ed2k "github.com/goed2k/core"
	"github.com/goed2k/core/protocol"
)

func TestAllTransfersFinished(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	done := []ed2k.TransferSnapshot{
		{Hash: hash, Status: ed2k.TransferStatus{State: ed2k.Finished}},
	}
	if !allTransfersFinished(done) {
		t.Fatal("expected finished")
	}
	pending := []ed2k.TransferSnapshot{
		{Hash: hash, Status: ed2k.TransferStatus{State: ed2k.Downloading}},
	}
	if allTransfersFinished(pending) {
		t.Fatal("expected not finished")
	}
	if allTransfersFinished(nil) {
		t.Fatal("empty transfers should not count as finished")
	}
}

func TestShouldAutoExitOnTimeout(t *testing.T) {
	deadline := time.Now().Add(-time.Minute)
	exit, msg := shouldAutoExit(deadline, nil, []ed2k.TransferSnapshot{
		{Status: ed2k.TransferStatus{State: ed2k.Downloading}},
	}, time.Now())
	if !exit || msg != "stopped before completion" {
		t.Fatalf("got exit=%v msg=%q", exit, msg)
	}
}

func TestShouldAutoExitOnAllCompleteWithLink(t *testing.T) {
	exit, msg := shouldAutoExit(time.Time{}, []string{"/tmp/a.bin"}, []ed2k.TransferSnapshot{
		{FilePath: "/tmp/a.bin", Status: ed2k.TransferStatus{State: ed2k.Finished}},
	}, time.Now())
	if !exit || msg != "completed: /tmp/a.bin" {
		t.Fatalf("got exit=%v msg=%q", exit, msg)
	}
}

func TestShouldNotAutoExitInteractiveIdle(t *testing.T) {
	exit, _ := shouldAutoExit(time.Time{}, nil, nil, time.Now())
	if exit {
		t.Fatal("interactive idle session should keep running")
	}
}
