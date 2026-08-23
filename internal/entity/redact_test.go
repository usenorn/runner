package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestATicketNeverSurvivesIntoSomethingAPersonCanRead(t *testing.T) {
	failure := `failed to WebSocket dial: Get "http://tunnel.norn.ink/v1/runners/tunnel?` +
		`ticket=nru_Mwp5sq765zn0NTdt33hFUPm2WWPvzvSp9zAiDgui6u0": dial tcp: connection refused`

	said := entity.Redacted(failure)

	if strings.Contains(said, "nru_Mwp5sq765zn0NTdt33hFUPm2WWPvzvSp9zAiDgui6u0") {
		t.Fatalf(
			"a dial failure carried the ticket into %q; norn runner status is printed, pasted "+
				"into issues and read over shoulders, and a credential does not belong in it",
			said,
		)
	}

	if !strings.Contains(said, "connection refused") {
		t.Fatalf(
			"redacting took the reason with it (%q); somebody reading the line still has to be "+
				"able to tell a refused connection from a bad name",
			said,
		)
	}
}
