package entity_test

import (
	"errors"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestALabelThatIsAPathIsRefused(t *testing.T) {
	for _, label := range []string{"", "reports/one.md", "a\\b", "a\nb"} {
		err := entity.Artifact{Path: "report.md", Label: label}.Valid()

		if !errors.Is(err, entity.ErrArtifactInvalid) {
			t.Fatalf(
				"an artifact labelled %q was accepted: %v. That label is written where a "+
					"person reads it and where norn stores it",
				label, err,
			)
		}
	}
}

func TestAnArtifactWithNoFileNamedIsRefused(t *testing.T) {
	if err := (entity.Artifact{Label: "notes"}).Valid(); !errors.Is(
		err, entity.ErrArtifactInvalid,
	) {
		t.Fatalf("an artifact naming no file was accepted: %v", err)
	}
}

func TestAnUnlabelledArtifactIsCalledAfterItsFile(t *testing.T) {
	if label := entity.ArtifactLabelFor("reports/coverage.html"); label != "coverage.html" {
		t.Fatalf("a file with no label of its own would be shown as %q", label)
	}
}
