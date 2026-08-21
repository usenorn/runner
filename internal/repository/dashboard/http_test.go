package dashboard_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/dashboardclient"
	"github.com/usenorn/runner/internal/repository"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
)

func newDashboard(t *testing.T, handler http.Handler) repository.Dashboard {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	runner := config.Runner{Server: server.URL}

	client, err := dashboardclient.New(
		runner,
		config.Session{RequestTimeout: 5 * time.Second},
		config.App{Version: "test"},
	)
	if err != nil {
		t.Fatalf("build a dashboard client: %v", err)
	}

	return dashboardrepo.New(client, runner)
}

func problem(t *testing.T, w http.ResponseWriter, status int, body map[string]any) {
	t.Helper()

	body["status"] = status
	body["type"] = "about:blank"
	body["title"] = http.StatusText(status)

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("write a problem: %v", err)
	}
}

func anEnrolment(t *testing.T) repository.Enrolment {
	t.Helper()

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a device key: %v", err)
	}

	return repository.Enrolment{
		Name:      "test-box",
		Host:      entity.Host{Hostname: "test-box", OS: "darwin", Arch: "arm64", Version: "test"},
		PublicKey: public,
	}
}

func anAssertion(t *testing.T) entity.Assertion {
	t.Helper()

	assertion, err := entity.NewAssertion(uuid.New(), time.Now())
	if err != nil {
		t.Fatalf("build an assertion: %v", err)
	}

	return assertion
}

func TestEnrolmentSendsTheDeviceKeyAndHostNornAsksForAndKeepsWhatComesBack(t *testing.T) {
	runnerID, workspaceID, agentID := uuid.New(), uuid.New(), uuid.New()

	var seen map[string]any

	repo := newDashboard(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runners" || r.Method != http.MethodPost {
			t.Errorf("enrolment went to %s %s, want POST /v1/runners", r.Method, r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer nrn_pasted" {
			t.Errorf("enrolment presented %q, want the agent token as a bearer", got)
		}

		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode the enrolment body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(map[string]any{
			"refreshToken": "nrr_secret",
			"runner": map[string]any{
				"id":          runnerID,
				"workspaceId": workspaceID,
				"agentId":     agentID,
				"agentName":   "opsy",
				"name":        "test-box",
				"status":      "active",
				"enrolledAt":  time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
				"host": map[string]any{
					"hostname": "test-box", "os": "darwin", "arch": "arm64", "version": "test",
				},
			},
		}); err != nil {
			t.Errorf("write the enrolled runner: %v", err)
		}
	}))

	enrolment := anEnrolment(t)

	enrolled, err := repo.Enrol(context.Background(), "nrn_pasted", enrolment)
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}

	if got := seen["publicKey"]; got != base64.StdEncoding.EncodeToString(enrolment.PublicKey) {
		t.Fatalf("the public key went over as %v, want it in standard base64", got)
	}

	if _, sent := seen["workspaceId"]; sent {
		t.Fatalf("enrolment named a workspace, and norn takes that from the agent instead")
	}

	if enrolled.RefreshToken != "nrr_secret" {
		t.Fatalf("the refresh secret came back as %q", enrolled.RefreshToken)
	}

	if enrolled.Identity.RunnerID != runnerID || enrolled.Identity.AgentID != agentID {
		t.Fatalf("the identity does not name the machine norn created: %+v", enrolled.Identity)
	}

	if enrolled.Identity.WorkspaceID != workspaceID {
		t.Fatalf("the identity lost the workspace norn put it in")
	}

	if enrolled.Identity.AgentName != "opsy" {
		t.Fatalf(
			"the identity came back naming the agent %q, want opsy so status needs no second call",
			enrolled.Identity.AgentName,
		)
	}
}

func TestEachWayNornCanRefuseEnrolmentBecomesItsOwnAnswer(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   map[string]any
		want   error
	}{
		{"an expired token", http.StatusUnauthorized, map[string]any{
			"detail": "that token is not valid",
		}, entity.ErrTokenRefused},
		{"a person's token", http.StatusForbidden, map[string]any{
			"detail": "only an agent may enrol a runner",
		}, entity.ErrTokenNotAgent},
		{"a disabled agent", http.StatusConflict, map[string]any{
			"code": "agent_disabled", "detail": "this agent is disabled",
		}, entity.ErrAgentDisabled},
		{"a name in use", http.StatusConflict, map[string]any{
			"code": "runner_name_taken", "detail": "runner name already used by this agent",
		}, entity.ErrRunnerNameTaken},
		{"a key norn cannot read", http.StatusUnprocessableEntity, map[string]any{
			"detail": "runner device key is not an ed25519 public key",
		}, entity.ErrDeviceKeyRefused},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			repo := newDashboard(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				problem(t, w, each.status, each.body)
			}))

			_, err := repo.Enrol(context.Background(), "nrn_pasted", anEnrolment(t))
			if !errors.Is(err, each.want) {
				t.Fatalf("%s returned %v, want %v so the message can name the fix", each.name, err, each.want)
			}
		})
	}
}

