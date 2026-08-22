package control

import (
	"errors"
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func adviceFor(err error) (int, string, string) {
	switch {
	case errors.Is(err, entity.ErrEnrolmentStranded):
		return http.StatusFailedDependency, ReasonRefused, err.Error()

	case errors.Is(err, entity.ErrNotEnrolled):
		return http.StatusConflict, ReasonNotEnrolled,
			"this machine is not connected to Norn. Connect it with " +
				"'norn runner connect --token nrn_…'"

	case errors.Is(err, entity.ErrAlreadyEnrolled):
		return http.StatusConflict, ReasonRefused,
			"this machine is already connected to Norn. Run 'norn runner disconnect' first, or " +
				"pass --force to replace the binding"

	case errors.Is(err, entity.ErrIdentityMalformed):
		return http.StatusConflict, ReasonRefused,
			"this machine's identity file cannot be read. Run 'norn runner connect --force' to " +
				"write a fresh one"

	case errors.Is(err, entity.ErrTokenRefused):
		return http.StatusUnauthorized, ReasonRefused,
			"Norn refused that token. It may have expired or been revoked; mint a new one for the " +
				"agent under settings, agents"

	case errors.Is(err, entity.ErrTokenNotAgent):
		return http.StatusForbidden, ReasonRefused,
			"that token does not belong to an agent. A runner acts as an agent, so paste the " +
				"agent's own API token, the same one an MCP client uses"

	case errors.Is(err, entity.ErrAgentDisabled):
		return http.StatusConflict, ReasonRefused,
			"that agent is disabled in Norn. Enable it under settings, agents, then connect again"

	case errors.Is(err, entity.ErrRunnerNameTaken):
		return http.StatusConflict, ReasonRefused,
			"that agent already has a machine connected under this name. Pass --name to call this " +
				"one something else"

	case errors.Is(err, entity.ErrDeviceKeyRefused):
		return http.StatusUnprocessableEntity, ReasonRefused,
			"Norn rejected this machine's device key. This is a bug in the runner; please report it"

	case errors.Is(err, entity.ErrRunnerRevoked):
		return http.StatusUnauthorized, ReasonRefused,
			"this machine has been revoked in Norn. Run 'norn runner disconnect' to clear what is " +
				"left of it here"

	case errors.Is(err, entity.ErrCredentialInvalid):
		return http.StatusUnauthorized, ReasonRefused,
			"this machine's credential is no longer valid. Run 'norn runner disconnect' and " +
				"connect again"

	case errors.Is(err, entity.ErrClockSkew):
		return http.StatusUnauthorized, ReasonRefused,
			err.Error() +
				". Norn refuses a signature more than a few minutes out; sync this machine's clock and " +
				"try again"

	case errors.Is(err, entity.ErrAssertionRefused):
		return http.StatusUnauthorized, ReasonRefused,
			"Norn refused this machine's signature. Run 'norn runner disconnect' and connect again"

	case errors.Is(err, entity.ErrKeystoreUnavailable):
		return http.StatusFailedDependency, ReasonRefused,
			"this machine has no usable OS keystore, so there is nowhere safe to keep the " +
				"credential. On a headless Linux box, pass --insecure-store to keep it in a file " +
				"encrypted with the machine id instead"

	case errors.Is(err, entity.ErrMachineSecretMissing):
		return http.StatusFailedDependency, ReasonRefused,
			"this machine has no machine id, so an encrypted file store has nothing to key itself " +
				"with. Use a machine with an OS keystore"

	case errors.Is(err, entity.ErrCredentialsMissing):
		return http.StatusConflict, ReasonRefused,
			"this machine's credentials are no longer in the store. Run 'norn runner disconnect' " +
				"and connect again"

	case errors.Is(err, entity.ErrEnrolmentInvalid):
		return http.StatusUnprocessableEntity, ReasonRefused,
			err.Error()

	case errors.Is(err, entity.ErrCodebaseRootMissing), errors.Is(err, entity.ErrCodebaseNotAFolder):
		return http.StatusNotFound, ReasonRefused,
			"there is no folder there to read"

	case errors.Is(err, entity.ErrGitMissing):
		return http.StatusFailedDependency, ReasonRefused,
			"git is not installed on this machine, and a folder is read by asking git what is in " +
				"it. Install git and run this again"

	case errors.Is(err, entity.ErrCodebaseEmpty):
		return http.StatusUnprocessableEntity, ReasonRefused,
			"this folder holds no git repositories, so there would be nothing for an agent to " +
				"work on. Connect the folder your repositories sit in"

	case errors.Is(err, entity.ErrCodebaseTooLarge):
		return http.StatusUnprocessableEntity, ReasonRefused,
			"this folder holds more repositories than Norn accepts. Connect a folder further down"

	case errors.Is(err, entity.ErrCodebaseOverlaps):
		return http.StatusConflict, ReasonRefused,
			"this folder sits inside one that is already connected, or holds one. Run " +
				"'norn runner status' to see what this machine already holds"

	case errors.Is(err, entity.ErrCodebaseAlreadyConnected):
		return http.StatusConflict, ReasonRefused,
			"Norn already has a folder connected at that path for this machine"

	case errors.Is(err, entity.ErrCodebaseNotConnected):
		return http.StatusConflict, ReasonRefused,
			"this folder is not connected to Norn. Run 'norn runner inspect' in it to connect it"

	case errors.Is(err, entity.ErrCodebaseNotDrifted):
		return http.StatusConflict, ReasonRefused,
			"this folder holds what was confirmed, so there is nothing to confirm"

	case errors.Is(err, entity.ErrCodebaseNotRunner):
		return http.StatusForbidden, ReasonRefused,
			"Norn does not recognise this machine as a runner. Run 'norn runner disconnect' and " +
				"connect again"

	case errors.Is(err, entity.ErrCodebaseRefused):
		return http.StatusUnprocessableEntity, ReasonRefused,
			err.Error()

	case errors.Is(err, entity.ErrServerUnreachable):
		return http.StatusBadGateway, ReasonRefused,
			err.Error()

	default:
		return http.StatusInternalServerError, ReasonRefused,
			err.Error()
	}
}
