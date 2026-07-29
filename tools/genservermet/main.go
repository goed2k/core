package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goed2k/core/protocol/server"
)

func main() {
	met := server.NewServerMet()
	entry, err := server.NewServerMetEntryFromIP("91.200.42.47", 3883, "goed2k test server", "fixture")
	if err != nil {
		fmt.Fprintf(os.Stderr, "entry: %v\n", err)
		os.Exit(1)
	}
	met.AddServer(entry)
	var buf bytes.Buffer
	if err := met.Put(&buf); err != nil {
		fmt.Fprintf(os.Stderr, "put: %v\n", err)
		os.Exit(1)
	}
	out := filepath.Join("testdata", "server.met")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote", out)
}
