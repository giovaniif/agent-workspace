package domain

import "time"

const (
	reconnectFirst = 500 * time.Millisecond
	reconnectMax   = 5 * time.Second
)

func ReconnectDelay(attempt int) time.Duration {
	d := reconnectFirst
	for i := 1; i < attempt && d < reconnectMax; i++ {
		d *= 2
	}
	return min(d, reconnectMax)
}
