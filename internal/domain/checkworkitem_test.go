package domain

import (
	"reflect"
	"testing"
)

func TestCheckWorkItem(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"free text", "tidy up the readme", ""},
		{"a branch name", "feature/login-fix", ""},
		{"an issue tracker URL is not a PR", "https://tracker.example.com/acme/issue/ENG-1/x", "unsupported link: use a GitHub pull request URL"},
		{"github PR URL", "https://github.com/acme/web/pull/4", ""},
		{"empty", "", "work item is empty"},
		{"blank", "  \t ", "work item is empty"},
		{"a github issue URL is not a PR", "https://github.com/acme/web/issues/4", "unsupported link: use a GitHub pull request URL"},
		{"another site", "https://example.com/x", "unsupported link: use a GitHub pull request URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task, err := CheckWorkItem(c.input)
			if c.wantErr == "" {
				if err != nil || !reflect.DeepEqual(task, ParseWorkItem(c.input)) {
					t.Fatalf("got %+v, %v", task, err)
				}
				return
			}
			if err == nil || err.Error() != c.wantErr {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
		})
	}
}
