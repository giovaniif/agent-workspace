package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func startPairing(t *testing.T, store app.Store) (*rpc.Client, *setClock) {
	t.Helper()
	clock := &setClock{now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	_, path := start(t, store, daemon.WithClock(clock.Now))
	return dial(t, path), clock
}

func pairCode(t *testing.T, c *rpc.Client, name string) rpc.PairCode {
	t.Helper()
	var code rpc.PairCode
	if err := c.Call(context.Background(), rpc.MethodPairCode, rpc.PairCodeParams{Name: name}, &code); err != nil {
		t.Fatal(err)
	}
	return code
}

func redeem(c *rpc.Client, code, name, addr string) (rpc.PairRedeemed, error) {
	var out rpc.PairRedeemed
	err := c.Call(context.Background(), rpc.MethodPairRedeem, rpc.PairRedeemParams{Code: code, Name: name, Addr: addr}, &out)
	return out, err
}

func errCode(err error) string {
	var rerr *rpc.Error
	if errors.As(err, &rerr) {
		return rerr.Code
	}
	return ""
}

func checkDevice(c *rpc.Client, token string) (rpc.Device, error) {
	var out rpc.DeviceChecked
	err := c.Call(context.Background(), rpc.MethodDeviceCheck, rpc.DeviceCheckParams{Token: token}, &out)
	return out.Device, err
}

func TestPairCodeWorksOnceAndNotAfterFiveMinutes(t *testing.T) {
	c, clock := startPairing(t, &memStore{})
	code := pairCode(t, c, "phone")
	if len(code.Code) != domain.PairCodeLength || !code.ExpiresAt.Equal(clock.Now().Add(5*time.Minute)) {
		t.Fatalf("code %+v", code)
	}
	got, err := redeem(c, code.Code, "", "100.64.0.2:51234")
	if err != nil {
		t.Fatal(err)
	}
	if got.Device.Name != "phone" || got.Device.ID == "" || len(got.Token) != 43 {
		t.Fatalf("redeemed %+v", got.Device)
	}
	if _, err := redeem(c, code.Code, "", "100.64.0.2:51234"); errCode(err) != rpc.CodeUnauthorized {
		t.Fatalf("second redeem: %v", err)
	}

	late := pairCode(t, c, "")
	clock.advance(5 * time.Minute)
	if _, err := redeem(c, late.Code, "ipad", "100.64.0.2:51234"); errCode(err) != rpc.CodeUnauthorized {
		t.Fatalf("redeem after five minutes: %v", err)
	}
}

func TestPairRedeemRefusesASixthFailedTryFromOneAddressAndKeepsTheCode(t *testing.T) {
	c, _ := startPairing(t, &memStore{})
	code := pairCode(t, c, "")
	for i := range domain.PairFailsPerAddress {
		if _, err := redeem(c, "WRONG234", "", "100.64.0.9:4000"); errCode(err) != rpc.CodeUnauthorized {
			t.Fatalf("try %d: %v", i+1, err)
		}
	}
	if _, err := redeem(c, code.Code, "", "100.64.0.9:4001"); errCode(err) != rpc.CodeRateLimited {
		t.Fatalf("sixth try from the same host: %v", err)
	}
	if _, err := redeem(c, code.Code, "", "100.64.0.10:4000"); err != nil {
		t.Fatalf("the code survives wrong guesses: %v", err)
	}
}

func TestPairRedeemNeedsTheCallersAddress(t *testing.T) {
	c, _ := startPairing(t, &memStore{})
	code := pairCode(t, c, "")
	if _, err := redeem(c, code.Code, "", ""); errCode(err) != rpc.CodeBadRequest {
		t.Fatalf("no address: %v", err)
	}
}

func TestDeviceTokenIsStoredOnlyAsItsHash(t *testing.T) {
	store := &memStore{}
	c, _ := startPairing(t, store)
	got, err := redeem(c, pairCode(t, c, "phone").Code, "", "100.64.0.2")
	if err != nil {
		t.Fatal(err)
	}
	devices := store.devices()
	if len(devices) != 1 || devices[0].TokenHash != domain.HashDeviceToken(got.Token) {
		t.Fatalf("stored %+v", devices)
	}
	stored, _ := json.Marshal(devices)
	if strings.Contains(string(stored), got.Token) {
		t.Fatal("the token was stored in clear")
	}
	var list rpc.DeviceList
	if err := c.Call(context.Background(), rpc.MethodDeviceList, nil, &list); err != nil {
		t.Fatal(err)
	}
	listed, _ := json.Marshal(list)
	if strings.Contains(string(listed), devices[0].TokenHash) || len(list.Devices) != 1 || list.Devices[0].ID != got.Device.ID {
		t.Fatalf("device.list %s", listed)
	}
}

func TestDeviceCheckKnowsTheTokenAndRecordsLastSeen(t *testing.T) {
	store := &memStore{}
	c, clock := startPairing(t, store)
	got, err := redeem(c, pairCode(t, c, "phone").Code, "", "100.64.0.2")
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Hour)
	dev, err := checkDevice(c, got.Token)
	if err != nil || dev.ID != got.Device.ID || !dev.LastSeen.Equal(clock.Now()) {
		t.Fatalf("check: %+v, %v", dev, err)
	}
	if store.devices()[0].LastSeen != clock.Now() {
		t.Fatalf("last seen not stored: %+v", store.devices())
	}
	if _, err := checkDevice(c, "not-a-token"); errCode(err) != rpc.CodeUnauthorized {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestRevokedDeviceFailsCheckAtOnceAndIsAnnounced(t *testing.T) {
	store := &memStore{}
	c, _ := startPairing(t, store)
	got, err := redeem(c, pairCode(t, c, "phone").Code, "", "100.64.0.2")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, rpc.MethodDeviceRevoke, rpc.DeviceRevokeParams{ID: got.Device.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := checkDevice(c, got.Token); errCode(err) != rpc.CodeUnauthorized {
		t.Fatalf("revoked token: %v", err)
	}
	if diff := next(t, sub.Diffs); diff.RevokedDevice != got.Device.ID {
		t.Fatalf("diff %+v", diff)
	}
	if len(store.devices()) != 0 {
		t.Fatalf("still stored: %+v", store.devices())
	}
	if err := c.Call(ctx, rpc.MethodDeviceRevoke, rpc.DeviceRevokeParams{ID: got.Device.ID}, nil); errCode(err) != rpc.CodeNotFound {
		t.Fatalf("revoking again: %v", err)
	}
}

func TestPairedDevicesSurviveARestart(t *testing.T) {
	dev, token := domain.NewDevice("abcdefgh", "phone", make([]byte, domain.DeviceTokenBytes), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	c, _ := startPairing(t, &memStore{snap: app.Snapshot{Devices: []domain.Device{dev}}})
	got, err := checkDevice(c, token)
	if err != nil || got.ID != "abcdefgh" || got.Name != "phone" {
		t.Fatalf("check: %+v, %v", got, err)
	}
}
