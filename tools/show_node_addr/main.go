// Command show_node_addr prints HMC address from dataDir/node_ed25519.seed (read-only).
package main

import (
	"fmt"
	"os"

	"hackme/internal/nodecrypto"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: show_node_addr DATA_DIR")
		os.Exit(2)
	}
	s, err := nodecrypto.LoadOrCreate(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(s.Address())
}
