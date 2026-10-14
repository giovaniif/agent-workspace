package serve

import (
	"net/http"

	"github.com/giovaniif/crew/internal/rpc"
)

type deviceKey struct{}

func deviceOf(r *http.Request) rpc.Device {
	dev, _ := r.Context().Value(deviceKey{}).(rpc.Device)
	return dev
}

func pushKey(r *http.Request, d Daemon) (any, error) {
	var out rpc.PushKey
	if err := d.Call(r.Context(), rpc.MethodPushKey, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func pushSubscribe(r *http.Request, d Daemon) (any, error) {
	var body struct {
		Endpoint string       `json:"endpoint"`
		Keys     rpc.PushKeys `json:"keys"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "send the browser's PushSubscription: endpoint, keys.p256dh and keys.auth"}
	}
	p := rpc.PushSubscribeParams{Device: deviceOf(r).ID, Endpoint: body.Endpoint, Keys: body.Keys}
	if err := d.Call(r.Context(), rpc.MethodPushSubscribe, p, nil); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func pushUnsubscribe(r *http.Request, d Daemon) (any, error) {
	if err := d.Call(r.Context(), rpc.MethodPushUnsubscribe, rpc.PushUnsubscribeParams{Device: deviceOf(r).ID}, nil); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}
