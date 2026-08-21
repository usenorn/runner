package control_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

func (h *harness) expectEnrolment(t *testing.T, runnerID, agentID uuid.UUID) {
	t.Helper()

	h.dashboard.EXPECT().
		Enrol(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(repository.Enrolled{
			Identity: entity.Identity{
				RunnerID:   runnerID,
				AgentID:    agentID,
				AgentName:  "opsy",
				RunnerName: "test-box",
				Server:     "https://norn.example",
				EnrolledAt: time.Now().UTC(),
			},
			RefreshToken: "nrr_secret",
		}, nil)

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a device key: %v", err)
	}

	h.credentials.EXPECT().Usable(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	h.credentials.EXPECT().Clear(gomock.Any()).Return(nil).AnyTimes()
	h.credentials.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	h.credentials.EXPECT().
		Load(gomock.Any(), gomock.Any()).
		Return(entity.Credentials{DeviceKey: private, RefreshToken: "nrr_secret"}, nil).
		AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{
			AccessToken:     "nrs_live",
			AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			RunnerName:      "test-box",
			AgentName:       "opsy",
		}, nil).
		AnyTimes()
}

func TestConnectingOverTheSocketBindsTheMachineAndStatusThenNamesItsAgent(t *testing.T) {
	h := newHarness(t, nil)
	runnerID, agentID := uuid.New(), uuid.New()

	h.expectEnrolment(t, runnerID, agentID)

	connected, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: "nrn_pasted"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if connected.Agent != "opsy" || connected.Machine != "test-box" {
		t.Fatalf("connect answered %+v, want it to name the agent and the machine", connected)
	}

	if connected.Session != string(entity.SessionLive) {
		t.Fatalf("connect returned before the machine was authenticated: session is %q", connected.Session)
	}

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !status.Enrolled {
		t.Fatalf("a connected machine still reports itself unenrolled")
	}

	if status.Agent != "opsy" {
		t.Fatalf("status names the agent %q, want opsy", status.Agent)
	}

	if status.RunnerID != runnerID.String() {
		t.Fatalf("status names machine %q, want %s", status.RunnerID, runnerID)
	}

	if status.Session != string(entity.SessionLive) {
		t.Fatalf("status reports session %q, want live", status.Session)
	}
}

func TestConnectingATokenNornRefusesComesBackAsSomethingAPersonCanAct(t *testing.T) {
	h := newHarness(t, nil)

	h.credentials.EXPECT().Usable(gomock.Any(), gomock.Any()).Return(nil)

	h.dashboard.EXPECT().
		Enrol(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(repository.Enrolled{}, entity.ErrTokenNotAgent)

	_, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: "nrn_person"})
	if err == nil {
		t.Fatalf("a person's token was accepted")
	}

	if !strings.Contains(err.Error(), "agent") {
		t.Fatalf("the refusal reads %q, and it must say to use the agent's own token", err)
	}
}

func TestDisconnectingAMachineThatWasNeverConnectedExitsDistinctly(t *testing.T) {
	h := newHarness(t, nil)

	_, err := h.client.Disconnect(context.Background())
	if err == nil {
		t.Fatalf("disconnecting an unbound machine reported success")
	}

	if code := entity.ExitCode(err); code != entity.ExitNotEnrolled {
		t.Fatalf(
			"exit code is %d, want %d so a script can tell this apart from a real failure",
			code, entity.ExitNotEnrolled,
		)
	}
}

func TestDisconnectingClearsTheBindingAndSaysWhereToRetireTheMachine(t *testing.T) {
	h := newHarness(t, nil)

	h.expectEnrolment(t, uuid.New(), uuid.New())

	if _, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: "nrn_pasted"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	disconnected, err := h.client.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	if disconnected.Machine != "test-box" || disconnected.Server != "https://norn.example" {
		t.Fatalf("disconnect answered %+v, want it to name the machine and where norn is", disconnected)
	}

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Enrolled {
		t.Fatalf("a disconnected machine still reports itself enrolled")
	}

	if status.Session != string(entity.SessionUnenrolled) {
		t.Fatalf("a disconnected machine reports session %q, want unenrolled", status.Session)
	}
}

func TestConnectingTwiceIsRefusedUntilTheBindingIsReplacedOnPurpose(t *testing.T) {
	h := newHarness(t, nil)

	h.expectEnrolment(t, uuid.New(), uuid.New())

	if _, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: "nrn_pasted"}); err != nil {
		t.Fatalf("the first connect: %v", err)
	}

	_, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: "nrn_pasted"})
	if err == nil {
		t.Fatalf("a second connect quietly replaced the binding")
	}

	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("the refusal reads %q, and it must say how to replace the binding on purpose", err)
	}
}

func TestThePastedTokenReachesNothingInTheStateDirectory(t *testing.T) {
	h := newHarness(t, nil)

	h.expectEnrolment(t, uuid.New(), uuid.New())

	const token = "nrn_averydistinctivetokenvalue"

	if _, err := h.client.Connect(context.Background(), control.ConnectRequest{Token: token}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	err := filepath.WalkDir(h.dir.Root(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if bytes.Contains(raw, []byte(token)) {
			t.Errorf("the pasted token is readable in %s", path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the state directory: %v", err)
	}
}
