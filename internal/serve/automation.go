package serve

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func listAutomations(r *http.Request, d Daemon) (any, error) {
	var out rpc.AutomationList
	if err := d.Call(r.Context(), rpc.MethodAutomationList, rpc.AutomationListParams{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func runAutomation(r *http.Request, d Daemon) (any, error) {
	var body struct {
		Context  string `json:"context"`
		WorkItem string `json:"work_item"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "the body takes only context and work_item: " + err.Error()}
	}
	p := rpc.AutomationRunParams{Name: r.PathValue("name"), Context: body.Context, WorkItem: body.WorkItem}
	var out rpc.AutomationRunResult
	if err := d.Call(r.Context(), rpc.MethodAutomationRun, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func automationRuns(r *http.Request, d Daemon) (any, error) {
	q := r.URL.Query()
	p := rpc.AutomationRunsParams{Name: q.Get("name")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "limit is a number of runs: " + v}
		}
		p.Limit = n
	}
	var out rpc.AutomationRuns
	if err := d.Call(r.Context(), rpc.MethodAutomationRuns, p, &out); err != nil {
		return nil, err
	}
	if out.Runs == nil {
		out.Runs = []domain.AutomationRun{}
	}
	return out, nil
}
