package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const browserUsage = "usage: crew browser [--session id] [--json] [--out file.png] [--timeout 30s] <" +
	"goto url | snapshot | click @eN | fill @eN text | type text | press key | wait @eN|ms|text | screenshot | eval js | tabs | cookies [domain] | set-device desktop|ipad|iphone>"

type browserShot struct {
	PNG    string `json:"png"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func runBrowser(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("browser", flag.ContinueOnError)
	fs.SetOutput(stderr)
	session := fs.String("session", sessionFromEnv(), "session id (default $CREW_SESSION, else the session on $TMUX_PANE)")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	out := fs.String("out", "", "file for the screenshot PNG (default a temp file)")
	timeout := fs.Duration("timeout", 30*time.Second, "how long to wait for the macOS app")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, browserUsage)
		return 2
	}
	call, err := domain.ParseBrowserCommand(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "crew browser: %v\n%s\n", err, browserUsage)
		return 2
	}
	p := rpc.BrowserCommandParams{Session: *session, Command: call.Command, Args: call.Args, TimeoutMS: int(timeout.Milliseconds())}
	if p.Session == "" {
		p.Pane = os.Getenv("TMUX_PANE")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+5*time.Second)
	defer cancel()
	c, err := connectHome(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "crew browser: %v\n", err)
		return 1
	}
	defer func() { _ = c.Close() }()
	var res json.RawMessage
	if err := c.Call(ctx, rpc.MethodBrowserCommand, p, &res); err != nil {
		var rerr *rpc.Error
		if errors.As(err, &rerr) {
			fmt.Fprintf(stderr, "crew browser: %s\n", rerr.Message)
		} else {
			fmt.Fprintf(stderr, "crew browser: %v\n", err)
		}
		return 1
	}
	if call.Command == "screenshot" {
		res, err = saveShot(res, *out, *asJSON)
		if err != nil {
			fmt.Fprintf(stderr, "crew browser: %v\n", err)
			return 1
		}
	}
	printBrowserResult(stdout, res, *asJSON)
	return 0
}

func saveShot(res json.RawMessage, out string, asJSON bool) (json.RawMessage, error) {
	var shot browserShot
	if err := json.Unmarshal(res, &shot); err != nil || shot.PNG == "" {
		return nil, errors.New("the macOS app sent no PNG")
	}
	data, err := base64.StdEncoding.DecodeString(shot.PNG)
	if err != nil {
		return nil, fmt.Errorf("the PNG is not base64: %w", err)
	}
	if out == "" {
		out = filepath.Join(os.TempDir(), "crew-browser-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".png")
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return nil, err
	}
	if !asJSON {
		b, _ := json.Marshal(out)
		return b, nil
	}
	return json.Marshal(struct {
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}{out, shot.Width, shot.Height})
}

func printBrowserResult(w io.Writer, res json.RawMessage, asJSON bool) {
	if asJSON {
		var buf bytes.Buffer
		if json.Indent(&buf, res, "", "  ") == nil {
			fmt.Fprintln(w, buf.String())
			return
		}
		fmt.Fprintln(w, string(res))
		return
	}
	var text string
	if json.Unmarshal(res, &text) == nil {
		fmt.Fprintln(w, text)
		return
	}
	var obj struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(res, &obj) == nil && obj.Text != nil {
		fmt.Fprint(w, *obj.Text)
		if !strings.HasSuffix(*obj.Text, "\n") {
			fmt.Fprintln(w)
		}
		return
	}
	printBrowserResult(w, res, true)
}
