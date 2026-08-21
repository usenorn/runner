package entity_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
)

func TestTheAssertionSignsTheFiveFieldsNornJoinsWithNewlines(t *testing.T) {
	runnerID := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	issued := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)

	assertion := entity.Assertion{
		RunnerID: runnerID,
		Nonce:    "nonce-that-is-long-enough",
		IssuedAt: issued,
		Audience: entity.AssertionAudience,
	}

	want := "v1\n" +
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8\n" +
		"nonce-that-is-long-enough\n" +
		"2026-08-21T12:00:00Z\n" +
		"norn-runner"

	if got := string(assertion.SigningPayload()); got != want {
		t.Fatalf("the signing payload is\n%q\nand norn will build\n%q", got, want)
	}
}

func TestAnAssertionStillVerifiesAfterItsTimestampHasBeenThroughJson(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a device key: %v", err)
	}

	assertion, err := entity.NewAssertion(uuid.New(), time.Now().Add(413*time.Millisecond))
	if err != nil {
		t.Fatalf("build an assertion: %v", err)
	}

	signature := assertion.Sign(private)

	encoded, err := json.Marshal(map[string]any{"issuedAt": assertion.IssuedAt})
	if err != nil {
		t.Fatalf("encode the timestamp: %v", err)
	}

	var decoded struct {
		IssuedAt time.Time `json:"issuedAt"`
	}

	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode the timestamp: %v", err)
	}

	replayed := assertion
	replayed.IssuedAt = decoded.IssuedAt

	raw, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("decode the signature: %v", err)
	}

	if !ed25519.Verify(public, replayed.SigningPayload(), raw) {
		t.Fatalf(
			"the signature stopped verifying once the timestamp went through json, so norn will "+
				"see a forged assertion; signed %q and norn rebuilt %q",
			assertion.SigningPayload(), replayed.SigningPayload(),
		)
	}
}

func TestEveryAssertionCarriesANonceNornWillAcceptAndNeverTheSameOneTwice(t *testing.T) {
	seen := make(map[string]struct{})

	for range 100 {
		assertion, err := entity.NewAssertion(uuid.New(), time.Now())
		if err != nil {
			t.Fatalf("build an assertion: %v", err)
		}

		if len(assertion.Nonce) < 16 || len(assertion.Nonce) > 128 {
			t.Fatalf(
				"the nonce is %d bytes, and norn accepts 16 to 128", len(assertion.Nonce),
			)
		}

		if _, repeated := seen[assertion.Nonce]; repeated {
			t.Fatalf("a nonce repeated, and norn spends each one once")
		}

		seen[assertion.Nonce] = struct{}{}
	}
}

func TestAnAssertionIsStampedInWholeSecondsOfUtc(t *testing.T) {
	assertion, err := entity.NewAssertion(uuid.New(), time.Now().In(time.FixedZone("west", -7*3600)))
	if err != nil {
		t.Fatalf("build an assertion: %v", err)
	}

	if assertion.IssuedAt.Location() != time.UTC {
		t.Fatalf("the assertion is stamped in %s, not utc", assertion.IssuedAt.Location())
	}

	if assertion.IssuedAt.Nanosecond() != 0 {
		t.Fatalf("the assertion carries a fraction of a second, which norn re-renders differently")
	}
}

func TestASessionIsRenewedBeforeItExpiresAndAnExpiredOneIsNotLive(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	session := entity.Session{AccessToken: "nrs_x", AccessExpiresAt: now.Add(15 * time.Minute)}

	if !session.Live(now) {
		t.Fatalf("a session with fifteen minutes left reported itself dead")
	}

	if wait := session.RefreshIn(now, 2*time.Minute); wait != 13*time.Minute {
		t.Fatalf("the session renews in %s, want 13m so it never runs to expiry", wait)
	}

	if session.Live(now.Add(16 * time.Minute)) {
		t.Fatalf("an expired session reported itself live")
	}

	if wait := session.RefreshIn(now.Add(16*time.Minute), 2*time.Minute); wait != entity.SessionRefreshFloor {
		t.Fatalf("an overdue session waits %s, want the floor so it retries at once", wait)
	}
}

func TestAClockSkewErrorNamesTheDriftAndStaysDetectable(t *testing.T) {
	behind := entity.ClockSkewError{Offset: -7 * time.Minute}

	if !errors.Is(behind, entity.ErrClockSkew) {
		t.Fatalf("a measured skew stopped matching the clock skew sentinel")
	}

	if got := behind.Error(); got != "this machine's clock is 7m0s behind norn's" {
		t.Fatalf("the skew message is %q, and it must name the drift a person has to fix", got)
	}

	ahead := entity.ClockSkewError{Offset: 90 * time.Second}
	if got := ahead.Error(); got != "this machine's clock is 1m30s ahead of norn's" {
		t.Fatalf("the skew message is %q, want it to say the clock runs fast", got)
	}
}

func TestAMachineNornCannotBeToldAboutIsRefusedBeforeTheRoundTrip(t *testing.T) {
	whole := entity.Host{Hostname: "box", OS: "linux", Arch: "amd64", Version: "0.1.0"}

	if err := entity.ValidateEnrolment("box", whole); err != nil {
		t.Fatalf("a complete machine was refused: %v", err)
	}

	broken := []struct {
		name string
		got  entity.Host
	}{
		{"no hostname", entity.Host{OS: "linux", Arch: "amd64", Version: "0.1.0"}},
		{"no os", entity.Host{Hostname: "box", Arch: "amd64", Version: "0.1.0"}},
		{"no arch", entity.Host{Hostname: "box", OS: "linux", Version: "0.1.0"}},
		{"no version", entity.Host{Hostname: "box", OS: "linux", Arch: "amd64"}},
	}

	for _, each := range broken {
		t.Run(each.name, func(t *testing.T) {
			if err := entity.ValidateEnrolment("box", each.got); !errors.Is(err, entity.ErrEnrolmentInvalid) {
				t.Fatalf("a machine with %s returned %v, want it refused here", each.name, err)
			}
		})
	}

	long := whole
	long.Hostname = string(make([]byte, entity.HostnameMaxLen+1))

	if err := entity.ValidateEnrolment("box", long); !errors.Is(err, entity.ErrEnrolmentInvalid) {
		t.Fatalf("an over-long hostname returned %v, want it refused before norn sees it", err)
	}
}

func TestAnIdentityNamesItsAgentAndFallsBackToTheIdWhenItHasNoName(t *testing.T) {
	agentID := uuid.New()

	named := entity.Identity{AgentID: agentID, AgentName: "opsy"}
	if got := named.Agent(); got != "opsy" {
		t.Fatalf("the identity named its agent %q, want opsy", got)
	}

	anonymous := entity.Identity{AgentID: agentID}
	if got := anonymous.Agent(); got != agentID.String() {
		t.Fatalf("an unnamed agent showed as %q, want its id so status still says which one", got)
	}
}
