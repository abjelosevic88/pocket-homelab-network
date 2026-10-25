// Command pocket-network-agent is the agent alone, without the server and
// its database: small enough for a router (OpenWrt, GL.iNet; MIPS and ARM).
// It sweeps this machine's network and reports to a Pocket Network.
//
//	pocket-network-agent -server https://network.example.com -token pna_…
package main

import (
	"fmt"
	"os"

	"github.com/abjelosevic88/pocket-homelab-network/internal/agent"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println("pocket-network-agent", version)
			return
		case "scan":
			agent.Scan(os.Args[2:])
			return
		}
	}
	agent.Run(os.Args[1:], version)
}
