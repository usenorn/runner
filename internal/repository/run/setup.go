package run

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedPermissions struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Chosen  string `json:"chosen"`
}

type storedPlan struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
	Path    string `json:"path,omitempty"`
}

type storedDriver struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Release   string `json:"release,omitempty"`
	Installed bool   `json:"installed"`
	Model     string `json:"model,omitempty"`
	Chosen    string `json:"chosen"`
}

type storedServices struct {
	Version int    `json:"version"`
	Runtime string `json:"runtime"`
	Chosen  string `json:"chosen"`
}

type storedEntry struct {
	Kind     string    `json:"kind"`
	State    string    `json:"state,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Occurred time.Time `json:"ts"`
}

func (r *fileRun) SaveSetup(_ context.Context, name string, setup entity.RunSetup) error {
	dir := filepath.Join(r.dir.Run(name), entity.RunMetadataDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	written := map[string]any{
		entity.RunPermissionsFile: storedPermissions{
			Version: version,
			Profile: string(setup.Permissions.Profile),
			Chosen:  setup.Permissions.Chosen,
		},
		entity.RunPlanFile: storedPlan{
			Version: version,
			Source:  string(setup.Plan.Source),
			Path:    setup.Plan.Path,
		},
		entity.RunDriverFile: storedDriver{
			Version:   version,
			Kind:      string(setup.Driver.Kind),
			Release:   setup.Driver.Version,
			Installed: setup.Driver.Installed,
			Model:     setup.Driver.Model,
			Chosen:    setup.Driver.Chosen,
		},
		entity.RunServicesFile: storedServices{
			Version: version,
			Runtime: string(setup.Services.Runtime),
			Chosen:  setup.Services.Chosen,
		},
	}

	for file, body := range written {
		raw, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return fmt.Errorf("write %s for %s: %w", file, name, err)
		}

		if err := statedir.WriteSecret(filepath.Join(dir, file), append(raw, '\n')); err != nil {
			return err
		}
	}

	return nil
}

func (r *fileRun) LoadSetup(_ context.Context, name string) (entity.RunSetup, error) {
	dir := filepath.Join(r.dir.Run(name), entity.RunMetadataDir)

	var (
		permissions storedPermissions
		plan        storedPlan
		driver      storedDriver
		services    storedServices
	)

	for file, into := range map[string]any{
		entity.RunPermissionsFile: &permissions,
		entity.RunPlanFile:        &plan,
		entity.RunDriverFile:      &driver,
		entity.RunServicesFile:    &services,
	} {
		if err := readInto(filepath.Join(dir, file), into); err != nil {
			return entity.RunSetup{}, err
		}
	}

	return entity.RunSetup{
		Permissions: entity.RunPermissions{
			Profile: entity.PermissionProfile(permissions.Profile),
			Chosen:  permissions.Chosen,
		},
		Plan: entity.RunPlan{
			Source: entity.PlanSource(plan.Source),
			Path:   plan.Path,
		},
		Driver: entity.RunDriver{
			Kind:      entity.DriverKind(driver.Kind),
			Version:   driver.Release,
			Installed: driver.Installed,
			Model:     driver.Model,
			Chosen:    driver.Chosen,
		},
		Services: entity.RunServices{
			Runtime: entity.Runtime(services.Runtime),
			Chosen:  services.Chosen,
		},
	}, nil
}

func readInto(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", entity.ErrSnapshotMissing, path)
		}

		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s cannot be read: %w", path, err)
	}

	return nil
}

func (r *fileRun) Append(_ context.Context, name string, entry entity.TimelineEntry) error {
	dir := filepath.Join(r.dir.Run(name), entity.RunLogsDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.Marshal(storedEntry{
		Kind:     string(entry.Kind),
		State:    string(entry.State),
		Reason:   entry.Reason,
		Occurred: entry.Occurred,
	})
	if err != nil {
		return fmt.Errorf("write a timeline entry for %s: %w", name, err)
	}

	path := filepath.Join(dir, entity.RunTimelineFile)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	if _, err := file.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return file.Sync()
}

func (r *fileRun) Timeline(_ context.Context, name string) ([]entity.TimelineEntry, error) {
	path := filepath.Join(r.dir.Run(name), entity.RunLogsDir, entity.RunTimelineFile)

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", entity.ErrExecutionUnknown, name)
		}

		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	entries := []entity.TimelineEntry{}
	lines := bufio.NewScanner(file)

	for lines.Scan() {
		var held storedEntry

		if err := json.Unmarshal(lines.Bytes(), &held); err != nil {
			continue
		}

		entries = append(entries, entity.TimelineEntry{
			Kind:     channelv1.EventKind(held.Kind),
			State:    entity.ExecutionState(held.State),
			Reason:   held.Reason,
			Occurred: held.Occurred,
		})
	}

	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return entries, nil
}
