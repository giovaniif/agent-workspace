package tmux

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func (h *Host) LastInput(ctx context.Context) (time.Time, error) {
	out, err := h.invoke(ctx, "", "list-clients", "-F", "#{client_activity}")
	if err != nil {
		if isServerGone(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return newestActivity(out), nil
}

func newestActivity(out string) time.Time {
	var newest int64
	for _, line := range strings.Split(out, "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil && n > newest {
			newest = n
		}
	}
	if newest == 0 {
		return time.Time{}
	}
	return time.Unix(newest, 0)
}
