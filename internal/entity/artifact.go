package entity

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
)

const ArtifactLabelMax = 120

var (
	ErrArtifactMissing  = errors.New("there is no file at that path in this run's workspace")
	ErrArtifactOutside  = errors.New("a run may only publish a file from its own workspace")
	ErrArtifactTooLarge = errors.New("that file is larger than norn will take in one artifact")
	ErrArtifactInvalid  = errors.New("that artifact cannot be published")
)

var artifactLabel = regexp.MustCompile(`^[^\x00-\x1f/\\]+$`)

type Artifact struct {
	Path  string
	Label string
}

func (a Artifact) Valid() error {
	if a.Path == "" {
		return fmt.Errorf("%w: it names no file", ErrArtifactInvalid)
	}

	if !artifactLabel.MatchString(a.Label) {
		return fmt.Errorf(
			"%w: %q is not a label a person can read; give it a short name with no slashes",
			ErrArtifactInvalid, a.Label,
		)
	}

	if len(a.Label) > ArtifactLabelMax {
		return fmt.Errorf(
			"%w: that label is %d characters and norn shows %d",
			ErrArtifactInvalid, len(a.Label), ArtifactLabelMax,
		)
	}

	return nil
}

func ArtifactLabelFor(path string) string {
	return filepath.Base(path)
}

type ArtifactReceipt struct {
	ID    string
	Label string
	Bytes int64
}
