package capability

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	kvmDevice = "/dev/kvm"

	gatewayHealthPath = "/__norn/healthz"
	probeLimit        = 4 << 10
)

var version = regexp.MustCompile(`\d+(\.\d+)+(-[0-9A-Za-z.]+)?`)

func codingTools() []string {
	return []string{"claude", "codex"}
}

type hostCapability struct {
	cfg config.Codebase
}

func New(cfg config.Codebase) repository.Capability {
	return &hostCapability{cfg: cfg}
}

func (r *hostCapability) Detect(ctx context.Context, gateway string) repository.Capabilities {
	return repository.Capabilities{
		Runtimes: r.runtimes(ctx),
		Tools:    r.tools(ctx),
		Gateway:  r.reaches(ctx, gateway),
	}
}

func (r *hostCapability) reaches(ctx context.Context, gateway string) entity.GatewayReach {
	if gateway == "" {
		return entity.GatewayUnconfigured
	}

	probing, settled := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
	defer settled()

	request, err := http.NewRequestWithContext(
		probing, http.MethodGet, strings.TrimSuffix(gateway, "/")+gatewayHealthPath, nil,
	)
	if err != nil {
		return entity.GatewayUnreachable
	}

	response, err := (&http.Client{Timeout: r.cfg.ProbeTimeout}).Do(request)
	if err != nil {
		return entity.GatewayUnreachable
	}

	defer func() { _ = response.Body.Close() }()

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, probeLimit))

	return entity.GatewayReachable
}

func (r *hostCapability) runtimes(ctx context.Context) []entity.Runtime {
	runtimes := []entity.Runtime{entity.RuntimeProcess}

	if _, ok := r.ask(ctx, "docker", "info", "--format", "{{.ServerVersion}}"); ok {
		runtimes = append(runtimes, entity.RuntimeDocker)
	}

	if _, err := os.Stat(kvmDevice); err == nil {
		runtimes = append(runtimes, entity.RuntimeKVM)
	}

	return runtimes
}

func (r *hostCapability) tools(ctx context.Context) []entity.Tool {
	tools := make([]entity.Tool, 0, len(codingTools()))

	for _, name := range codingTools() {
		reported, ok := r.ask(ctx, name, "--version")
		if !ok {
			continue
		}

		tools = append(tools, entity.Tool{Name: name, Version: versionIn(reported)})
	}

	return tools
}

func versionIn(reported string) string {
	found := version.FindString(reported)
	if found == "" {
		return truncate(reported, entity.ToolVersionMaxLen)
	}

	return truncate(found, entity.ToolVersionMaxLen)
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}

	return string(runes[:max])
}

func (r *hostCapability) ask(ctx context.Context, name string, args ...string) (string, bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}

	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")

	return strings.TrimSpace(first), true
}
