package tunnel

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

func (s *tunnelsService) serve(ctx context.Context, raw net.Conn) {
	stream := channelv1.NewStream(raw)

	defer func() { _ = stream.Close() }()

	var open channelv1.StreamOpen

	if err := stream.ReadFrame(&open); err != nil {
		return
	}

	if !s.take() {
		_ = stream.WriteFrame(channelv1.StreamReady{Reason: entity.ErrTunnelCrowded.Error()})

		return
	}

	defer s.give()

	local, err := s.reach(ctx, open)
	if err != nil {
		logging.From(ctx).InfoContext(
			ctx,
			"this machine refused a preview stream norn's gateway opened",
			slog.String("execution_id", open.Execution),
			slog.String("preview", open.Preview),
			slog.String("reason", err.Error()),
		)

		_ = stream.WriteFrame(channelv1.StreamReady{Reason: err.Error()})

		return
	}

	service, err := net.Dial("tcp", local)
	if err != nil {
		_ = stream.WriteFrame(channelv1.StreamReady{Reason: entity.ErrPreviewUnknown.Error()})

		return
	}

	defer func() { _ = service.Close() }()

	if err := stream.WriteFrame(channelv1.StreamReady{Open: true}); err != nil {
		return
	}

	relay(ctx, stream, service)
}

func (s *tunnelsService) reach(
	ctx context.Context,
	open channelv1.StreamOpen,
) (string, error) {
	if open.Execution == "" || open.Preview == "" {
		return "", entity.ErrTunnelUnknownPair
	}

	preview, err := s.previews.Resolve(ctx, open.Execution, open.Preview)
	if err != nil {
		return "", entity.ErrTunnelUnknownPair
	}

	if preview.Port == 0 {
		return "", entity.ErrTunnelUnknownPair
	}

	return net.JoinHostPort("127.0.0.1", strconv.Itoa(preview.Port)), nil
}

func (s *tunnelsService) take() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams >= s.cfg.MaxStreams {
		return false
	}

	s.streams++

	return true
}

func (s *tunnelsService) give() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams > 0 {
		s.streams--
	}
}

func relay(ctx context.Context, stream, service net.Conn) {
	var both sync.WaitGroup

	both.Add(2)

	go func() {
		defer both.Done()

		_, _ = io.Copy(service, stream)

		if half, ok := service.(*net.TCPConn); ok {
			_ = half.CloseWrite()
		}
	}()

	go func() {
		defer both.Done()

		_, _ = io.Copy(stream, service)

		_ = stream.Close()
	}()

	settled := make(chan struct{})

	go func() {
		both.Wait()
		close(settled)
	}()

	select {
	case <-settled:
	case <-ctx.Done():
		_ = stream.Close()
		_ = service.Close()

		<-settled
	}
}
