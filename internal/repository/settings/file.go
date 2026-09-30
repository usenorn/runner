package settings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type fileSettings struct{}

func New() repository.Settings {
	return &fileSettings{}
}

func (r *fileSettings) Load(
	_ context.Context,
	root string,
) (repository.CodebaseSettings, error) {
	path := filepath.Join(root, entity.SettingsDir, entity.SettingsFile)

	if _, err := os.Stat(path); err != nil {
		return repository.CodebaseSettings{}, nil
	}

	held := viper.New()
	held.SetConfigFile(path)

	if err := held.ReadInConfig(); err != nil {
		return repository.CodebaseSettings{}, fmt.Errorf(
			"read the settings this folder keeps in %s: %w", path, err,
		)
	}

	settings := repository.CodebaseSettings{
		GitMode:      held.GetString("snapshot.git_mode"),
		Base:         held.GetString("snapshot.base"),
		LocalChanges: held.GetString("snapshot.local_changes"),
	}

	if held.IsSet("snapshot.fetch") {
		fetch := held.GetBool("snapshot.fetch")
		settings.Fetch = &fetch
	}

	return settings, nil
}

func (r *fileSettings) Plan(_ context.Context, root string) (string, error) {
	path := filepath.Join(root, entity.SettingsDir, entity.PlanFile)

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return path, nil
}

func (r *fileSettings) Ignores(_ context.Context, dir string) ([]entity.IgnoreRule, error) {
	raw, err := os.ReadFile(filepath.Join(dir, entity.IgnoreFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", filepath.Join(dir, entity.IgnoreFileName), err)
	}

	return entity.ParseIgnore(string(raw)), nil
}

type planFile struct {
	Services map[string]planService `yaml:"services"`
	Previews []planPreview          `yaml:"previews"`
}

type planService struct {
	Kind        string            `yaml:"kind"`
	Cwd         string            `yaml:"cwd"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	Requires    []string          `yaml:"requires"`
	Health      planHealth        `yaml:"health"`
}

type planHealth struct {
	TCP  string          `yaml:"tcp"`
	HTTP *planHTTPHealth `yaml:"http"`
	Log  string          `yaml:"log"`
}

type planHTTPHealth struct {
	Path string `yaml:"path"`
	Port string `yaml:"port"`
}

type planPreview struct {
	Name    string `yaml:"name"`
	Service string `yaml:"service"`
	Path    string `yaml:"path"`
}

func (r *fileSettings) Definition(_ context.Context, path string) (entity.PlanDefinition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return entity.PlanDefinition{}, fmt.Errorf("read %s: %w", path, err)
	}

	var plan planFile

	if err := yaml.Unmarshal(raw, &plan); err != nil {
		return entity.PlanDefinition{}, fmt.Errorf("%w: %s: %w", entity.ErrPlanInvalid, path, err)
	}

	definition := entity.PlanDefinition{
		Services: make([]entity.PlanService, 0, len(plan.Services)),
		Previews: make([]entity.PlanPreview, 0, len(plan.Previews)),
	}

	for _, name := range slices.Sorted(maps.Keys(plan.Services)) {
		definition.Services = append(definition.Services, serviceOf(name, plan.Services[name]))
	}

	for _, preview := range plan.Previews {
		definition.Previews = append(definition.Previews, entity.PlanPreview(preview))
	}

	return definition, nil
}

func serviceOf(name string, declared planService) entity.PlanService {
	kind := entity.PlanServiceKind(declared.Kind)
	if kind == "" {
		kind = entity.PlanServiceProcess
	}

	return entity.PlanService{
		Kind: kind,
		Service: entity.Service{
			Name:        name,
			Dir:         declared.Cwd,
			Command:     declared.Command,
			Environment: declared.Environment,
			Requires:    declared.Requires,
			Health:      healthOf(declared.Health),
		},
	}
}

func healthOf(declared planHealth) entity.Health {
	switch {
	case declared.HTTP != nil:
		return entity.Health{Kind: entity.HealthHTTP, Path: declared.HTTP.Path, Port: declared.HTTP.Port}
	case declared.TCP != "":
		return entity.Health{Kind: entity.HealthTCP, Port: declared.TCP}
	case declared.Log != "":
		return entity.Health{Kind: entity.HealthLog, Pattern: declared.Log}
	default:
		return entity.Health{}
	}
}
