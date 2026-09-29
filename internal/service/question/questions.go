package question

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

// standing is one question a run is waiting on. answers is buffered by one so an answer that
// arrives just as the soft wait lapses is kept rather than dropped on the floor: the run needs it
// on the way back in.
type standing struct {
	question entity.Question
	answers  chan entity.Answer
	answer   *entity.Answer
}

type questionsService struct {
	runs  repository.Run
	spool repository.Spool
	cfg   config.Questions
	now   func() time.Time

	mu   sync.Mutex
	held map[string]*standing
}

func New(runs repository.Run, spool repository.Spool, cfg config.Questions) service.Questions {
	return &questionsService{
		runs:  runs,
		spool: spool,
		cfg:   cfg,
		now:   func() time.Time { return time.Now().UTC() },
		held:  map[string]*standing{},
	}
}

func (s *questionsService) Ask(
	ctx context.Context,
	executionID string,
	question entity.Question,
) (entity.Asked, error) {
	if err := question.Fault(); err != nil {
		return entity.Asked{}, err
	}

	if err := s.holding(ctx, executionID); err != nil {
		return entity.Asked{}, err
	}

	question.Ref = ulid.Make().String()
	question.Asked = s.now()

	waiting := &standing{question: question, answers: make(chan entity.Answer, 1)}

	if question.Blocking {
		if err := s.runs.SaveQuestion(ctx, executionID, entity.OpenQuestion{Question: question}); err != nil {
			return entity.Asked{}, err
		}

		s.mu.Lock()
		s.held[executionID] = waiting
		s.mu.Unlock()
	}

	if err := s.send(ctx, executionID, question); err != nil {
		return entity.Asked{}, errors.Join(err, s.drop(ctx, executionID, waiting))
	}

	if !question.Blocking {
		return entity.Asked{Outcome: entity.AskNoted, Ref: question.Ref, Advice: entity.AskNotedAdvice}, nil
	}

	return s.wait(ctx, executionID, waiting)
}

// holding refuses a question against a run this machine is not working on, so an agent given one
// execution's socket cannot put a question on another issue.
func (s *questionsService) holding(ctx context.Context, executionID string) error {
	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return fmt.Errorf("%w: %s", entity.ErrQuestionUnknownRun, executionID)
	}

	if execution.Finished() {
		return fmt.Errorf(
			"%w: %s has already finished as %s",
			entity.ErrQuestionUnknownRun, executionID, execution.State,
		)
	}

	return nil
}

func (s *questionsService) wait(
	ctx context.Context,
	executionID string,
	waiting *standing,
) (entity.Asked, error) {
	softly, lapsed := context.WithTimeout(ctx, s.softWait(waiting.question))
	defer lapsed()

	select {
	case answer := <-waiting.answers:
		if err := s.drop(ctx, executionID, waiting); err != nil {
			return entity.Asked{}, err
		}

		return entity.Asked{
			Outcome:    entity.AskAnswered,
			Ref:        waiting.question.Ref,
			Answer:     answer.Answer,
			AnsweredBy: answer.AnsweredBy,
		}, nil
	case <-softly.Done():
		return entity.Asked{
			Outcome: entity.AskPending,
			Ref:     waiting.question.Ref,
			Advice:  entity.AskPendingAdvice,
		}, nil
	}
}

func (s *questionsService) softWait(question entity.Question) time.Duration {
	if question.Wait > 0 && question.Wait <= s.cfg.MaxWait {
		return question.Wait
	}

	return s.cfg.SoftWait
}

func (s *questionsService) Answered(
	ctx context.Context,
	executionID string,
	answer entity.Answer,
) error {
	s.mu.Lock()
	waiting, held := s.held[executionID]

	// An answer to something this run has already moved past is not the one it stopped on, and
	// handing it over would resume the agent against a question it did not ask last.
	if held && answer.Ref != "" && answer.Ref != waiting.question.Ref {
		held = false
	}

	if held {
		waiting.answer = &answer
	}
	s.mu.Unlock()

	if held {
		if err := s.runs.SaveQuestion(ctx, executionID, entity.OpenQuestion{
			Question: waiting.question, Answer: &answer,
		}); err != nil {
			return err
		}
	}

	if !held {
		logging.From(ctx).InfoContext(
			ctx,
			"norn answered a question this run is no longer waiting on",
			slog.String("execution_id", executionID),
			slog.String("question_id", answer.QuestionID),
		)

		return nil
	}

	select {
	case waiting.answers <- answer:
	default:
	}

	return nil
}

func (s *questionsService) Waiting(executionID string) (entity.Question, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	waiting, held := s.held[executionID]
	if !held || waiting.answer != nil {
		return entity.Question{}, false
	}

	return waiting.question, true
}

func (s *questionsService) Take(
	ctx context.Context,
	executionID string,
) (entity.Question, entity.Answer, bool, error) {
	s.mu.Lock()
	waiting, held := s.held[executionID]

	if !held || waiting.answer == nil {
		s.mu.Unlock()

		return entity.Question{}, entity.Answer{}, false, nil
	}

	delete(s.held, executionID)
	s.mu.Unlock()

	if err := s.runs.ClearQuestion(ctx, executionID); err != nil {
		return entity.Question{}, entity.Answer{}, false, err
	}

	return waiting.question, *waiting.answer, true, nil
}

func (s *questionsService) Forget(ctx context.Context, executionID string) error {
	s.mu.Lock()
	delete(s.held, executionID)
	s.mu.Unlock()

	return s.runs.ClearQuestion(ctx, executionID)
}

func (s *questionsService) Restore(ctx context.Context, executionID string) error {
	open, err := s.runs.LoadQuestion(ctx, executionID)
	if errors.Is(err, entity.ErrSnapshotMissing) {
		return nil
	}

	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.held[executionID] = &standing{
		question: open.Question,
		answers:  make(chan entity.Answer, 1),
		answer:   open.Answer,
	}

	return nil
}

func (s *questionsService) drop(ctx context.Context, executionID string, waiting *standing) error {
	s.mu.Lock()
	owned := s.held[executionID] == waiting

	if owned {
		delete(s.held, executionID)
	}
	s.mu.Unlock()

	if !owned {
		return nil
	}

	return s.runs.ClearQuestion(ctx, executionID)
}

func (s *questionsService) send(
	ctx context.Context,
	executionID string,
	question entity.Question,
) error {
	body, err := json.Marshal(channelv1.Question{
		Ref:           question.Ref,
		Kind:          string(question.Kind),
		Blocking:      question.Blocking,
		Message:       question.Message,
		Options:       question.Options,
		AllowFreeText: question.AllowFreeText,
		Default:       question.Default,
		Context: channelv1.QuestionContext{
			Preview:   question.Context.Preview,
			Files:     question.Context.Files,
			Artifacts: question.Context.Artifacts,
		},
		Asked: question.Asked,
	})
	if err != nil {
		return fmt.Errorf("encode a question: %w", err)
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.QuestionAsked, executionID, body, s.now(),
	)
	if err != nil {
		return err
	}

	return s.spool.Append(ctx, message)
}
