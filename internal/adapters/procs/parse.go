package procs

import (
	"sort"
	"strconv"
	"strings"

	"github.com/giovaniif/crew/internal/domain"
)

func parseNetstat(out string) []domain.Listener {
	var listeners []domain.Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 11 || f[5] != "LISTEN" {
			continue
		}
		port, ok := portAfterLastDot(f[3])
		i := strings.LastIndex(f[10], ":")
		if !ok || i < 0 {
			continue
		}
		pid, err := strconv.Atoi(f[10][i+1:])
		if err != nil {
			continue
		}
		listeners = append(listeners, domain.Listener{Port: port, PID: pid, Command: f[10][:i]})
	}
	return listeners
}

func portAfterLastDot(addr string) (int, bool) {
	i := strings.LastIndex(addr, ".")
	if i < 0 {
		return 0, false
	}
	port, err := strconv.Atoi(addr[i+1:])
	return port, err == nil
}

type details struct {
	pgid    int
	command string
	cwd     string
}

func parseDetails(out string) map[int]details {
	found := map[int]details{}
	pid, have := 0, false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		field, value := line[0], line[1:]
		if field == 'p' {
			n, err := strconv.Atoi(value)
			pid, have = n, err == nil
			if have {
				found[pid] = details{pgid: pid}
			}
			continue
		}
		if !have {
			continue
		}
		d := found[pid]
		switch field {
		case 'g':
			if pgid, err := strconv.Atoi(value); err == nil {
				d.pgid = pgid
			}
		case 'c':
			d.command = value
		case 'n':
			d.cwd = value
		}
		found[pid] = d
	}
	return found
}

func merge(listeners []domain.Listener, found map[int]details) []domain.Listener {
	for i, l := range listeners {
		d, ok := found[l.PID]
		if !ok {
			continue
		}
		listeners[i].PGID, listeners[i].Cwd = d.pgid, d.cwd
		if d.command != "" {
			listeners[i].Command = d.command
		}
	}
	return listeners
}

func pidList(listeners []domain.Listener) string {
	seen := map[int]bool{}
	var pids []int
	for _, l := range listeners {
		if !seen[l.PID] {
			seen[l.PID] = true
			pids = append(pids, l.PID)
		}
	}
	sort.Ints(pids)
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}
