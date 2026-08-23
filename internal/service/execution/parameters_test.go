package execution_test

import (
	"context"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func (h *harness) asked(id string, params channelv1.Params) channelv1.Offer {
	offer := h.offer(id)
	params.Tool = offer.Params.Tool
	offer.Params = params

	return offer
}

func (h *harness) prepared(t *testing.T, offer channelv1.Offer) entity.RunSetup {
	t.Helper()

	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, offer); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, offer.ExecutionID, started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.awaitNote(t, "workspace for this run is ready")

	setup, err := h.runs.LoadSetup(ctx, offer.ExecutionID)
	if err != nil {
		t.Fatalf("read what the run was set up with: %v", err)
	}

	return setup
}

func TestARunIsSetUpUnderTheProfileTheDelegationAskedFor(t *testing.T) {
	h := newHarnessUnder(t, config.ProfileUnrestricted)

	setup := h.prepared(t, h.asked("exec-01ABC", channelv1.Params{
		Profile: channelv1.ProfileStrict,
	}))

	if setup.Permissions.Profile != entity.ProfileStrict {
		t.Fatalf(
			"the run came out under %q after the delegation asked for strict. Somebody "+
				"delegating a change they do not trust yet gets a run that can write anyway",
			setup.Permissions.Profile,
		)
	}

	if !strings.Contains(setup.Permissions.Chosen, "the delegation asked for it") {
		t.Fatalf("permissions.json does not say where the profile came from: %q", setup.Permissions.Chosen)
	}
}

func TestAMachineWillNotBeTalkedIntoMoreThanItsOwnProfileAllows(t *testing.T) {
	h := newHarnessUnder(t, config.ProfileStrict)

	setup := h.prepared(t, h.asked("exec-01ABC", channelv1.Params{
		Profile: channelv1.ProfileUnrestricted,
	}))

	if setup.Permissions.Profile != entity.ProfileStrict {
		t.Fatalf(
			"a delegation asking for unrestricted got %q on a machine set to strict. The "+
				"machine's setting is the ceiling, so whoever owns the laptop decides how far "+
				"a coding agent may go on it, not whoever delegated the issue",
			setup.Permissions.Profile,
		)
	}

	if !strings.Contains(setup.Permissions.Chosen, "goes no further than") {
		t.Fatalf(
			"permissions.json reads %q; a run that is more restricted than it was asked to be "+
				"has to say so, or the agent's refusals look like a bug",
			setup.Permissions.Chosen,
		)
	}
}

func TestADelegationThatNamesNoProfileLeavesTheMachineToDecide(t *testing.T) {
	h := newHarnessUnder(t, config.ProfileUnrestricted)

	setup := h.prepared(t, h.asked("exec-01ABC", channelv1.Params{}))

	if setup.Permissions.Profile != entity.ProfileUnrestricted {
		t.Fatalf(
			"a delegation that asked for nothing came out under %q rather than the machine's "+
				"own setting",
			setup.Permissions.Profile,
		)
	}
}

func TestWhatTheDelegationAskedAboutTheWorkingTreeReachesTheSnapshot(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.prepared(t, h.asked("exec-01ABC", channelv1.Params{
		BaseRef:      channelv1.BaseRefHead,
		IncludeDirty: true,
	}))

	requests := h.requests()

	if len(requests) != 1 {
		t.Fatalf("the folder was copied %d times", len(requests))
	}

	if requests[0].LocalChanges != entity.LocalChangesInclude {
		t.Fatalf(
			"the delegation asked to carry uncommitted work across and the snapshot was taken "+
				"with %q. The agent then works on a tree that is missing the change somebody "+
				"delegated the issue about",
			requests[0].LocalChanges,
		)
	}

	if requests[0].Base != entity.BaseHead {
		t.Fatalf(
			"the delegation asked to branch from head and the snapshot used %q; the run starts "+
				"from the default branch and the work it was meant to build on is not there",
			requests[0].Base,
		)
	}
}

func TestADelegationSilentAboutTheWorkingTreeLeavesTheMachinesOwnRulesAlone(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.prepared(t, h.asked("exec-01ABC", channelv1.Params{}))

	requests := h.requests()

	if len(requests) != 1 {
		t.Fatalf("the folder was copied %d times", len(requests))
	}

	if requests[0].LocalChanges != "" || requests[0].Base != "" {
		t.Fatalf(
			"a delegation that chose nothing still asked the snapshot for %+v. Unset has to "+
				"reach the machine as unset, or runner.yaml and the per-codebase settings stop "+
				"meaning anything",
			requests[0],
		)
	}
}
