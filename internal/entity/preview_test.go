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
