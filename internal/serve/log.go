package serve

import (
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	LogBurst  = 20
	LogWindow = time.Minute
)

type limitedLog struct {
	mu      sync.Mutex
	w       io.Writer
	now     func() time.Time
	start   time.Time
	written int
	dropped int
}

func newLimitedLog(w io.Writer, now func() time.Time) *limitedLog {
	if now == nil {
		now = time.Now
	}
	return &limitedLog{w: w, now: now}
}

func (l *limitedLog) printf(format string, args ...any) {
	if l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.start) >= LogWindow {
		if l.dropped > 0 {
			_, _ = fmt.Fprintf(l.w, "agentws serve: dropped %d log lines in the last %s\n", l.dropped, LogWindow)
		}
		l.start, l.written, l.dropped = now, 0, 0
	}
	if l.written >= LogBurst {
		l.dropped++
		return
	}
	l.written++
	_, _ = fmt.Fprintf(l.w, "agentws serve: "+format+"\n", args...)
}
