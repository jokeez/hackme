// Command show_node_addr prints HMC address from dataDir/node_ed25519.seed (read-only).
// With -long, also prints the Economics-v2 long address (HMC- + 64 hex).
package main

import (
	"fmt"
	"os"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
)

func main() {
	long := false
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "-long" {
		long = true
		args = args[1:]
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: show_node_addr [-long] DATA_DIR")
		os.Exit(2)
	}
	s, err := nodecrypto.LoadOrCreate(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(s.Address())
	if long {
		la, err := chain.LongAddressFromPubKeyHex(s.PublicKeyHex())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(la)
	}
}
