package port

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type holder struct {
	run  string
	name string
}

type rangePort struct {
	lowest  int
	highest int

	mu     sync.Mutex
	held   map[holder]int
	cursor int
}

func New(runner config.Runner) repository.Port {
	return &rangePort{
		lowest:  runner.PortRange[0],
		highest: runner.PortRange[1],
		held:    map[holder]int{},
		cursor:  runner.PortRange[0],
	}
}

func (r *rangePort) Reserve(_ context.Context, run string, name string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if port, already := r.held[holder{run: run, name: name}]; already {
		return port, nil
	}

	taken := map[int]bool{}

	for _, port := range r.held {
		taken[port] = true
	}

	span := r.highest - r.lowest + 1

	for tried := 0; tried < span; tried++ {
		port := r.lowest + (r.cursor-r.lowest+tried)%span

		if taken[port] || !free(port) {
			continue
		}

		r.held[holder{run: run, name: name}] = port
		r.cursor = r.lowest + (port-r.lowest+1)%span

		return port, nil
	}

	return 0, fmt.Errorf(
		"%w: %d-%d is fully spoken for", entity.ErrPortsExhausted, r.lowest, r.highest,
	)
}

func (r *rangePort) Held(_ context.Context, run string) (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ports := map[string]int{}

	for who, port := range r.held {
		if who.run == run {
			ports[who.name] = port
		}
	}

	return ports, nil
}

func (r *rangePort) Release(_ context.Context, run string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for who := range r.held {
		if who.run == run {
			delete(r.held, who)
		}
	}
}

func free(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}

	_ = listener.Close()

	return true
}
