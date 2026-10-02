package toolchain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const (
	commandNotFound = 127
	scratchMode     = 0o700
)

type toolchainsService struct {
	toolchain   repository.Toolchain
	sandboxes   repository.Sandbox
	inventories repository.Inventory
	runs        repository.Run
	forges      repository.Forge
	host        config.Host
	results     config.Results
}

func New(
	toolchain repository.Toolchain,
	sandboxes repository.Sandbox,
	inventories repository.Inventory,
	runs repository.Run,
	forges repository.Forge,
	host config.Host,
	results config.Results,
) service.Toolchains {
	return &toolchainsService{
		toolchain:   toolchain,
		sandboxes:   sandboxes,
		inventories: inventories,
		runs:        runs,
		forges:      forges,
		host:        host,
		results:     results,
	}
}

func (s *toolchainsService) Check(
	ctx context.Context,
	probe entity.ToolchainProbe,
) (entity.ToolchainReport, error) {
	manifests, err := s.toolchain.Manifests(ctx, probe.Roots)
	if err != nil {
		return nil, err
	}

	required := entity.Requirements(manifests)
	report := make(entity.ToolchainReport, 0, len(required))

	for _, requirement := range required {
		report = append(report, s.check(ctx, probe, requirement))
	}

	return report, nil
}

func (s *toolchainsService) check(
	ctx context.Context,
	probe entity.ToolchainProbe,
	requirement entity.Requirement,
) entity.ToolCheck {
	check := entity.ToolCheck{Requirement: requirement}

	if probe.Box.Runtime != entity.RuntimeDocker {
		path, found := s.toolchain.Locate(requirement.Tool.Version[0])
		if !found {
			check.State = entity.ToolMissing

			return check
		}

		check.Path = path

		if entity.InstalledAsSnap(path) {
			check.State = entity.ToolSnap

			return check
		}
	}

	var said bytes.Buffer

	code, err := s.sandboxes.Run(ctx, probe.Box, repository.Launch{
		Dir:         probe.Workdir,
		Command:     requirement.Tool.Version,
		Environment: probe.Environment,
		Output:      &said,
		Errors:      &said,
	}, s.host.Timeout)

	switch {
	case err == nil && code == 0:
		check.State, check.Version = entity.ToolReady, entity.ToolVersion(said.String())
	case err == nil && code == commandNotFound:
		check.State = entity.ToolMissing
	case err != nil:
		check.State, check.Detail = entity.ToolBroken, err.Error()
	default:
		check.State, check.Detail = entity.ToolBroken, entity.ToolVersion(said.String())
	}

	return check
}

func (s *toolchainsService) Doctor(ctx context.Context) (entity.Doctor, error) {
	doctor := entity.Doctor{}

	if err := s.sandboxes.Check(ctx, entity.RuntimeProcess); err != nil {
		doctor.Sandbox = err.Error()
	}

	doctor.Identity, _ = entity.FirstCompleteIdentity(
		entity.GitIdentity{Name: s.results.CommitName, Email: s.results.CommitEmail},
		s.runs.HostIdentity(ctx),
	)

	codebases, err := s.inventories.List(ctx)
	if err != nil {
		return doctor, err
	}

	for _, codebase := range codebases {
		if doctor.PullRequests == "" {
			doctor.PullRequests, _ = s.forges.Available(ctx, codebase.RootPath)
		}

		doctor.Codebases = append(doctor.Codebases, s.examine(ctx, codebase))
	}

	return doctor, nil
}

func (s *toolchainsService) examine(ctx context.Context, codebase entity.Codebase) entity.DoctorCodebase {
	examined := entity.DoctorCodebase{Name: codebase.Name, Root: codebase.RootPath}

	scratch, err := os.MkdirTemp("", "norn-doctor-")
	if err != nil {
		examined.Failure = err.Error()

		return examined
	}

	defer func() { _ = os.RemoveAll(scratch) }()

	home := entity.RunHomeOf(scratch)

	for _, dir := range []string{home.Root, home.Tmp} {
		if err := os.MkdirAll(dir, scratchMode); err != nil {
			examined.Failure = err.Error()

			return examined
		}
	}

	box := entity.Sandbox{Run: filepath.Base(scratch), Runtime: entity.RuntimeProcess}

	if err := s.sandboxes.Open(ctx, entity.DoctorSpec(box, codebase.RootPath, home)); err != nil {
		examined.Failure = err.Error()

		return examined
	}

	defer func() { _ = s.sandboxes.Close(context.WithoutCancel(ctx), box) }()

	roots := make([]entity.ManifestRoot, 0, len(codebase.Confirmed.Listed()))
	for _, held := range codebase.Confirmed.Listed() {
		roots = append(roots, entity.ManifestRoot{
			RelPath: held.RelPath,
			Path:    filepath.Join(codebase.RootPath, filepath.FromSlash(held.RelPath)),
		})
	}

	hostHome, _ := os.UserHomeDir()

	report, err := s.Check(ctx, entity.ToolchainProbe{
		Box:     box,
		Workdir: codebase.RootPath,
		Environment: entity.TaskEnvironment(
			entity.RuntimeProcess, os.Environ(), hostHome, home, exists,
		),
		Roots: roots,
	})
	if err != nil {
		examined.Failure = err.Error()
	}

	examined.Report = report

	return examined
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
