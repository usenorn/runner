package entity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestAPreviewPathThatLeavesTheServiceIsRefused(t *testing.T) {
	for _, path := range []string{"../admin", "/app/../../etc", "admin"} {
		err := entity.Preview{Name: "web", Service: "web", Path: path}.Valid()

		if !errors.Is(err, entity.ErrPreviewInvalid) {
			t.Fatalf(
				"a preview opening at %q was accepted: %v. What follows the port is handed "+
					"straight to whatever is listening",
				path, err,
			)
		}
	}
}

func TestAPreviewNamedLikeAServiceIsAccepted(t *testing.T) {
	for _, path := range []string{"", "/", "/admin", "/app/settings"} {
		if err := (entity.Preview{Name: "web-2", Service: "api", Path: path}).Valid(); err != nil {
			t.Fatalf("an ordinary preview opening at %q was refused: %v", path, err)
		}
	}
}

func TestAPreviewAddressLeadsToThePortAndThePath(t *testing.T) {
	address := entity.PreviewURL(43111, "/admin")

	if !strings.HasSuffix(address, ":43111/admin") {
		t.Fatalf("a preview address reads %q, which is not where the service is", address)
	}
}

func TestASharedPreviewAddressIsTheIssueTheRunAndThePortAndNothingTheAgentNamedIt(t *testing.T) {
	serving := entity.PreviewService{Gateway: "gateway.norn.ink", Domain: "norn.ink", Scheme: "https"}
	execution := entity.Execution{
		ID:       "exec-01M0SMJXBJ451KZ0MCQ6TY2GH1",
		IssueKey: "NORN-75",
		Title:    "A preview address must be one per task, execution and port",
	}

	want := "https://norn-75-a-preview-address-exec-01m0smjxbj451kz0mcq6ty2gh1-43000.norn.ink/admin"

	if got := serving.Address(execution, 43000, "/admin"); got != want {
		t.Fatalf(
			"the address came out as %q, want %q. This is the link a pull request carries, so "+
				"it has to be the one norn's gateway routes by",
			got, want,
		)
	}
}

func TestTwoPreviewsOfOneRunShareAnAddressOnlyWhenTheyShareAPort(t *testing.T) {
	serving := entity.PreviewService{Gateway: "gateway.norn.ink", Domain: "norn.ink", Scheme: "https"}
	execution := entity.Execution{ID: "exec-01ABC", IssueKey: "NORN-75", Title: "Preview address"}

	web := serving.Address(execution, 43000, "")
	api := serving.Address(execution, 43001, "")
	renamed := serving.Address(execution, 43000, "")

	if web == api {
		t.Fatalf(
			"two ports of one run answered to the same address %q, so one preview would reach "+
				"the other's service",
			web,
		)
	}

	if web != renamed {
		t.Fatalf(
			"the same port came out as %q and then %q. A link somebody already sent outside "+
				"the workspace has to keep working for the life of the run",
			web, renamed,
		)
	}
}
