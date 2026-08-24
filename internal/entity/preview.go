package entity

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const PreviewsMax = 8

var (
	ErrPreviewUnknown  = errors.New("this run has no preview by that name")
	ErrPreviewInvalid  = errors.New("that preview cannot be opened")
	ErrPreviewCrowded  = errors.New("this run already holds as many previews as it may")
	ErrPreviewNotOwned = errors.New(
		"a preview only goes on a service norn is running for this run",
	)
)

type Preview struct {
	Name      string
	Service   string
	Path      string
	Port      int
	URL       string
	Shared    string
	ExposedAt time.Time
}

func (p Preview) Valid() error {
	if !serviceName.MatchString(p.Name) {
		return fmt.Errorf(
			"%w: %q is not a name a preview can have; use lower-case letters, digits, "+
				"dashes and underscores",
			ErrPreviewInvalid, p.Name,
		)
	}

	if !serviceName.MatchString(p.Service) {
		return fmt.Errorf(
			"%w: %s names %q, which is not a name a service can have",
			ErrPreviewInvalid, p.Name, p.Service,
		)
	}

	if p.Path != "" && !strings.HasPrefix(p.Path, "/") {
		return fmt.Errorf(
			"%w: %s opens at %q, which is not a path", ErrPreviewInvalid, p.Name, p.Path,
		)
	}

	if strings.Contains(p.Path, "..") {
		return fmt.Errorf(
			"%w: %s opens at %s, which leaves the service", ErrPreviewInvalid, p.Name, p.Path,
		)
	}

	return nil
}

func PreviewURL(port int, path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + path
}