func TestATokenExchangeCarriesTheSignedAssertionInTheBodyAndNoBearer(t *testing.T) {
	assertion := anAssertion(t)

	var seen map[string]any

	repo := newDashboard(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runners/token" || r.Method != http.MethodPost {
			t.Errorf("the exchange went to %s %s, want POST /v1/runners/token", r.Method, r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("the exchange presented %q, and norn takes no bearer there", got)
		}

		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode the exchange body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(map[string]any{
			"accessToken":     "nrs_live",
			"expiresIn":       900,
			"ticket":          "nrt_once",
			"ticketExpiresIn": 60,
			"runner": map[string]any{
				"id": assertion.RunnerID, "workspaceId": uuid.New(), "agentId": uuid.New(),
				"agentName": "opsy", "name": "test-box", "status": "active",
				"enrolledAt": time.Now().UTC(),
				"host": map[string]any{
					"hostname": "test-box", "os": "darwin", "arch": "arm64", "version": "test",
				},
			},
		}); err != nil {
			t.Errorf("write the session: %v", err)
		}
	}))

	before := time.Now().UTC()

	session, err := repo.Exchange(context.Background(), "nrr_secret", assertion, "c2lnbmF0dXJl")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if seen["refreshToken"] != "nrr_secret" || seen["signature"] != "c2lnbmF0dXJl" {
		t.Fatalf("the exchange body lost the credential or the signature: %v", seen)
	}

	if seen["audience"] != entity.AssertionAudience {
		t.Fatalf("the exchange named audience %v, and norn accepts only %q", seen["audience"], entity.AssertionAudience)
	}

	if seen["issuedAt"] != assertion.IssuedAt.Format(time.RFC3339Nano) {
		t.Fatalf(
			"the timestamp went over as %v but was signed as %q, so norn will rebuild a different payload",
			seen["issuedAt"], assertion.IssuedAt.Format(time.RFC3339Nano),
		)
	}

	if session.AccessToken != "nrs_live" || session.Ticket != "nrt_once" {
		t.Fatalf("the session came back as %+v", session)
	}

	if session.AccessExpiresAt.Before(before.Add(890 * time.Second)) {
		t.Fatalf("the session expires at %s, want fifteen minutes out", session.AccessExpiresAt)
	}

	if session.AgentName != "opsy" || session.RunnerName != "test-box" {
		t.Fatalf("the session lost the names norn holds for this machine: %+v", session)
	}
}

func TestEachWayNornCanRefuseAnExchangeBecomesItsOwnAnswer(t *testing.T) {
	cases := []struct {
		name string
		code string
		want error
	}{
		{"a revoked machine", "runner_revoked", entity.ErrRunnerRevoked},
		{"a credential norn no longer knows", "runner_credential_invalid", entity.ErrCredentialInvalid},
		{"a forged signature", "runner_assertion_forged", entity.ErrAssertionRefused},
		{"an assertion naming another machine", "runner_assertion_mismatch", entity.ErrAssertionRefused},
		{"a nonce already spent", "runner_assertion_replayed", entity.ErrAssertionRefused},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			repo := newDashboard(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				problem(t, w, http.StatusUnauthorized, map[string]any{
					"code": each.code, "detail": each.code,
				})
			}))

			_, err := repo.Exchange(context.Background(), "nrr_secret", anAssertion(t), "c2ln")
			if !errors.Is(err, each.want) {
				t.Fatalf("%s returned %v, want %v", each.name, err, each.want)
			}
		})
	}
}

func TestAClockTooFarFromNornsIsReportedWithTheDriftItMeasured(t *testing.T) {
	repo := newDashboard(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Add(-8*time.Minute).Format(http.TimeFormat))
		problem(t, w, http.StatusUnauthorized, map[string]any{
			"code": "runner_assertion_stale", "detail": "runner assertion is outside the permitted clock skew",
		})
	}))

	_, err := repo.Exchange(context.Background(), "nrr_secret", anAssertion(t), "c2ln")
	if !errors.Is(err, entity.ErrClockSkew) {
		t.Fatalf("a stale assertion returned %v, want it read as clock skew", err)
	}

	var skew entity.ClockSkewError
	if !errors.As(err, &skew) {
		t.Fatalf("the clock skew carried no measurement, so the message cannot say how far out it is")
	}

	if skew.Offset.Round(time.Minute) != 8*time.Minute {
		t.Fatalf("the drift measured %s, want about eight minutes ahead", skew.Offset)
	}
}

func TestANornThatCannotBeReachedIsSaidToBeUnreachable(t *testing.T) {
	runner := config.Runner{Server: "http://127.0.0.1:1"}

	client, err := dashboardclient.New(
		runner,
		config.Session{RequestTimeout: time.Second},
		config.App{Version: "test"},
	)
	if err != nil {
		t.Fatalf("build a dashboard client: %v", err)
	}

	repo := dashboardrepo.New(client, runner)

	if _, err := repo.Enrol(context.Background(), "nrn_pasted", anEnrolment(t)); !errors.Is(err, entity.ErrServerUnreachable) {
		t.Fatalf("a server that is not there returned %v, want it named as unreachable", err)
	}
}
