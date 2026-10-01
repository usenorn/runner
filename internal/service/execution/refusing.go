package execution

import (
	"context"
	"encoding/json"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) Refused(ctx context.Context, refused channelv1.Message, reason string) error {
	if refused.ExecutionID == "" {
		return nil
	}

	if err := s.note(ctx, refused.ExecutionID, channelv1.EventNote, entity.RefusalNote(refused.Type, reason)); err != nil {
		return err
	}

	if refused.Type != channelv1.QuestionAsked {
		return nil
	}

	var asked channelv1.Question

	if err := json.Unmarshal(refused.Payload, &asked); err != nil {
		return nil
	}

	if err := s.questions.Answered(ctx, refused.ExecutionID, entity.RefusedQuestionAnswer(asked.Ref, reason)); err != nil {
		return err
	}

	return s.Continue(ctx, refused.ExecutionID, channelv1.Instruction{
		Reason:      entity.ResumeRefused,
		Instruction: entity.RefusedQuestionInjection(asked.Message, reason),
	})
}
