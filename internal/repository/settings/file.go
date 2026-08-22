package settings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/viper"

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
