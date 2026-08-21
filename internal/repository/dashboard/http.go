package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	api "github.com/usenorn/norn/pkg/http/v1/dashboard"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/dashboardclient"
	"github.com/usenorn/runner/internal/repository"
)

const agentDisabledCode = "agent_disabled"

type httpDashboard struct {
	client *dashboardclient.Client
	server string
}

func New(client *dashboardclient.Client, runner config.Runner) repository.Dashboard {
	return &httpDashboard{client: client, server: runner.Server}
}

func (r *httpDashboard) Enrol(
	ctx context.Context,
	token string,
	enrolment repository.Enrolment,
) (repository.Enrolled, error) {
	name := enrolment.Name

	response, err := r.client.EnrolRunnerWithResponse(ctx, api.EnrolRunnerJSONRequestBody{
		Name:      &name,
		PublicKey: entity.EncodePublicKey(enrolment.PublicKey),
		Host: api.RunnerHost{
			Hostname: enrolment.Host.Hostname,
			Os:       enrolment.Host.OS,
			Arch:     enrolment.Host.Arch,
			Version:  enrolment.Host.Version,
		},
	}, bearer(token))
	if err != nil {
		return repository.Enrolled{}, r.unreachable(err)
	}

	if response.JSON201 == nil {
		return repository.Enrolled{}, r.enrolmentRefusal(response)
	}

	return repository.Enrolled{
		Identity:     identityOf(response.JSON201.Runner, r.server, entity.Store("")),
		RefreshToken: response.JSON201.RefreshToken,
	}, nil
}

func (r *httpDashboard) Exchange(
	ctx context.Context,
	refreshToken string,
	assertion entity.Assertion,
	signature string,
) (entity.Session, error) {
	response, err := r.client.ExchangeRunnerTokenWithResponse(
		ctx,
		api.ExchangeRunnerTokenJSONRequestBody{
			RefreshToken: refreshToken,
			RunnerId:     assertion.RunnerID,
			Nonce:        assertion.Nonce,
			IssuedAt:     assertion.IssuedAt,
			Audience:     assertion.Audience,
			Signature:    signature,
		},
	)
	if err != nil {
		return entity.Session{}, r.unreachable(err)
	}

	if response.JSON200 == nil {
		return entity.Session{}, r.exchangeRefusal(response)
	}

	now := time.Now().UTC()
	session := response.JSON200

	return entity.Session{
		AccessToken:     session.AccessToken,
		AccessExpiresAt: now.Add(time.Duration(session.ExpiresIn) * time.Second),
		Ticket:          session.Ticket,
		TicketExpiresAt: now.Add(time.Duration(session.TicketExpiresIn) * time.Second),
		RunnerName:      session.Runner.Name,
		AgentName:       session.Runner.AgentName,
	}, nil
}

func (r *httpDashboard) enrolmentRefusal(response *api.EnrolRunnerResponse) error {
	switch response.HTTPResponse.StatusCode {
	case http.StatusUnauthorized:
		return entity.ErrTokenRefused
	case http.StatusForbidden:
		return entity.ErrTokenNotAgent
	case http.StatusConflict:
		if code(response.Body) == agentDisabledCode {
			return entity.ErrAgentDisabled
		}

		return entity.ErrRunnerNameTaken
	case http.StatusUnprocessableEntity:
		return entity.ErrDeviceKeyRefused
	default:
		return r.refusal(response.HTTPResponse, response.Body)
	}
}

func (r *httpDashboard) exchangeRefusal(response *api.ExchangeRunnerTokenResponse) error {
	if response.ApplicationproblemJSON401 == nil {
		return r.refusal(response.HTTPResponse, response.Body)
	}

	switch response.ApplicationproblemJSON401.Code {
	case api.RunnerProblemCodeRunnerRevoked:
		return entity.ErrRunnerRevoked
	case api.RunnerProblemCodeRunnerCredentialInvalid:
		return entity.ErrCredentialInvalid
	case api.RunnerProblemCodeRunnerAssertionStale:
		return skewOf(response.HTTPResponse)
	case api.RunnerProblemCodeRunnerAssertionForged, api.RunnerProblemCodeRunnerAssertionMismatch, api.RunnerProblemCodeRunnerAssertionReplayed:
		return entity.ErrAssertionRefused
	default:
		return r.refusal(response.HTTPResponse, response.Body)
	}
}

func (r *httpDashboard) refusal(response *http.Response, body []byte) error {
	if detail := detailOf(body); detail != "" {
		return fmt.Errorf("norn answered %s: %s", response.Status, detail)
	}

	return fmt.Errorf("norn answered %s", response.Status)
}

func (r *httpDashboard) unreachable(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return fmt.Errorf("%w at %s: %w", entity.ErrServerUnreachable, r.server, err)
}

func skewOf(response *http.Response) error {
	stamped, err := http.ParseTime(response.Header.Get("Date"))
	if err != nil {
		return entity.ErrClockSkew
	}

	return entity.ClockSkewError{Offset: time.Now().UTC().Sub(stamped.UTC())}
}

func identityOf(runner api.Runner, server string, store entity.Store) entity.Identity {
	return entity.Identity{
		RunnerID:    runner.Id,
		WorkspaceID: runner.WorkspaceId,
		AgentID:     runner.AgentId,
		AgentName:   runner.AgentName,
		RunnerName:  runner.Name,
		Server:      server,
		Store:       store,
		EnrolledAt:  runner.EnrolledAt.UTC(),
	}
}

func bearer(token string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

		return nil
	}
}

func detailOf(body []byte) string {
	var problem struct {
		Detail string `json:"detail"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}

	return problem.Detail
}

func code(body []byte) string {
	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}

	return problem.Code
}
