package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedQuestion struct {
	Version       int           `json:"version"`
	Ref           string        `json:"ref"`
	Kind          string        `json:"kind"`
	Blocking      bool          `json:"blocking"`
	Message       string        `json:"message"`
	Options       []string      `json:"options,omitempty"`
	AllowFreeText bool          `json:"allowFreeText,omitempty"`
	Default       string        `json:"default,omitempty"`
	Wait          time.Duration `json:"wait,omitempty"`
	Preview       string        `json:"preview,omitempty"`
	Files         []string      `json:"files,omitempty"`
	Artifacts     []string      `json:"artifacts,omitempty"`
	Asked         time.Time     `json:"asked"`
	Answer        *storedAnswer `json:"answer,omitempty"`
}

type storedAnswer struct {
	QuestionID string    `json:"questionId,omitempty"`
	Ref        string    `json:"ref,omitempty"`
	Question   string    `json:"question,omitempty"`
	Answer     string    `json:"answer"`
	AnsweredBy string    `json:"answeredBy,omitempty"`
	AnsweredAt time.Time `json:"answeredAt,omitzero"`
}

func (r *fileRun) questionPath(name string) string {
	return filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.RunQuestionFile)
}

func (r *fileRun) SaveQuestion(_ context.Context, name string, open entity.OpenQuestion) error {
	path := r.questionPath(name)

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	asked := open.Question
	stored := storedQuestion{
		Version:       version,
		Ref:           asked.Ref,
		Kind:          string(asked.Kind),
		Blocking:      asked.Blocking,
		Message:       asked.Message,
		Options:       asked.Options,
		AllowFreeText: asked.AllowFreeText,
		Default:       asked.Default,
		Wait:          asked.Wait,
		Preview:       asked.Context.Preview,
		Files:         asked.Context.Files,
		Artifacts:     asked.Context.Artifacts,
		Asked:         asked.Asked,
	}

	if open.Answer != nil {
		answer := storedAnswer(*open.Answer)
		stored.Answer = &answer
	}

	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("write the question %s is waiting on: %w", name, err)
	}

	return statedir.WriteSecret(path, append(raw, '\n'))
}

func (r *fileRun) LoadQuestion(_ context.Context, name string) (entity.OpenQuestion, error) {
	var stored storedQuestion

	if err := readInto(r.questionPath(name), &stored); err != nil {
		return entity.OpenQuestion{}, err
	}

	open := entity.OpenQuestion{Question: entity.Question{
		Ref:           stored.Ref,
		Kind:          entity.QuestionKind(stored.Kind),
		Blocking:      stored.Blocking,
		Message:       stored.Message,
		Options:       stored.Options,
		AllowFreeText: stored.AllowFreeText,
		Default:       stored.Default,
		Wait:          stored.Wait,
		Context: entity.QuestionContext{
			Preview:   stored.Preview,
			Files:     stored.Files,
			Artifacts: stored.Artifacts,
		},
		Asked: stored.Asked,
	}}

	if stored.Answer != nil {
		answer := entity.Answer(*stored.Answer)
		open.Answer = &answer
	}

	return open, nil
}

func (r *fileRun) ClearQuestion(_ context.Context, name string) error {
	if err := os.Remove(r.questionPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear the question %s was waiting on: %w", name, err)
	}

	return nil
}
