package run_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestAnOpenQuestionAndItsAnswerReadBackAsTheyWereWritten(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01QST"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	asked := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	open := entity.OpenQuestion{
		Question: entity.Question{
			Ref: "01QREF", Kind: entity.QuestionApproval, Blocking: true,
			Message: "Ship the plan as written?", Options: []string{"yes", "no"},
			Default: "yes", Wait: time.Minute,
			Context: entity.QuestionContext{Files: []string{"PLAN.md"}}, Asked: asked,
		},
		Answer: &entity.Answer{
			QuestionID: "q-1", Ref: "01QREF", Answer: "yes", AnsweredBy: "Vlad",
			AnsweredAt: asked.Add(time.Hour),
		},
	}

	if err := runs.SaveQuestion(ctx, "exec-01QST", open); err != nil {
		t.Fatalf("save the question: %v", err)
	}

	read, err := runs.LoadQuestion(ctx, "exec-01QST")
	if err != nil {
		t.Fatalf("load the question: %v", err)
	}

	if !reflect.DeepEqual(read, open) {
		t.Fatalf(
			"read back %+v, want %+v; a machine restarting under a run waiting on somebody "+
				"has to know exactly what it asked",
			read, open,
		)
	}

	if err := runs.ClearQuestion(ctx, "exec-01QST"); err != nil {
		t.Fatalf("clear the question: %v", err)
	}

	if _, err := runs.LoadQuestion(ctx, "exec-01QST"); !errors.Is(err, entity.ErrSnapshotMissing) {
		t.Fatalf("err = %v after clearing, want nothing written down", err)
	}

	if err := runs.ClearQuestion(ctx, "exec-01QST"); err != nil {
		t.Fatalf("clearing a question twice failed: %v", err)
	}
}
