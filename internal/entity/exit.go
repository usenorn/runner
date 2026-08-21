package entity

import "errors"

const (
	ExitOK                = 0
	ExitFailure           = 1
	ExitDrainForced       = 3
	ExitDaemonUnavailable = 4
)

type ExitError struct {
	Code int
	Err  error
}

func Exit(code int, err error) error {
	return ExitError{Code: code, Err: err}
}

func (e ExitError) Error() string {
	return e.Err.Error()
}

func (e ExitError) Unwrap() error {
	return e.Err
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}

	var exit ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}

	return ExitFailure
}
