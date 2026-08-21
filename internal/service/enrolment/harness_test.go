package enrolment_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	"github.com/usenorn/runner/internal/service"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
)

type harness struct {
	dashboard   *dashboardrepo.MockDashboard
	identities  *identityrepo.MockIdentity
	credentials *credentialrepo.MockCredential
	sessions    *sessionsvc.MockSessions
	service     service.Enrolments
	host        entity.Host
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	ctrl := gomock.NewController(t)

	h := &harness{
		dashboard:   dashboardrepo.NewMockDashboard(ctrl),
		identities:  identityrepo.NewMockIdentity(ctrl),
		credentials: credentialrepo.NewMockCredential(ctrl),
		sessions:    sessionsvc.NewMockSessions(ctrl),
		host: entity.Host{
			Hostname: "test-box", OS: "darwin", Arch: "arm64", Version: "0.1.0",
		},
	}

	h.service = enrolmentsvc.New(h.dashboard, h.identities, h.credentials, h.sessions, h.host)

	return h
}

func (h *harness) identity() entity.Identity {
	return entity.Identity{
		RunnerID:    uuid.New(),
		WorkspaceID: uuid.New(),
		AgentID:     uuid.New(),
		AgentName:   "opsy",
		RunnerName:  "test-box",
		Server:      "https://norn.example",
		EnrolledAt:  time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}
}

func (h *harness) expectUsableStore() {
	h.credentials.EXPECT().Usable(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
}

func (h *harness) expectUnenrolled() {
	h.identities.EXPECT().Load(gomock.Any()).Return(entity.Identity{}, entity.ErrNotEnrolled)
}

func (h *harness) expectEnrolment(identity entity.Identity) {
	h.dashboard.EXPECT().
		Enrol(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(repository.Enrolled{Identity: identity, RefreshToken: "nrr_secret"}, nil)
}

func (h *harness) expectAdoption() {
	h.sessions.EXPECT().
		Adopt(gomock.Any(), gomock.Any()).
		Return(entity.SessionReport{State: entity.SessionLive}).
		AnyTimes()
}

func (h *harness) expectReplacement() {
	h.sessions.EXPECT().Forget()
	h.credentials.EXPECT().Clear(gomock.Any()).Return(nil)
	h.identities.EXPECT().Clear(gomock.Any()).Return(nil)
}
