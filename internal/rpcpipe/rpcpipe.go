package rpcpipe

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"

	"github.com/giovaniif/agent-workspace/internal/rpc"
)

var ErrUnavailable = errors.New("rpcpipe: no daemon is listening")

func Run(socket string, in io.Reader, out io.Writer) error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		line, _ := json.Marshal(rpc.Response{V: rpc.Version, Error: &rpc.Error{Code: rpc.CodeUnavailable, Message: "no agentws daemon is running: " + err.Error()}})
		_, _ = out.Write(append(line, '\n'))
		return ErrUnavailable
	}
	defer func() { _ = conn.Close() }()
	upErr := make(chan error, 1)
	go func() {
		err := copyLines(conn, in)
		upErr <- err
		if err != nil {
			_ = conn.Close()
			return
		}
		if uc, ok := conn.(*net.UnixConn); ok {
			_ = uc.CloseWrite()
		}
	}()
	downErr := copyLines(out, conn)
	select {
	case err := <-upErr:
		if err != nil {
			return err
		}
	default:
	}
	if errors.Is(downErr, net.ErrClosed) {
		return nil
	}
	return downErr
}

func copyLines(dst io.Writer, src io.Reader) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), rpc.MaxMessage)
	var line []byte
	for sc.Scan() {
		line = append(append(line[:0], sc.Bytes()...), '\n')
		if _, err := dst.Write(line); err != nil {
			return err
		}
	}
	return sc.Err()
}
