package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChooseBrowserPicksTheMacAppThenTheHeadlessFallback(t *testing.T) {
	on := BrowserConfig{Fallback: BrowserHeadless, MaxHeadless: 2}
	off := BrowserConfig{Fallback: BrowserFallbackOff, MaxHeadless: 2}
	cases := []struct {
		name string
		want string
		mac  bool
		cfg  BrowserConfig
		got  string
		ok   bool
	}{
		{"any with the mac app", BrowserAny, true, on, BrowserMac, true},
		{"empty is any", "", true, off, BrowserMac, true},
		{"any without the mac app and fallback on", BrowserAny, false, on, BrowserHeadless, true},
		{"any without the mac app and fallback off", BrowserAny, false, off, "", false},
		{"empty without the mac app and fallback off", "", false, off, "", false},
		{"mac with the mac app", BrowserMac, true, on, BrowserMac, true},
		{"mac without the mac app never falls back", BrowserMac, false, on, "", false},
		{"headless with the mac app", BrowserHeadless, true, off, BrowserHeadless, true},
		{"headless without the mac app and fallback off", BrowserHeadless, false, off, BrowserHeadless, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ChooseBrowser(c.want, c.mac, c.cfg)
			if got != c.got || ok != c.ok {
				t.Fatalf("got %q %v, want %q %v", got, ok, c.got, c.ok)
			}
		})
	}
}

