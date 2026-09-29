package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func planned(t *testing.T, h *harness, id, session, body string) script {
	t.Helper()

	written := finishes(session, "wrote the plan")
	written.during = func() {
		plans := entity.RunHomeOf(h.dir.Run(id)).Plans()

		if err := os.MkdirAll(plans, 0o700); err != nil {
			t.Error(err)
		}

		name := filepath.Join(plans, "plan-"+time.Now().Format("150405.000000000")+entity.PlanFileExt)
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Error(err)
		}
	}

	return written
}

func planning(t *testing.T, h *harness, id string) {
	t.Helper()

	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer(id)); err != nil {
		t.Fatalf("offer: %v", err)
	}

	start := started()
	start.Stage = channelv1.StagePlanning

	if err := h.service.Start(ctx, id, start); err != nil {
		t.Fatalf("start: %v", err)
	}
}

func (h *harness) proposals(t *testing.T) []channelv1.Plan {
	t.Helper()

	sent := h.sentOf(t, channelv1.PlanProposed)
	plans := make([]channelv1.Plan, 0, len(sent))

	for _, message := range sent {
		plans = append(plans, decodeInto[channelv1.Plan](t, message))
	}

	return plans
}

func TestANewRunPlansFirstAndWaitsForAPersonToApproveThePlan(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{planned(t, h, "exec-01ABC", "session-01", "1. Add the median helper.")}

	stop := h.start(t)
	defer stop()

	planning(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateAwaitingPlan)

	proposals := h.proposals(t)
	if len(proposals) != 1 || proposals[0].Body != "1. Add the median helper." || proposals[0].Ref == "" {
		t.Fatalf("norn was sent %+v, want the plan the agent wrote", proposals)
	}

	worked := h.drivers.worked()
	if len(worked) != 1 || !worked[0].Planning() || worked[0].Plans == "" {
		t.Fatalf(
			"the agent started as %+v; a planning session has to run with implementation "+
				"disabled and its plan written where the machine can read it",
			worked,
		)
	}

	if prompt := h.drivers.began()[0].Prompt; !strings.Contains(prompt, "How to plan here") {
		t.Fatalf("the agent was told to work rather than to plan:\n%s", prompt)
	}

	if collected := h.sentOf(t, channelv1.ChangeSetUpdated); len(collected) != 0 {
		t.Fatal("a planning run collected changes; nothing may be built before the plan is approved")
	}
}

func TestAPlanningSessionThatWroteNoPlanStopsTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{finishes("session-01", "thought about it")}

	stop := h.start(t)
	defer stop()

	planning(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateFailed)

	if proposals := h.proposals(t); len(proposals) != 0 {
		t.Fatalf("a run with no plan proposed %+v", proposals)
	}
}

func TestAnApprovedPlanIsBuiltInTheSameSessionThatWroteIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{
		planned(t, h, "exec-01ABC", "session-01", "1. Add the median helper."),
		finishes("session-01", "built the plan"),
	}

	stop := h.start(t)
	defer stop()

	planning(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateAwaitingPlan)

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason:      channelv1.ResumePlanApproved,
		Stage:       channelv1.StageImplementation,
		Instruction: "1. Add the median helper.",
	}); err != nil {
		t.Fatalf("approve the plan: %v", err)
	}

	h.awaitReview(t, "exec-01ABC")

	carried := h.drivers.carried()
	if len(carried) != 1 || carried[0].ID != "session-01" {
		t.Fatalf(
			"the plan was built in %+v rather than session-01; the agent would lose everything "+
				"it learned while planning",
			carried,
		)
	}

	if env := h.drivers.carriedIn(); len(env) != 1 || env[0].Planning() {
		t.Fatal("the approved plan was built with implementation still disabled")
	}

	said := h.drivers.injections()[0]
	if !strings.Contains(said, "1. Add the median helper.") || !strings.Contains(said, "How to work here") {
		t.Fatalf("the agent was told %q, want the approved plan and how to work", said)
	}
}

func TestARevisedPlanIsWrittenInTheSameSessionWithoutBuildingAnything(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{
		planned(t, h, "exec-01ABC", "session-01", "Drop the table."),
		planned(t, h, "exec-01ABC", "session-01", "Archive the table."),
	}

	stop := h.start(t)
	defer stop()

	planning(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateAwaitingPlan)

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason:      channelv1.ResumePlanRevision,
		Stage:       channelv1.StagePlanning,
		Instruction: "Keep the data.",
	}); err != nil {
		t.Fatalf("ask for a revision: %v", err)
	}

	h.await(t, "waited for the revised plan", func() bool { return len(h.proposals(t)) == 2 })

	if proposals := h.proposals(t); proposals[1].Body != "Archive the table." ||
		proposals[1].Ref == proposals[0].Ref {
		t.Fatalf("the revision went out as %+v", proposals)
	}

	if env := h.drivers.carriedIn(); len(env) != 1 || !env[0].Planning() {
		t.Fatal("a revision request let the agent start implementing")
	}

	if said := h.drivers.injections()[0]; !strings.Contains(said, "Keep the data.") {
		t.Fatalf("the agent was told %q rather than what to change", said)
	}
}

func TestAQuestionWhilePlanningParksAndTheAnswerResumesPlanning(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{
		asking(t, h, "exec-01ABC"),
		planned(t, h, "exec-01ABC", "session-01", "Remove the endpoint."),
	}

	stop := h.start(t)
	defer stop()

	planning(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateWaitingForInput)

	if proposals := h.proposals(t); len(proposals) != 0 {
		t.Fatal("a run that stopped to ask proposed a plan it had not finished")
	}

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer,
		Stage:  channelv1.StagePlanning,
		Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove it", AnsweredBy: "Rae",
		}},
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateAwaitingPlan)

	if env := h.drivers.carriedIn(); len(env) != 1 || !env[0].Planning() {
		t.Fatal("an answer while planning let the agent start implementing; only approving the plan may")
	}

	said := h.drivers.injections()[0]
	if !strings.Contains(said, "Keep the old endpoint?") || !strings.Contains(said, "Remove it") {
		t.Fatalf("the agent was told %q, want the question and its answer", said)
	}
}
