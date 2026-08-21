package servicemanager

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const (
	unitMode = 0o644

	goBuildMarker = "/go-build"
)

type Command struct {
	Args     []string
	Optional bool
}

type Plan struct {
	Label    string
	Path     string
	Content  []byte
	Activate []Command
	Remove   []Command
	Manager  string
}

type Manager struct {
	dir   *statedir.Dir
	state config.State
}

func New(dir *statedir.Dir, state config.State) *Manager {
	return &Manager{dir: dir, state: state}
}

func (m *Manager) Plan() (Plan, error) {
	binary, err := binaryPath()
	if err != nil {
		return Plan{}, err
	}

	environment, err := m.environment()
	if err != nil {
		return Plan{}, err
	}

	return plan(binary, environment)
}

func (m *Manager) Install(ctx context.Context) (Plan, error) {
	prepared, err := m.Plan()
	if err != nil {
		return Plan{}, err
	}

	if err := prepared.Write(); err != nil {
		return Plan{}, err
	}

	if err := available(prepared.Manager); err != nil {
		return prepared, err
	}

	for _, command := range prepared.Activate {
		if err := run(ctx, command); err != nil && !command.Optional {
			return prepared, err
		}
	}

	return prepared, nil
}

func (m *Manager) Uninstall(ctx context.Context) (Plan, error) {
	prepared, err := m.Plan()
	if err != nil {
		return Plan{}, err
	}

	if err := available(prepared.Manager); err == nil {
		for _, command := range prepared.Remove {
			_ = run(ctx, command)
		}
	}

	if err := os.Remove(prepared.Path); err != nil && !os.IsNotExist(err) {
		return prepared, fmt.Errorf("remove %s: %w", prepared.Path, err)
	}

	return prepared, nil
}

func (m *Manager) Installed() (bool, error) {
	prepared, err := m.Plan()
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(prepared.Path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("stat %s: %w", prepared.Path, err)
	}

	return true, nil
}

func (m *Manager) environment() (map[string]string, error) {
	standard, err := config.DefaultStateRoot()
	if err != nil {
		return nil, err
	}

	if m.dir.Root() == standard {
		return nil, nil
	}

	return map[string]string{config.StateRootEnv: m.dir.Root()}, nil
}

func (p Plan) Write() error {
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p.Path), err)
	}

	if err := os.WriteFile(p.Path, p.Content, unitMode); err != nil {
		return fmt.Errorf("write %s: %w", p.Path, err)
	}

	return nil
}

func binaryPath() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find this binary: %w", err)
	}

	if strings.Contains(binary, goBuildMarker) || strings.HasPrefix(binary, os.TempDir()) {
		return "", fmt.Errorf(
			"this binary is running from %s, which is a temporary build directory that will be "+
				"deleted; build and install the runner first, then register the service from "+
				"where it will live",
			binary,
		)
	}

	return binary, nil
}

func available(manager string) error {
	if _, err := exec.LookPath(manager); err != nil {
		return fmt.Errorf(
			"%s is not on this system, so the unit file was written but nothing was registered; "+
				"run 'norn runner start' yourself, or wire the file into whatever supervises "+
				"services here",
			manager,
		)
	}

	return nil
}

func run(ctx context.Context, command Command) error {
	output, err := exec.CommandContext(ctx, command.Args[0], command.Args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"%s: %w: %s", strings.Join(command.Args, " "), err, strings.TrimSpace(string(output)),
		)
	}

	return nil
}

func render(text string, data any) ([]byte, error) {
	parsed, err := template.New("unit").Funcs(template.FuncMap{"xml": escapeXML}).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parse service template: %w", err)
	}

	var out bytes.Buffer

	if err := parsed.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render service template: %w", err)
	}

	return out.Bytes(), nil
}

func escapeXML(value string) string {
	var out bytes.Buffer

	if err := xml.EscapeText(&out, []byte(value)); err != nil {
		return ""
	}

	return out.String()
}
