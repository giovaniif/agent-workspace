package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/serve"
	"github.com/giovaniif/agent-workspace/internal/version"
)

const serveUsage = "usage: agentws serve [--addr host:port] [--cert file --key file | --self-signed] [--url https://host]"

var errServeUsage = errors.New("usage")

type serveFlags struct {
	opts serve.Options
	url  string
}

func runServe(args []string, stdout, stderr io.Writer) int {
	home, err := rpc.Home()
	if err != nil {
		fmt.Fprintf(stderr, "agentws serve: %v\n", err)
		return 1
	}
	f, err := parseServe(args, home, stderr)
	switch {
	case errors.Is(err, errServeUsage):
		fmt.Fprintln(stderr, serveUsage)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "agentws serve: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serveIn(ctx, home, f, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "agentws serve: %v\n", err)
		return 1
	}
	return 0
}

func parseServe(args []string, home string, stderr io.Writer) (serveFlags, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f serveFlags
	fs.StringVar(&f.opts.Addr, "addr", serve.DefaultAddr, "address to listen on; plain HTTP only on loopback")
	fs.StringVar(&f.opts.Cert, "cert", "", "TLS certificate file")
	fs.StringVar(&f.opts.Key, "key", "", "TLS key file")
	fs.BoolVar(&f.opts.SelfSigned, "self-signed", false, "serve TLS with a self-signed certificate kept in $AGENTWS_HOME/serve")
	fs.StringVar(&f.url, "url", "", "public URL the app is opened from (default: [serve] url in config.toml)")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return serveFlags{}, errServeUsage
	}
	f.opts.CertDir = filepath.Join(home, "serve")
	if err := f.opts.Check(); err != nil {
		return serveFlags{}, err
	}
	if strings.TrimSpace(f.url) == "" {
		u, err := loadServeURL(filepath.Join(home, "config.toml"))
		if err != nil {
			return serveFlags{}, err
		}
		f.url = u
	}
	return f, nil
}

func serveIn(ctx context.Context, home string, f serveFlags, stdout, stderr io.Writer) error {
	srv, err := serve.New(serve.Config{
		Build: version.String(),
		URL:   f.url,
		Log:   stdout,
		Dial: func(ctx context.Context) (serve.Daemon, error) {
			return connect(ctx, home)
		},
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(f.url) == "" {
		fmt.Fprintf(stderr, "agentws serve: no public URL, so the stream refuses every client; set [serve] url in %s or pass --url\n", filepath.Join(home, "config.toml"))
	}
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("subscribe to the daemon: %w", err)
	}
	return serve.Listen(ctx, f.opts, serveHandler(srv), func(url string) {
		fmt.Fprintf(stdout, "agentws serve: listening on %s\n", url)
		if f.opts.SelfSigned {
			fmt.Fprintf(stdout, "agentws serve: self-signed certificate %s; set your proxy to trust it\n", filepath.Join(f.opts.CertDir, "cert.pem"))
		}
	})
}

func serveHandler(srv *serve.Server) http.Handler {
	return srv.Handler(serve.Static(serve.Dist()))
}
