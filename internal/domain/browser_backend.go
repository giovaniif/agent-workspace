package domain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	BrowserAny         = "any"
	BrowserMac         = "mac"
	BrowserHeadless    = "headless"
	BrowserFallbackOff = "off"
)

type BrowserConfig struct {
	Fallback    string
	MaxHeadless int
	Idle        time.Duration
}

func DefaultBrowserConfig() BrowserConfig {
	return BrowserConfig{Fallback: BrowserFallbackOff, MaxHeadless: 2, Idle: 10 * time.Minute}
}

func ParseBrowserConfig(fallback string, maxHeadless *int, idle string) (BrowserConfig, error) {
	cfg := DefaultBrowserConfig()
	switch fallback {
	case "", BrowserFallbackOff:
	case BrowserHeadless:
		cfg.Fallback = BrowserHeadless
	default:
		return DefaultBrowserConfig(), fmt.Errorf("browser: fallback %q must be \"headless\" or \"off\"", fallback)
	}
	if maxHeadless != nil {
		if *maxHeadless < 1 {
			return DefaultBrowserConfig(), fmt.Errorf("browser: max_headless %d must be 1 or more", *maxHeadless)
		}
		cfg.MaxHeadless = *maxHeadless
	}
	if idle != "" {
		d, err := time.ParseDuration(idle)
		if err != nil || d <= 0 {
			return DefaultBrowserConfig(), fmt.Errorf("browser: idle %q is not a duration such as \"10m\"", idle)
		}
		cfg.Idle = d
	}
	return cfg, nil
}

func ValidBrowserChoice(want string) bool {
	return want == "" || want == BrowserAny || want == BrowserMac || want == BrowserHeadless
}

func ChooseBrowser(want string, macConnected bool, cfg BrowserConfig) (string, bool) {
	switch want {
	case BrowserHeadless:
		return BrowserHeadless, true
	case BrowserMac:
		if macConnected {
			return BrowserMac, true
		}
		return "", false
	}
	if macConnected {
		return BrowserMac, true
	}
	if cfg.Fallback == BrowserHeadless {
		return BrowserHeadless, true
	}
	return "", false
}

func AdmitHeadless(running []string, session string, maxHeadless int) error {
	if slices.Contains(running, session) || len(running) < maxHeadless {
		return nil
	}
	return fmt.Errorf("no browser: %d headless browsers run, the limit of [browser] max_headless", maxHeadless)
}

func HeadlessToStop(lastUse map[string]time.Time, live map[string]bool, now time.Time, idle time.Duration) []string {
	var out []string
	for session, last := range lastUse {
		if !live[session] || now.Sub(last) >= idle {
			out = append(out, session)
		}
	}
	slices.Sort(out)
	return out
}

type BrowserCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

type cookieDoc struct {
	Name           string   `json:"name"`
	Value          string   `json:"value"`
	Domain         string   `json:"domain"`
	Path           string   `json:"path"`
	Expires        *float64 `json:"expires"`
	ExpirationDate *float64 `json:"expirationDate"`
	Secure         bool     `json:"secure"`
	HTTPOnly       bool     `json:"httpOnly"`
	SameSite       string   `json:"sameSite"`
}

func ParseCookieFile(data []byte) ([]BrowserCookie, error) {
	trimmed := bytes.TrimSpace(data)
	var out []BrowserCookie
	var err error
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		out, err = jsonCookies(trimmed)
	case len(trimmed) > 0 && trimmed[0] == '{':
		var state struct {
			Cookies json.RawMessage `json:"cookies"`
		}
		if err = json.Unmarshal(trimmed, &state); err == nil {
			if len(state.Cookies) == 0 {
				return nil, errors.New("the JSON has no cookies list: use a cookie list or a Playwright storageState")
			}
			out, err = jsonCookies(state.Cookies)
		}
	default:
		out = netscapeCookies(trimmed)
	}
	if err != nil {
		return nil, fmt.Errorf("the cookie file does not parse: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("the file has no cookies: use cookies.txt, a JSON cookie list or a Playwright storageState")
	}
	return out, nil
}

func jsonCookies(raw []byte) ([]BrowserCookie, error) {
	var docs []cookieDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, err
	}
	out := make([]BrowserCookie, 0, len(docs))
	for _, d := range docs {
		if d.Name == "" || d.Domain == "" {
			return nil, fmt.Errorf("a cookie needs a name and a domain (got name %q, domain %q)", d.Name, d.Domain)
		}
		c := BrowserCookie{Name: d.Name, Value: d.Value, Domain: d.Domain, Path: d.Path, Secure: d.Secure, HTTPOnly: d.HTTPOnly, SameSite: sameSite(d.SameSite)}
		if c.Path == "" {
			c.Path = "/"
		}
		for _, e := range []*float64{d.Expires, d.ExpirationDate} {
			if e != nil && *e > 0 {
				c.Expires = *e
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func sameSite(s string) string {
	switch strings.ToLower(s) {
	case "lax":
		return "Lax"
	case "strict":
		return "Strict"
	case "none", "no_restriction":
		return "None"
	}
	return ""
}

func netscapeCookies(data []byte) []BrowserCookie {
	var out []BrowserCookie
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		httpOnly := false
		if rest, ok := strings.CutPrefix(line, "#HttpOnly_"); ok {
			line, httpOnly = rest, true
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			continue
		}
		c := BrowserCookie{Domain: f[0], Path: f[2], Secure: strings.EqualFold(f[3], "TRUE"), Name: f[5], Value: f[6], HTTPOnly: httpOnly}
		if exp, err := strconv.ParseFloat(f[4], 64); err == nil && exp > 0 {
			c.Expires = exp
		}
		if c.Path == "" {
			c.Path = "/"
		}
		out = append(out, c)
	}
	return out
}

func MergeCookies(old, add []BrowserCookie) []BrowserCookie {
	key := func(c BrowserCookie) string { return c.Domain + "\x00" + c.Path + "\x00" + c.Name }
	byKey := map[string]BrowserCookie{}
	for _, c := range old {
		byKey[key(c)] = c
	}
	for _, c := range add {
		byKey[key(c)] = c
	}
	out := make([]BrowserCookie, 0, len(byKey))
	for _, c := range byKey {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b BrowserCookie) int { return strings.Compare(key(a), key(b)) })
	return out
}
