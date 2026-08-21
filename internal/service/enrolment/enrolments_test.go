package enrolment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

func TestConnectingDescribesThisMachineAndKeepsWhatNornHandsBack(t *testing.T) {
	h := newHarness(t)
	identity := h.identity()

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectAdoption()

	h.dashboard.EXPECT().
		Enrol(gomock.Any(), "nrn_pasted", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, enrolment repository.Enrolment) (repository.Enrolled, error) {
			if enrolment.Host != h.host {
				t.Errorf("enrolment described the machine as %+v, want %+v", enrolment.Host, h.host)
			}

			if enrolment.Name != h.host.Hostname {
				t.Errorf("the machine was named %q, want its hostname by default", enrolment.Name)
			}

			if len(enrolment.PublicKey) == 0 {
				t.Errorf("enrolment carried no device key")
			}

			return repository.Enrolled{Identity: identity, RefreshToken: "nrr_secret"}, nil
		})

	h.expectReplacement()
	h.credentials.EXPECT().Save(gomock.Any(), entity.StoreKeyring, gomock.Any()).Return(nil)
	h.identities.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)

	connected, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if connected.Identity.AgentName != "opsy" {
		t.Fatalf("the identity does not name the agent, so status cannot either")
	}

	if connected.Identity.Store != entity.StoreKeyring {
		t.Fatalf("the identity records the %q store, want the keystore by default", connected.Identity.Store)
	}
}

func TestTheCredentialIsKeptBeforeTheIdentityThatPointsAtIt(t *testing.T) {
	h := newHarness(t)

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectEnrolment(h.identity())
	h.expectAdoption()

	h.expectReplacement()

	gomock.InOrder(
		h.credentials.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
		h.identities.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil),
	)

	if _, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func TestAHostWithNoUsableStoreIsRefusedBeforeAnythingIsPutOnRecordInNorn(t *testing.T) {
	h := newHarness(t)

	h.expectUnenrolled()

	h.credentials.EXPECT().
		Usable(gomock.Any(), entity.StoreKeyring).
		Return(entity.ErrKeystoreUnavailable)

	_, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"})
	if !errors.Is(err, entity.ErrKeystoreUnavailable) {
		t.Fatalf("a host with no keystore returned %v, want it refused", err)
	}
}

func TestAMachineThatCannotKeepItsCredentialIsToldTheEnrolmentIsStranded(t *testing.T) {
	h := newHarness(t)
	identity := h.identity()

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectEnrolment(identity)
	h.expectReplacement()

	h.credentials.EXPECT().
		Save(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ErrKeystoreUnavailable)

	_, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"})
	if err == nil {
		t.Fatalf("a machine that could not keep its credential reported success")
	}

	if !errors.Is(err, entity.ErrEnrolmentStranded) {
		t.Fatalf("the failure does not say the machine was left stranded in norn: %v", err)
	}

	if !strings.Contains(err.Error(), identity.RunnerName) || !strings.Contains(err.Error(), "Revoke") {
		t.Fatalf("the message does not say which machine to revoke in norn: %q", err)
	}
}

func TestTheSecretsNeverLeaveTheStoreTheyBelongIn(t *testing.T) {
	h := newHarness(t)

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectEnrolment(h.identity())
	h.expectAdoption()

	h.expectReplacement()

	h.credentials.EXPECT().
		Save(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ entity.Store, credentials entity.Credentials) error {
			if credentials.RefreshToken != "nrr_secret" {
				t.Errorf("the refresh secret did not reach the store")
			}

			return nil
		})

	h.identities.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, identity entity.Identity) error {
			for _, field := range []string{
				identity.AgentName, identity.RunnerName, identity.Server, identity.Agent(),
			} {
				if strings.Contains(field, "nrn_pasted") || strings.Contains(field, "nrr_secret") {
					t.Errorf("a secret reached the identity file in %q", field)
				}
			}

			return nil
		})

	if _, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func TestAMachineAlreadyBoundIsNotRebound(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity(), nil)

	_, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"})
	if !errors.Is(err, entity.ErrAlreadyEnrolled) {
		t.Fatalf("connecting over an existing binding returned %v, want it refused", err)
	}
}

