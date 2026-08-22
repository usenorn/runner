package supervisor

import (
	"bytes"
	"io"
	"sync"

	"github.com/usenorn/runner/internal/entity"
)

const watching = 256

type stream struct {
	sink io.WriteCloser

	mu      sync.Mutex
	pending []byte
	ring    []string
	readers []chan string
}

func newStream(sink io.WriteCloser) *stream {
	return &stream{sink: sink, ring: []string{}, readers: []chan string{}}
}

func (s *stream) Write(raw []byte) (int, error) {
	s.mu.Lock()

	s.pending = append(s.pending, raw...)

	for {
		at := bytes.IndexByte(s.pending, '\n')
		if at < 0 {
			break
		}

		s.keep(string(bytes.TrimRight(s.pending[:at], "\r")))

		s.pending = s.pending[at+1:]
	}

	s.mu.Unlock()

	if s.sink == nil {
		return len(raw), nil
	}

	return s.sink.Write(raw)
}

func (s *stream) keep(line string) {
	s.ring = append(s.ring, line)

	if len(s.ring) > entity.ServiceRing {
		s.ring = s.ring[1:]
	}

	for _, reader := range s.readers {
		select {
		case reader <- line:
		default:
		}
	}
}

func (s *stream) Recent(lines int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lines > 0 && len(s.ring) > lines {
		return append([]string(nil), s.ring[len(s.ring)-lines:]...)
	}

	return append([]string(nil), s.ring...)
}

func (s *stream) Watch() (<-chan string, func()) {
	reader := make(chan string, watching)

	s.mu.Lock()
	s.readers = append(s.readers, reader)
	s.mu.Unlock()

	return reader, func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		for at, held := range s.readers {
			if held == reader {
				s.readers = append(s.readers[:at], s.readers[at+1:]...)

				return
			}
		}
	}
}

func (s *stream) Close() error {
	s.mu.Lock()

	if len(s.pending) > 0 {
		s.keep(string(s.pending))

		s.pending = nil
	}

	s.readers = nil

	s.mu.Unlock()

	if s.sink == nil {
		return nil
	}

	return s.sink.Close()
}
