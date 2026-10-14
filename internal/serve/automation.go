package serve

import (
	"encoding/json"
	"net/http"

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
