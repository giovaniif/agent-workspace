package domain

import (
	"testing"
	"time"
)

func TestReconnectDelayDoublesFromHalfASecondUpToFive(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 5 * time.Second},
		{6, 5 * time.Second},
		{60, 5 * time.Second},
	} {
		if got := ReconnectDelay(tc.attempt); got != tc.want {
			t.Errorf("ReconnectDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}
