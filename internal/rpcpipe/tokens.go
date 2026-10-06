package rpcpipe

import (
	"encoding/json"

	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/syntax"
)

type object map[string]json.RawMessage

type reviewOpenParams struct {
	Tokens bool `json:"tokens"`
}

func wantsTokens(req rpc.Request) bool {
	if req.Method != rpc.MethodReviewOpen || len(req.Params) == 0 {
		return false
	}
	var p reviewOpenParams
	return json.Unmarshal(req.Params, &p) == nil && p.Tokens
}

func withSpans(result json.RawMessage) (json.RawMessage, error) {
	var review object
	if err := json.Unmarshal(result, &review); err != nil {
		return nil, err
	}
	var worktrees []object
	if err := decode(review["worktrees"], &worktrees); err != nil {
		return nil, err
	}
	for _, wt := range worktrees {
		var files []object
		if err := decode(wt["Files"], &files); err != nil {
			return nil, err
		}
		for _, f := range files {
			if err := fileSpans(f); err != nil {
				return nil, err
			}
		}
		if err := put(wt, "Files", files); err != nil {
			return nil, err
		}
	}
	if err := put(review, "worktrees", worktrees); err != nil {
		return nil, err
	}
	return json.Marshal(review)
}

func fileSpans(f object) error {
	var path string
	var hunks []object
	if err := decode(f["Path"], &path); err != nil {
		return err
	}
	if err := decode(f["Hunks"], &hunks); err != nil {
		return err
	}
	lines := make([][]object, len(hunks))
	var texts []string
	for i, h := range hunks {
		if err := decode(h["Lines"], &lines[i]); err != nil {
			return err
		}
		for _, l := range lines[i] {
			var text string
			if err := decode(l["Text"], &text); err != nil {
				return err
			}
			texts = append(texts, text)
		}
	}
	spans := syntax.Spans(path, texts)
	if spans == nil {
		return nil
	}
	n := 0
	for i, h := range hunks {
		for _, l := range lines[i] {
			if err := put(l, "Spans", nonNil(spans[n])); err != nil {
				return err
			}
			n++
		}
		if err := put(h, "Lines", lines[i]); err != nil {
			return err
		}
	}
	return put(f, "Hunks", hunks)
}

func nonNil(s []syntax.Span) []syntax.Span {
	if s == nil {
		return []syntax.Span{}
	}
	return s
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func put(o object, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	o[key] = b
	return nil
}
