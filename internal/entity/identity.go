package entity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	AssertionVersion  = "v1"
	AssertionAudience = "norn-runner"
	AssertionNonceLen = 24

	RunnerNameMaxLen = 200
	HostnameMaxLen   = 255
	PlatformMaxLen   = 64
	VersionMaxLen    = 64

	SessionRefreshFloor = time.Second
)

var (
	ErrNotEnrolled          = errors.New("this machine is not connected to norn")
	ErrAlreadyEnrolled      = errors.New("this machine is already connected to norn")
	ErrTokenRefused         = errors.New("norn refused that token")
	ErrTokenNotAgent        = errors.New("that token does not belong to an agent")
	ErrAgentDisabled        = errors.New("that agent is disabled in norn")
	ErrRunnerNameTaken      = errors.New("that agent already has a machine with this name")
	ErrDeviceKeyRefused     = errors.New("norn rejected this machine's device key")
	ErrCredentialInvalid    = errors.New("this machine's credential is no longer valid")
	ErrRunnerRevoked        = errors.New("this machine has been revoked in norn")
	ErrClockSkew            = errors.New("this machine's clock is too far from norn's")
	ErrAssertionRefused     = errors.New("norn refused this machine's signature")
	ErrServerUnreachable    = errors.New("norn could not be reached")
	ErrKeystoreUnavailable  = errors.New("no os keystore is available on this machine")
	ErrMachineSecretMissing = errors.New("this machine has no machine id to key an encrypted store with")
	ErrCredentialsMissing   = errors.New("this machine's credentials are not in the store")
	ErrIdentityMalformed    = errors.New("the identity file cannot be read")
	ErrEnrolmentInvalid     = errors.New("this machine cannot be described to norn")
	ErrEnrolmentStranded    = errors.New("this machine enrolled but cannot keep its credential")
	ErrTicketMissing        = errors.New("norn renewed this machine's session without a channel ticket")
)

type ClockSkewError struct {
	Offset time.Duration
}

func (e ClockSkewError) Error() string {
	direction := "ahead of"
	offset := e.Offset

	if offset < 0 {
		direction = "behind"
		offset = -offset
	}

	return fmt.Sprintf("this machine's clock is %s %s norn's", offset.Round(time.Second), direction)
}

func (e ClockSkewError) Unwrap() error {
	return ErrClockSkew
}

type UnreachableError struct {
	Detail string
}

func (e UnreachableError) Error() string {
	if e.Detail == "" {
		return ErrServerUnreachable.Error()
	}

	return e.Detail
}

func (e UnreachableError) Unwrap() error {
	return ErrServerUnreachable
}

type Store string

const (
	StoreKeyring   Store = "keyring"
	StoreEncrypted Store = "encrypted-file"
)

func Stores() []Store {
	return []Store{StoreKeyring, StoreEncrypted}
}

func (s Store) Valid() bool {
	return slices.Contains(Stores(), s)
}

type Host struct {
	Hostname string
	OS       string
	Arch     string
	Version  string
}

type Identity struct {
	RunnerID    uuid.UUID
	WorkspaceID uuid.UUID
	AgentID     uuid.UUID
	AgentName   string
	RunnerName  string
	Server      string
	Store       Store
	EnrolledAt  time.Time
}

func (i Identity) Bound() bool {
	return i.RunnerID != uuid.Nil
}

func (i Identity) Agent() string {
	if strings.TrimSpace(i.AgentName) != "" {
		return i.AgentName
	}

	return i.AgentID.String()
}

type Credentials struct {
	DeviceKey    ed25519.PrivateKey
	RefreshToken string
}

func (c Credentials) Complete() bool {
	return len(c.DeviceKey) == ed25519.PrivateKeySize && c.RefreshToken != ""
}

type Assertion struct {
	RunnerID uuid.UUID
	Nonce    string
	IssuedAt time.Time
	Audience string
}

func NewAssertion(runnerID uuid.UUID, now time.Time) (Assertion, error) {
	raw := make([]byte, AssertionNonceLen)
	if _, err := rand.Read(raw); err != nil {
		return Assertion{}, fmt.Errorf("generate assertion nonce: %w", err)
	}

	return Assertion{
		RunnerID: runnerID,
		Nonce:    base64.RawURLEncoding.EncodeToString(raw),
		IssuedAt: now.UTC().Truncate(time.Second),
		Audience: AssertionAudience,
	}, nil
}

func (a Assertion) SigningPayload() []byte {
	return []byte(strings.Join([]string{
		AssertionVersion,
		a.RunnerID.String(),
		a.Nonce,
		a.IssuedAt.UTC().Format(time.RFC3339Nano),
		a.Audience,
	}, "\n"))
}

func EncodePublicKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

func (a Assertion) Sign(key ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, a.SigningPayload()))
}

type SessionState string

const (
	SessionUnenrolled        SessionState = "unenrolled"
	SessionConnecting        SessionState = "connecting"
	SessionLive              SessionState = "live"
	SessionOffline           SessionState = "offline"
	SessionClockSkew         SessionState = "clock-skew"
	SessionRevoked           SessionState = "revoked"
	SessionCredentialInvalid SessionState = "credential-invalid"
)

func SessionStates() []SessionState {
	return []SessionState{
		SessionUnenrolled,
		SessionConnecting,
		SessionLive,
		SessionOffline,
		SessionClockSkew,
		SessionRevoked,
		SessionCredentialInvalid,
	}
}

func (s SessionState) Valid() bool {
	return slices.Contains(SessionStates(), s)
}

func (s SessionState) Settled() bool {
	return s == SessionRevoked || s == SessionCredentialInvalid
}

type Session struct {
	AccessToken     string
	AccessExpiresAt time.Time
	Ticket          string
	TicketExpiresAt time.Time
	RunnerName      string
	AgentName       string
}

func (s Session) Live(now time.Time) bool {
	return s.AccessToken != "" && now.Before(s.AccessExpiresAt)
}

func (s Session) TicketLive(now time.Time) bool {
	return s.Ticket != "" && now.Before(s.TicketExpiresAt)
}

func (s Session) RefreshIn(now time.Time, lead time.Duration) time.Duration {
	wait := s.AccessExpiresAt.Add(-lead).Sub(now)
	if wait < SessionRefreshFloor {
		return SessionRefreshFloor
	}

	return wait
}

type SessionReport struct {
	State     SessionState
	ExpiresAt time.Time
	Detail    string
}

func ValidateEnrolment(name string, host Host) error {
	limits := []struct {
		field string
		value string
		max   int
	}{
		{"name", name, RunnerNameMaxLen},
		{"hostname", host.Hostname, HostnameMaxLen},
		{"os", host.OS, PlatformMaxLen},
		{"arch", host.Arch, PlatformMaxLen},
		{"version", host.Version, VersionMaxLen},
	}

	for _, limit := range limits {
		trimmed := strings.TrimSpace(limit.value)

		switch {
		case trimmed == "":
			return fmt.Errorf("%w: %s is empty", ErrEnrolmentInvalid, limit.field)
		case utf8.RuneCountInString(trimmed) > limit.max:
			return fmt.Errorf(
				"%w: %s is longer than %d characters", ErrEnrolmentInvalid, limit.field, limit.max,
			)
		}
	}

	return nil
}
