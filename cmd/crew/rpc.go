package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/rpcpipe"
)

func runRPC(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: crew rpc")
		return 2
	}
	home, err := rpc.Home()
	if err != nil {
		fmt.Fprintf(stderr, "crew rpc: %v\n", err)
		return 1
	}
	err = rpcpipe.Run(rpc.SocketPath(home), stdin, stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, rpcpipe.ErrUnavailable):
		fmt.Fprintln(stderr, "crew rpc: no daemon is running")
		return 1
	default:
		fmt.Fprintf(stderr, "crew rpc: %v\n", err)
		return 1
	}
}
