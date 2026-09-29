package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedResume struct {
	Version     int    `json:"version"`
	Reason      string `json:"reason"`
	Instruction string `json:"instruction,omitempty"`
	QuestionID  string `json:"questionId,omitempty"`
	QuestionRef string `json:"questionRef,omitempty"`
}

func (r *fileRun) resumePath(name string) string {
	return filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.RunResumeFile)
}

func (r *fileRun) SaveResume(_ context.Context, name string, instruction channelv1.Instruction) error {
	path := r.resumePath(name)

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	raw, err := json.MarshalIndent(storedResume{
		Version:     version,
		Reason:      instruction.Reason,
		Instruction: instruction.Instruction,
		QuestionID:  instruction.QuestionID,
		QuestionRef: instruction.QuestionRef,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("write how %s is to carry on: %w", name, err)
	}

	return statedir.WriteSecret(path, append(raw, '\n'))
}

func (r *fileRun) LoadResume(_ context.Context, name string) (channelv1.Instruction, error) {
	var stored storedResume

	if err := readInto(r.resumePath(name), &stored); err != nil {
		return channelv1.Instruction{}, err
	}

	return channelv1.Instruction{
		Reason:      stored.Reason,
		Instruction: stored.Instruction,
		QuestionID:  stored.QuestionID,
		QuestionRef: stored.QuestionRef,
	}, nil
}

func (r *fileRun) ClearResume(_ context.Context, name string) error {
	if err := os.Remove(r.resumePath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear how %s was to carry on: %w", name, err)
	}

	return nil
}
