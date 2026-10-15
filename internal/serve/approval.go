package serve

import (
	"net/http"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func listApprovals(r *http.Request, d Daemon) (any, error) {
	var out []domain.Approval
	if err := d.Call(r.Context(), rpc.MethodApprovalList, struct{}{}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []domain.Approval{}
	}
	return out, nil
}

func decideApproval(approve bool) func(r *http.Request, d Daemon) (any, error) {
	return func(r *http.Request, d Daemon) (any, error) {
		var out domain.Approval
		if err := d.Call(r.Context(), rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: r.PathValue("id"), Approve: approve}, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}
