package mcpserver

import (
	"errors"

	"github.com/usenorn/runner/internal/entity"
)

var errNoExecution = errors.New(
	"this server runs inside one execution and nothing says which; set " +
		entity.ExecutionVariable + " or pass --exec",
)

func toolFailure(err error) error {
	if errors.Is(err, entity.ErrDaemonUnavailable) {
		return errors.New(
			"the norn daemon on this machine is not answering, so nothing outside your own " +
				"editing is reachable right now",
		)
	}

	return errors.New(err.Error())
}