func TestParseBrowserConfig(t *testing.T) {
	two, zero, neg := 3, 0, -1
	cases := []struct {
		name     string
		fallback string
		max      *int
		idle     string
		want     BrowserConfig
		err      string
	}{
		{"defaults", "", nil, "", BrowserConfig{Fallback: BrowserFallbackOff, MaxHeadless: 2, Idle: 10 * time.Minute}, ""},
		{"all set", "headless", &two, "90s", BrowserConfig{Fallback: BrowserHeadless, MaxHeadless: 3, Idle: 90 * time.Second}, ""},
		{"off", "off", nil, "", BrowserConfig{Fallback: BrowserFallbackOff, MaxHeadless: 2, Idle: 10 * time.Minute}, ""},
		{"unknown fallback", "chrome", nil, "", DefaultBrowserConfig(), "fallback"},
		{"zero max", "", &zero, "", DefaultBrowserConfig(), "max_headless"},
		{"negative max", "", &neg, "", DefaultBrowserConfig(), "max_headless"},
		{"bad idle", "", nil, "soon", DefaultBrowserConfig(), "idle"},
		{"zero idle", "", nil, "0s", DefaultBrowserConfig(), "idle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseBrowserConfig(c.fallback, c.max, c.idle)
			if c.err == "" && err != nil {
				t.Fatal(err)
			}
			if c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
				t.Fatalf("err %v, want it to name %s", err, c.err)
			}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestAdmitHeadlessKeepsOneBrowserPerSessionUnderTheCap(t *testing.T) {
	if err := AdmitHeadless([]string{"a", "b"}, "a", 2); err != nil {
		t.Fatalf("a session that has a browser: %v", err)
	}
	if err := AdmitHeadless([]string{"a"}, "b", 2); err != nil {
		t.Fatalf("room for one more: %v", err)
	}
	err := AdmitHeadless([]string{"a", "b"}, "c", 2)
	if err == nil || !strings.Contains(err.Error(), "max_headless") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("full: %v", err)
	}
}

func TestHeadlessToStopNamesIdleAndEndedSessions(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	last := map[string]time.Time{
		"busy":  now.Add(-time.Minute),
		"idle":  now.Add(-11 * time.Minute),
		"ended": now,
		"edge":  now.Add(-10 * time.Minute),
	}
	live := map[string]bool{"busy": true, "idle": true, "edge": true}
	got := HeadlessToStop(last, live, now, 10*time.Minute)
	if want := []string{"edge", "ended", "idle"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseCookieFileReadsCookiesTxt(t *testing.T) {
	data := "# Netscape HTTP Cookie File\n" +
		".example.com\tTRUE\t/\tTRUE\t1893456000\tsid\tabc\n" +
		"#HttpOnly_app.example.com\tFALSE\t/api\tFALSE\t0\ttoken\tx y\n" +
		"\n" +
		"broken line\n"
	got, err := ParseCookieFile([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := []BrowserCookie{
		{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/", Expires: 1893456000, Secure: true},
		{Name: "token", Value: "x y", Domain: "app.example.com", Path: "/api", HTTPOnly: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseCookieFileReadsAJSONListAndPlaywrightStorageState(t *testing.T) {
	list := `[{"name":"sid","value":"abc","domain":".example.com","path":"/","expirationDate":1893456000.5,"secure":true,"httpOnly":true,"sameSite":"lax"}]`
	state := `{"cookies":[{"name":"sid","value":"abc","domain":".example.com","path":"/","expires":1893456000.5,"secure":true,"httpOnly":true,"sameSite":"Lax"}],"origins":[]}`
	want := []BrowserCookie{{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/", Expires: 1893456000.5, Secure: true, HTTPOnly: true, SameSite: "Lax"}}
	for name, data := range map[string]string{"list": list, "storageState": state} {
		got, err := ParseCookieFile([]byte(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %+v\nwant %+v", name, got, want)
		}
	}
}

func TestParseCookieFileDefaultsThePathAndDropsSessionExpiry(t *testing.T) {
	got, err := ParseCookieFile([]byte(`[{"name":"a","value":"1","domain":"example.com","expires":-1,"sameSite":"no_restriction"}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []BrowserCookie{{Name: "a", Value: "1", Domain: "example.com", Path: "/", SameSite: "None"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseCookieFileRefusesFilesWithNoCookies(t *testing.T) {
	for name, data := range map[string]string{
		"empty":       "",
		"comments":    "# Netscape HTTP Cookie File\n",
		"not cookies": `{"hello":"world"}`,
		"no domain":   `[{"name":"a","value":"1"}]`,
		"bad json":    `[{"name":`,
	} {
		if _, err := ParseCookieFile([]byte(data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestMergeCookiesReplacesTheSameNameDomainAndPath(t *testing.T) {
	old := []BrowserCookie{
		{Name: "sid", Value: "old", Domain: ".example.com", Path: "/"},
		{Name: "keep", Value: "1", Domain: ".example.com", Path: "/"},
		{Name: "sid", Value: "other", Domain: ".example.com", Path: "/api"},
	}
	add := []BrowserCookie{{Name: "sid", Value: "new", Domain: ".example.com", Path: "/"}, {Name: "x", Value: "2", Domain: "b.com", Path: "/"}}
	got := MergeCookies(old, add)
	want := []BrowserCookie{
		{Name: "keep", Value: "1", Domain: ".example.com", Path: "/"},
		{Name: "sid", Value: "new", Domain: ".example.com", Path: "/"},
		{Name: "sid", Value: "other", Domain: ".example.com", Path: "/api"},
		{Name: "x", Value: "2", Domain: "b.com", Path: "/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestValidateAutomationChecksTheBrowser(t *testing.T) {
	for _, b := range []string{"", "any", "mac", "headless"} {
		a := validAutomation()
		a.Browser = b
		if err := ValidateAutomation(a); err != nil {
			t.Errorf("%q: %v", b, err)
		}
	}
	a := validAutomation()
	a.Browser = "chrome"
	if err := ValidateAutomation(a); err == nil || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("got %v", err)
	}
}

func TestAutomationTOMLWritesTheBrowser(t *testing.T) {
	a := validAutomation()
	a.Browser = "headless"
	if got := AutomationTOML(a); !strings.Contains(got, "browser = \"headless\"\n") {
		t.Fatalf("got %s", got)
	}
	a.Browser = ""
	if got := AutomationTOML(a); strings.Contains(got, "browser") {
		t.Fatalf("got %s", got)
	}
}
