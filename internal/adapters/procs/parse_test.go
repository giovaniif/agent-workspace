package procs

import (
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

const netstatFixture = `Active Internet connections (including servers)
Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)          rxbytes      txbytes  rhiwat  shiwat          process:pid    state  options           gencnt    flags   flags1 usecnt rtncnt fltrs
tcp4       0      0  127.0.0.1.8081         *.*                    LISTEN                 0            0  131072  131072            node:101  00100 00000006 0000000002a72b1e 00000000 00080800      1      0 000000
tcp6       0      0  ::1.8081               *.*                    LISTEN                 0            0  131072  131072            node:101  00100 00000006 0000000002a72b1f 00000000 00080800      1      0 000000
tcp46      0      0  *.9229                 *.*                    LISTEN                 0            0  131072  131072            node:101  00100 00000006 0000000002a72b20 00000000 00080800      1      0 000000
tcp4       0      0  *.7000                 *.*                    LISTEN                 0            0  131072  131072  ControlCenter:202  00100 00000006 0000000002a72b21 00000000 00080800      1      0 000000
tcp6       0      0  fe80::1%lo0.3000       *.*                    LISTEN                 0            0  131072  131072            bun:303  00100 00000006 0000000002a72b22 00000000 00080800      1      0 000000
tcp4       0      0  127.0.0.1.8081         127.0.0.1.55000        ESTABLISHED         1024         2048  131072  131072            node:101  00100 00000006 0000000002a72b23 00000000 00080800      1      0 000000
tcp4       0      0  *.4000                 *.*                    CLOSED                 0            0  131072  131072            node:404  00100 00000006 0000000002a72b24 00000000 00080800      1      0 000000
`

const lsofFixture = "p101\ng100\ncnode server\nfcwd\nn/w/api-feat\n" +
	"p202\ng202\ncControlCenter\nfcwd\nn/\n"

func TestPortsParseNetstatAndLsof(t *testing.T) {
	got := merge(parseNetstat(netstatFixture), parseDetails(lsofFixture))

	want := []domain.Listener{
		{Port: 8081, PID: 101, PGID: 100, Command: "node server", Cwd: "/w/api-feat"},
		{Port: 8081, PID: 101, PGID: 100, Command: "node server", Cwd: "/w/api-feat"},
		{Port: 9229, PID: 101, PGID: 100, Command: "node server", Cwd: "/w/api-feat"},
		{Port: 7000, PID: 202, PGID: 202, Command: "ControlCenter", Cwd: "/"},
		{Port: 3000, PID: 303, Command: "bun"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestPortsParseSkipsMalformedRows(t *testing.T) {
	out := "tcp4 0 0 *.abc *.* LISTEN 0 0 1 1 node:5\n" +
		"tcp4 0 0 *.80 *.* LISTEN 0 0 1 1 node:x\n" +
		"tcp4 0 0 *.81 *.* LISTEN 0 0 1 1 nopid\n" +
		"tcp4 0 0 *.82 *.* LISTEN\n" +
		"tcp4 0 0 *.83 *.* LISTEN 0 0 1 1 ok:7\n"
	got := parseNetstat(out)
	want := []domain.Listener{{Port: 83, PID: 7, Command: "ok"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPortsParseDetailsSkipsBadPid(t *testing.T) {
	got := parseDetails("pabc\ng9\ncbad\nn/x\np5\nnnot-a-group-line\ngzz\nc\n")
	want := map[int]details{5: {pgid: 5, cwd: "not-a-group-line"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPortsParseEmpty(t *testing.T) {
	if got := parseNetstat(""); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
	if got := parseDetails(""); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestPortsPIDListDeduplicatesAndSorts(t *testing.T) {
	got := pidList([]domain.Listener{{PID: 9}, {PID: 3}, {PID: 9}, {PID: 5}})
	if want := "3,5,9"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
