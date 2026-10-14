package daemon

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"sort"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type DeviceRevoked struct{ ID string }

func (e DeviceRevoked) apply(s *state) rpc.Diff {
	delete(s.devices, e.ID)
	s.store.DeleteDevice(e.ID)
	return rpc.Diff{RevokedDevice: e.ID}
}

func randomIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}

func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func (d *Daemon) pairMethod(req rpc.Request) (*rpc.Response, bool) {
	switch req.Method {
	case rpc.MethodPairCode:
		return d.pairCode(req)
	case rpc.MethodPairRedeem:
		return d.pairRedeem(req)
	case rpc.MethodDeviceCheck:
		return d.deviceCheck(req)
	case rpc.MethodDeviceList:
		return d.deviceList(req)
	default:
		return d.deviceRevoke(req)
	}
}

func (d *Daemon) pairCode(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.PairCodeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "pair.code params: "+err.Error()), true
		}
	}
	code := domain.NewPairCode(randomIntn)
	var issued domain.PairCode
	ok := d.query(func(s *state) { issued = s.pairing.Issue(code, p.Name, d.ws.now()) })
	return result(req.ID, rpc.PairCode{Code: issued.Code, ExpiresAt: issued.ExpiresAt}), ok
}

func (d *Daemon) pairRedeem(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.PairRedeemParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "pair.redeem params: "+err.Error()), true
	}
	if p.Addr == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "pair.redeem needs the caller's address"), true
	}
	random := make([]byte, domain.DeviceTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, "no randomness for a device token: "+err.Error()), true
	}
	var dev domain.Device
	var token string
	var rerr error
	ok := d.query(func(s *state) {
		now := d.ws.now()
		issued, err := s.pairing.Redeem(p.Code, hostOf(p.Addr), now)
		if err != nil {
			rerr = err
			return
		}
		id := domain.NewDeviceID(randomIntn, func(id string) bool { _, taken := s.devices[id]; return taken })
		dev, token = domain.NewDevice(id, domain.DeviceName(p.Name, issued.Name), random, now)
		s.devices[dev.ID] = dev
		s.store.PutDevice(dev)
	})
	switch {
	case errors.Is(rerr, domain.ErrPairRateLimited):
		return errorResponse(req.ID, rpc.CodeRateLimited, rerr.Error()), ok
	case rerr != nil:
		return errorResponse(req.ID, rpc.CodeUnauthorized, rerr.Error()), ok
	}
	return result(req.ID, rpc.PairRedeemed{Device: rpc.DeviceOf(dev), Token: token}), ok
}

func (d *Daemon) deviceCheck(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.DeviceCheckParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "device.check params are not valid JSON"), true
	}
	var dev domain.Device
	var found bool
	ok := d.query(func(s *state) {
		dev, found = domain.CheckDeviceToken(sorted(s.devices), p.Token)
		if !found {
			return
		}
		var changed bool
		if dev, changed = dev.Seen(d.ws.now()); changed {
			s.devices[dev.ID] = dev
			s.store.PutDevice(dev)
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeUnauthorized, "unknown or revoked device token"), ok
	}
	return result(req.ID, rpc.DeviceChecked{Device: rpc.DeviceOf(dev)}), ok
}

func (d *Daemon) deviceList(req rpc.Request) (*rpc.Response, bool) {
	var list rpc.DeviceList
	ok := d.query(func(s *state) {
		list.Devices = make([]rpc.Device, 0, len(s.devices))
		for _, dev := range s.devices {
			list.Devices = append(list.Devices, rpc.DeviceOf(dev))
		}
	})
	sort.Slice(list.Devices, func(i, j int) bool {
		a, b := list.Devices[i], list.Devices[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return result(req.ID, list), ok
}

func (d *Daemon) deviceRevoke(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.DeviceRevokeParams
	if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "device.revoke needs an id"), true
	}
	var found bool
	ok := d.query(func(s *state) {
		if _, found = domain.RevokeDevice(sorted(s.devices), p.ID); found {
			s.emit(DeviceRevoked{ID: p.ID})
		}
	})
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no device "+p.ID), ok
	}
	return result(req.ID, struct{}{}), ok
}
