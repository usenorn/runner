package session_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	"github.com/usenorn/runner/internal/service"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
)

type harness struct {
	dashboard   *dashboardrepo.MockDashboard
	identities  *identityrepo.MockIdentity
	credentials *credentialrepo.MockCredential
	service     service.Sessions
	identity    entity.Identity
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	ctrl := gomock.NewController(t)

	h := &harness{
		dashboard:   dashboardrepo.NewMockDashboard(ctrl),
		identities:  identityrepo.NewMockIdentity(ctrl),
		credentials: credentialrepo.NewMockCredential(ctrl),
		identity: entity.Identity{
			RunnerID:   uuid.New(),
			RunnerName: "test-box",
			Store:      entity.StoreKeyring,
		},
	}

	h.service = sessionsvc.New(
		h.dashboard,
		h.identities,
		h.credentials,
		config.Session{
			RequestTimeout: time.Second,
			RefreshLead:    2 * time.Minute,
			RetryMin:       5 * time.Second,
			RetryMax:       time.Minute,
		},
	)

	return h
}

func (h *harness) expectCredentials(t *testing.T) {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a device key: %v", err)
	}

	h.credentials.EXPECT().
		Load(gomock.Any(), h.identity.Store).
		Return(entity.Credentials{DeviceKey: private, RefreshToken: "nrr_secret"}, nil).
		AnyTimes()
}

func TestAMachineWithNoBindingReportsItselfUnenrolled(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().
		Load(gomock.Any()).
		Return(entity.Identity{}, entity.ErrNotEnrolled).
		AnyTimes()

	runOnce(t, h.service)

	if state := h.service.Report().State; state != entity.SessionUnenrolled {
		t.Fatalf("an unbound machine reported %q, want unenrolled", state)
	}
}

func TestAdoptingAnIdentityTradesTheSignedAssertionForALiveSession(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), "nrr_secret", gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, _ string, assertion entity.Assertion, signature string,
		) (entity.Session, error) {
			if assertion.RunnerID != h.identity.RunnerID {
				t.Errorf("the assertion named %s, want this machine", assertion.RunnerID)
			}

			if assertion.Audience != entity.AssertionAudience {
				t.Errorf("the assertion named audience %q", assertion.Audience)
			}

			if signature == "" {
				t.Errorf("the assertion went out unsigned")
			}

			return entity.Session{
				AccessToken:     "nrs_live",
				AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			}, nil
		})

	report := h.service.Adopt(context.Background(), h.identity)

	if report.State != entity.SessionLive {
		t.Fatalf("adopting a fresh identity reported %q, want live", report.State)
	}

	if h.service.Report().State != entity.SessionLive {
		t.Fatalf("the machine did not hold on to the session it just took")
	}
}

func TestARevokedMachineStopsRenewingAndSaysWhy(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{}, entity.ErrRunnerRevoked).
		Times(1)

	runOnce(t, h.service)

	if state := h.service.Report().State; state != entity.SessionRevoked {
		t.Fatalf("a revoked machine reported %q, want revoked", state)
	}

	runOnce(t, h.service)

	if state := h.service.Report().State; state != entity.SessionRevoked {
		t.Fatalf("a revoked machine drifted to %q instead of staying put", state)
	}
}

func TestARevokedMachineTriesAgainOnceItIsConnectedAfresh(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	gomock.InOrder(
		h.dashboard.EXPECT().
			Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(entity.Session{}, entity.ErrRunnerRevoked),
		h.dashboard.EXPECT().
			Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(entity.Session{
				AccessToken:     "nrs_live",
				AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			}, nil),
	)

	runOnce(t, h.service)
	h.service.Forget()

	if state := h.service.Report().State; state != entity.SessionUnenrolled {
		t.Fatalf("forgetting a revoked machine left it reporting %q", state)
	}

	if report := h.service.Adopt(context.Background(), h.identity); report.State != entity.SessionLive {
		t.Fatalf("a freshly connected machine reported %q, want live", report.State)
	}
}

func TestANornThatCannotBeReachedLeavesTheMachineOfflineRatherThanSettled(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{}, entity.ErrServerUnreachable).
		MinTimes(1)

	report := h.service.Adopt(context.Background(), h.identity)

	if report.State != entity.SessionOffline {
		t.Fatalf("an unreachable norn left the machine %q, want offline", report.State)
	}

	if report.State.Settled() {
		t.Fatalf("an unreachable norn stopped the machine trying, and a network comes back")
	}
}

func TestAClockTooFarOutIsItsOwnStateSoTheMessageCanNameIt(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{}, entity.ClockSkewError{Offset: -9 * time.Minute}).
		MinTimes(1)

	report := h.service.Adopt(context.Background(), h.identity)

	if report.State != entity.SessionClockSkew {
		t.Fatalf("a skewed clock left the machine %q, want clock-skew", report.State)
	}

	if report.Detail == "" {
		t.Fatalf("the report carries no detail, so status cannot say how far the clock is out")
	}
}

func TestCredentialsNoLongerInTheStoreStopTheMachineRatherThanSpinning(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()
	h.credentials.EXPECT().
		Load(gomock.Any(), gomock.Any()).
		Return(entity.Credentials{}, entity.ErrCredentialsMissing).
		Times(1)

	runOnce(t, h.service)

	if state := h.service.Report().State; state != entity.SessionCredentialInvalid {
		t.Fatalf("a machine with no credential reported %q, want credential-invalid", state)
	}
}

func TestALiveSessionIsNotTradedAgainUntilItNearsExpiry(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{
			AccessToken:     "nrs_live",
			AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		}, nil).
		Times(1)

	h.service.Adopt(context.Background(), h.identity)

	for range 3 {
		runOnce(t, h.service)
	}

	if state := h.service.Report().State; state != entity.SessionLive {
		t.Fatalf("a live session was disturbed and became %q", state)
	}
}

func runOnce(t *testing.T, sessions service.Sessions) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		sessions.Run(ctx)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("the session loop did not stop when its context was cancelled")
	}
}

func TestAnAgentRenamedInNornIsRecordedTheNextTimeTheSessionRenews(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identity.AgentName = "opsy"

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{
			AccessToken:     "nrs_live",
			AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			RunnerName:      h.identity.RunnerName,
			AgentName:       "opsy-renamed",
		}, nil)

	h.identities.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, identity entity.Identity) error {
			if identity.AgentName != "opsy-renamed" {
				t.Errorf("the machine kept calling its agent %q after norn renamed it", identity.AgentName)
			}

			return nil
		})

	if report := h.service.Adopt(context.Background(), h.identity); report.State != entity.SessionLive {
		t.Fatalf("adopting reported %q, want live", report.State)
	}
}

func TestAnUnchangedNameIsNotRewrittenEveryTimeTheSessionRenews(t *testing.T) {
	h := newHarness(t)
	h.expectCredentials(t)

	h.identity.AgentName = "opsy"

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity, nil).AnyTimes()

	h.dashboard.EXPECT().
		Exchange(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.Session{
			AccessToken:     "nrs_live",
			AccessExpiresAt: time.Now().UTC().Add(15 * time.Minute),
			RunnerName:      h.identity.RunnerName,
			AgentName:       "opsy",
		}, nil)

	if report := h.service.Adopt(context.Background(), h.identity); report.State != entity.SessionLive {
		t.Fatalf("adopting reported %q, want live", report.State)
	}
}
