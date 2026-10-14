package github

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var _ app.TitleResolver = Titles{}

type Titles struct {
	Bin string
}

func (t Titles) Title(ctx context.Context, task domain.Task) (string, error) {
	if task.Source != domain.TaskPR || task.URL == "" {
		return "", nil
	}
	bin := t.Bin
	if bin == "" {
		bin = "gh"
	}
	out, err := exec.CommandContext(ctx, bin, "pr", "view", task.URL, "--json", "title").Output()
	if err != nil {
		return "", err
	}
	var pr struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return "", err
	}
	return strings.TrimSpace(pr.Title), nil
}
