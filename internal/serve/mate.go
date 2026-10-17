package serve

import (
	"net/http"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type MateSetup = rpc.MateSetup

func listMates(r *http.Request, d Daemon) (any, error) {
	var out rpc.MateList
	if err := d.Call(r.Context(), rpc.MethodMateList, struct{}{}, &out); err != nil {
		return nil, err
	}
	if out.Mates == nil {
		out.Mates = []domain.Mate{}
	}
	return out, nil
}

func setMate(r *http.Request, d Daemon) (any, error) {
	var p rpc.MateSetParams
	if err := decodeBody(r, &p); err != nil {
		return nil, err
	}
	var out domain.Mate
	if err := d.Call(r.Context(), rpc.MethodMateSet, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mateByName(method string) func(r *http.Request, d Daemon) (any, error) {
	return func(r *http.Request, d Daemon) (any, error) {
		var out domain.Mate
		if err := d.Call(r.Context(), method, rpc.MateNameParams{Name: r.PathValue("name")}, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func stopMates(r *http.Request, d Daemon) (any, error) {
	var p rpc.MateStopAllParams
	if err := decodeBody(r, &p); err != nil {
		return nil, err
	}
	var out domain.MateLimits
	if err := d.Call(r.Context(), rpc.MethodMateStopAll, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mateSetup(r *http.Request, d Daemon) (any, error) {
	var out MateSetup
	if err := d.Call(r.Context(), rpc.MethodMateSetup, rpc.MateShowParams{Name: r.PathValue("name")}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func setMateTokens(r *http.Request, d Daemon) (any, error) {
	var p rpc.MateSetTokensParams
	if err := decodeBody(r, &p); err != nil {
		return nil, err
	}
	p.Name = r.PathValue("name")
	var out rpc.MateTokenStatus
	if err := d.Call(r.Context(), rpc.MethodMateSetTokens, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func claimMate(r *http.Request, d Daemon) (any, error) {
	var out rpc.MateClaim
	if err := d.Call(r.Context(), rpc.MethodMateClaim, rpc.MateClaimParams{Name: r.PathValue("name")}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
