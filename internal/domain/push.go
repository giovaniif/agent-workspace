package domain

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	PushTitleFallback = "agentws"
	PushBodyFallback  = "needs you"
	pushP256dhBytes   = 65
	pushAuthBytes     = 16
)

var ErrPushSubscription = errors.New("not a push subscription this server can use")

type PushSubscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

type PushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

func CheckPushSubscription(s PushSubscription) error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: the endpoint must be an https URL", ErrPushSubscription)
	}
	p256dh, err := decodePushKey(s.P256dh)
	if err != nil || len(p256dh) != pushP256dhBytes || p256dh[0] != 4 {
		return fmt.Errorf("%w: keys.p256dh must be an uncompressed P-256 point", ErrPushSubscription)
	}
	auth, err := decodePushKey(s.Auth)
	if err != nil || len(auth) != pushAuthBytes {
		return fmt.Errorf("%w: keys.auth must be 16 bytes", ErrPushSubscription)
	}
	return nil
}

func decodePushKey(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func SubscribePush(devices []Device, id string, sub PushSubscription) ([]Device, bool) {
	var target *Device
	var others []Device
	for _, d := range devices {
		if d.ID == id {
			s := sub
			d.Push = &s
			target = &d
			continue
		}
		if d.Push != nil && d.Push.Endpoint == sub.Endpoint {
			d.Push = nil
			others = append(others, d)
		}
	}
	if target == nil {
		return nil, false
	}
	return append([]Device{*target}, others...), true
}

func DropPushSubscription(devices []Device, sent PushSubscription) []Device {
	var changed []Device
	for _, d := range devices {
		if d.Push != nil && *d.Push == sent {
			d.Push = nil
			changed = append(changed, d)
		}
	}
	return changed
}

func UnsubscribePush(devices []Device, id string) ([]Device, bool) {
	for _, d := range devices {
		if d.ID != id {
			continue
		}
		if d.Push == nil {
			return nil, true
		}
		d.Push = nil
		return []Device{d}, true
	}
	return nil, false
}

func PushTargets(devices []Device, skip map[string]bool) []PushSubscription {
	var out []PushSubscription
	for _, d := range devices {
		if d.Push != nil && !skip[d.ID] {
			out = append(out, *d.Push)
		}
	}
	return out
}

func PushFor(b Banner) PushMessage {
	title := strings.TrimSpace(b.Title)
	if title == "" {
		title = PushTitleFallback
	}
	body := strings.TrimSpace(b.Body)
	if body == "" {
		body = bannerBody[b.State]
	}
	if body == "" {
		body = PushBodyFallback
	}
	return PushMessage{Title: title, Body: body, URL: SessionURL(b.Group), Tag: b.Group}
}

func SessionURL(id string) string {
	if id == "" {
		return "/"
	}
	return "/#/sessions/" + url.PathEscape(id)
}