func TestAForcedConnectOnlyLetsGoOfTheOldBindingOnceNornHasAcceptedTheNew(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity(), nil)
	h.expectUsableStore()
	h.expectAdoption()
	h.sessions.EXPECT().Forget()

	gomock.InOrder(
		h.dashboard.EXPECT().
			Enrol(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(repository.Enrolled{Identity: h.identity(), RefreshToken: "nrr_secret"}, nil),
		h.credentials.EXPECT().Clear(gomock.Any()).Return(nil),
		h.credentials.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
		h.identities.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil),
	)

	h.identities.EXPECT().Clear(gomock.Any()).Return(nil)

	if _, err := h.service.Connect(
		context.Background(), service.ConnectInput{Token: "nrn_pasted", Force: true},
	); err != nil {
		t.Fatalf("a forced connect: %v", err)
	}
}

func TestAForcedConnectThatNornRefusesLeavesTheWorkingBindingAlone(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(h.identity(), nil)
	h.expectUsableStore()

	h.dashboard.EXPECT().
		Enrol(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(repository.Enrolled{}, entity.ErrRunnerNameTaken)

	_, err := h.service.Connect(
		context.Background(), service.ConnectInput{Token: "nrn_pasted", Force: true},
	)
	if !errors.Is(err, entity.ErrRunnerNameTaken) {
		t.Fatalf("a forced connect returned %v, want norn's refusal", err)
	}
}

func TestAnIdentityThatCannotBeReadIsReplaceableWithForce(t *testing.T) {
	h := newHarness(t)

	h.identities.EXPECT().Load(gomock.Any()).Return(entity.Identity{}, entity.ErrIdentityMalformed)
	h.expectUsableStore()
	h.sessions.EXPECT().Forget()
	h.credentials.EXPECT().Clear(gomock.Any()).Return(nil)
	h.identities.EXPECT().Clear(gomock.Any()).Return(nil)
	h.expectEnrolment(h.identity())
	h.expectAdoption()
	h.credentials.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	h.identities.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)

	if _, err := h.service.Connect(
		context.Background(), service.ConnectInput{Token: "nrn_pasted", Force: true},
	); err != nil {
		t.Fatalf("forcing over an unreadable identity: %v", err)
	}
}

func TestAnAgentNornDoesNotNameStillLeavesTheMachineConnected(t *testing.T) {
	h := newHarness(t)

	identity := h.identity()
	identity.AgentName = ""

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectEnrolment(identity)
	h.expectAdoption()
	h.expectReplacement()

	h.credentials.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	h.identities.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)

	connected, err := h.service.Connect(context.Background(), service.ConnectInput{Token: "nrn_pasted"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if connected.Identity.Agent() != identity.AgentID.String() {
		t.Fatalf("an unnamed agent showed as %q, want its id", connected.Identity.Agent())
	}
}

func TestConnectingWithTheInsecureStoreRecordsWhereTheCredentialWent(t *testing.T) {
	h := newHarness(t)

	h.expectUnenrolled()
	h.expectUsableStore()
	h.expectEnrolment(h.identity())
	h.expectAdoption()

	h.expectReplacement()
	h.credentials.EXPECT().Save(gomock.Any(), entity.StoreEncrypted, gomock.Any()).Return(nil)

	h.identities.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, identity entity.Identity) error {
			if identity.Store != entity.StoreEncrypted {
				t.Errorf(
					"the identity records the %q store, so a later start would look in the wrong place",
					identity.Store,
				)
			}

			return nil
		})

	if _, err := h.service.Connect(context.Background(), service.ConnectInput{
		Token: "nrn_pasted", Store: entity.StoreEncrypted,
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func TestDisconnectingClearsBothStoresAndNamesTheMachineItLetGo(t *testing.T) {
	h := newHarness(t)
	identity := h.identity()

	h.identities.EXPECT().Load(gomock.Any()).Return(identity, nil)
	h.sessions.EXPECT().Forget()

	gomock.InOrder(
		h.credentials.EXPECT().Clear(gomock.Any()).Return(nil),
		h.identities.EXPECT().Clear(gomock.Any()).Return(nil),
	)

	released, err := h.service.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	if released.RunnerName != identity.RunnerName || released.Server != identity.Server {
		t.Fatalf("disconnect did not say which machine it let go of, or where norn is")
	}
}

func TestDisconnectingAMachineThatWasNeverConnectedSaysSo(t *testing.T) {
	h := newHarness(t)

	h.expectUnenrolled()
	h.expectUsableStore()

	if _, err := h.service.Disconnect(context.Background()); !errors.Is(err, entity.ErrNotEnrolled) {
		t.Fatalf("disconnecting an unbound machine returned %v, want it named", err)
	}
}
