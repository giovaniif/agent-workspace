package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/term"
	"rsc.io/qr"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const remoteUsage = "usage: agentws remote pair [--name n] [--url u] | devices | revoke <id>"

const qrQuietZone = 4

var errRemoteUsage = errors.New("usage")

func runRemote(args []string, stdout, stderr io.Writer) int {
	err := remoteCommand(args, stdout, stderr)
	switch {
	case errors.Is(err, errRemoteUsage):
		fmt.Fprintln(stderr, remoteUsage)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "agentws remote: %v\n", err)
		return 1
	}
	return 0
}

func remoteCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errRemoteUsage
	}
	switch args[0] {
	case "pair":
		return remotePair(args[1:], stdout, stderr)
	case "devices":
		if len(args) != 1 {
			return errRemoteUsage
		}
		return remoteDevices(stdout)
	case "revoke":
		if len(args) != 2 {
			return errRemoteUsage
		}
		return remoteRevoke(args[1], stdout)
	default:
		return errRemoteUsage
	}
}

func remotePair(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("remote pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "name for the device (default: the name the app sends)")
	url := fs.String("url", "", "public URL of agentws serve (default: [serve] url in config.toml)")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return errRemoteUsage
	}
	home, err := rpc.Home()
	if err != nil {
		return err
	}
	base := *url
	if base == "" {
		if base, err = loadServeURL(filepath.Join(home, "config.toml")); err != nil {
			return err
		}
	}
	if strings.TrimSpace(base) == "" {
		return fmt.Errorf("no public URL for agentws serve: pass --url https://<host> or add\n\n  [serve]\n  url = \"https://<host>\"\n\nto %s", filepath.Join(home, "config.toml"))
	}
	if _, err := domain.PairURL(base, ""); err != nil {
		return err
	}
	ctx := context.Background()
	c, err := connect(ctx, home)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var code rpc.PairCode
	if err := c.Call(ctx, rpc.MethodPairCode, rpc.PairCodeParams{Name: *name}, &code); err != nil {
		return err
	}
	link, err := domain.PairURL(base, code.Code)
	if err != nil {
		return err
	}
	if err := writeQR(stdout, stderr, link, qrTargetFor(stdout, os.Getenv)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nScan the code with your phone, or open:\n\n  %s\n\nCode %s, single use, expires at %s.\n", link, code.Code, code.ExpiresAt.Local().Format(time.Kitchen))
	return nil
}

func remoteDevices(stdout io.Writer) error {
	ctx := context.Background()
	c, err := connectHome(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var list rpc.DeviceList
	if err := c.Call(ctx, rpc.MethodDeviceList, nil, &list); err != nil {
		return err
	}
	printDevices(stdout, list.Devices)
	return nil
}

func printDevices(w io.Writer, devices []rpc.Device) {
	if len(devices) == 0 {
		fmt.Fprintln(w, "no paired devices")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPAIRED\tLAST SEEN")
	for _, d := range devices {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.ID, d.Name, d.CreatedAt.Local().Format(time.DateTime), d.LastSeen.Local().Format(time.DateTime))
	}
	_ = tw.Flush()
}

func remoteRevoke(id string, stdout io.Writer) error {
	ctx := context.Background()
	c, err := connectHome(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Call(ctx, rpc.MethodDeviceRevoke, rpc.DeviceRevokeParams{ID: id}, nil); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "revoked %s\n", id)
	return nil
}

func loadServeURL(path string) (string, error) {
	var cfg struct {
		Serve struct {
			URL string `toml:"url"`
		} `toml:"serve"`
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return cfg.Serve.URL, nil
}

type qrTarget struct {
	Colour  bool
	Columns int
}

func qrTargetFor(w io.Writer, getenv func(string) string) qrTarget {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return qrTarget{}
	}
	columns, _, err := term.GetSize(f.Fd())
	if err != nil || columns <= 0 {
		columns = columnsFrom(getenv)
	}
	return qrTarget{Colour: getenv("NO_COLOR") == "", Columns: columns}
}

func columnsFrom(getenv func(string) string) int {
	n, err := strconv.Atoi(getenv("COLUMNS"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func qrWidth(text string) (int, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0, err
	}
	return code.Size + 2*qrQuietZone, nil
}

func writeQR(stdout, stderr io.Writer, text string, t qrTarget) error {
	width, err := qrWidth(text)
	if err != nil {
		return err
	}
	if t.Columns > 0 && t.Columns < width {
		fmt.Fprintf(stderr, "The terminal is %d columns wide and the QR code needs %d, so it is not drawn. Widen the window and run the command again, or use the link below.\n", t.Columns, width)
		return nil
	}
	img, err := renderQR(text, t.Colour)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, img)
	return nil
}

func renderQR(text string, colour bool) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	if colour {
		return colourBlocks(code.Size, code.Black, qrQuietZone), nil
	}
	return halfBlocks(code.Size, code.Black, qrQuietZone), nil
}

func colourBlocks(size int, black func(x, y int) bool, quiet int) string {
	total := size + 2*quiet
	dark := func(x, y int) bool {
		cx, cy := x-quiet, y-quiet
		if cx < 0 || cy < 0 || cx >= size || cy >= size {
			return false
		}
		return black(cx, cy)
	}
	colour := func(isDark bool) string {
		if isDark {
			return "16"
		}
		return "231"
	}
	var b strings.Builder
	for y := 0; y < total; y += 2 {
		prev := ""
		for x := range total {
			seq := "\x1b[38;5;" + colour(dark(x, y)) + ";48;5;" + colour(dark(x, y+1)) + "m"
			if seq != prev {
				b.WriteString(seq)
				prev = seq
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}

func halfBlocks(size int, black func(x, y int) bool, quiet int) string {
	total := size + 2*quiet
	light := func(x, y int) bool {
		if y >= total {
			return false
		}
		cx, cy := x-quiet, y-quiet
		if cx < 0 || cy < 0 || cx >= size || cy >= size {
			return true
		}
		return !black(cx, cy)
	}
	var b strings.Builder
	for y := 0; y < total; y += 2 {
		for x := range total {
			top, bottom := light(x, y), light(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
