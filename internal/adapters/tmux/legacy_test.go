package tmux

import (
	"slices"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

func TestTheLegacyServerKeepsTheOldNamesAndEveryOtherServerGetsCrewNames(t *testing.T) {
	tests := []struct {
		socket string
		want   names
	}{
		{domain.LegacyTmuxServer, names{session: "agentws", option: "@agentws", titleOption: "@agentws_title", popupPrefix: "agentws-popup-", nativePrefix: "agentws-native-"}},
		{"crew", names{session: "crew", option: "@crew", titleOption: "@crew_title", popupPrefix: "crew-popup-", nativePrefix: "crew-native-"}},
		{"crew-test-1", names{session: "crew", option: "@crew", titleOption: "@crew_title", popupPrefix: "crew-popup-", nativePrefix: "crew-native-"}},
	}
	for _, tc := range tests {
		if got := namesFor(tc.socket); got != tc.want {
			t.Errorf("namesFor(%q) = %+v, want %+v", tc.socket, got, tc.want)
		}
	}
}

func TestAttachTargetsTheSessionOfTheServerItRunsOn(t *testing.T) {
	for socket, want := range map[string]string{domain.LegacyTmuxServer: "=agentws:@1", "crew": "=crew:@1"} {
		attach := New(Config{Socket: socket}).AttachCommand("@1")
		if !slices.Contains(attach, want) {
			t.Errorf("socket %s: attach = %v, want %s", socket, attach, want)
		}
	}
}

func TestThePopupFilterUsesThePrefixOfItsServer(t *testing.T) {
	out := "/dev/pts/5 agentws 100\n/dev/pts/9 agentws-popup-1-2 200\n"
	if got, ok := activeClient(out, "agentws-popup-"); !ok || got != "/dev/pts/5" {
		t.Fatalf("activeClient = %q, %v", got, ok)
	}
}
