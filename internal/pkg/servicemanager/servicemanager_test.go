package servicemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/config"
)

func TestThePlanNamesTheBinaryItWasGiven(t *testing.T) {
	prepared, err := plan("/usr/local/bin/norn", nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(string(prepared.Content), "/usr/local/bin/norn") {
		t.Fatalf("the unit does not name the binary:\n%s", prepared.Content)
	}

	if !strings.Contains(string(prepared.Content), "runner") {
		t.Fatalf("the unit does not start the runner:\n%s", prepared.Content)
	}

	if prepared.Label == "" || prepared.Path == "" || prepared.Manager == "" {
		t.Fatalf("the plan is missing its identity: %+v", prepared)
	}

	if len(prepared.Activate) == 0 || len(prepared.Remove) == 0 {
		t.Fatalf("the plan can neither be activated nor removed")
	}
}

func TestTheStateRootReachesTheServiceOnlyWhenItIsNotTheDefault(t *testing.T) {
	plain, err := plan("/usr/local/bin/norn", nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if strings.Contains(string(plain.Content), config.StateRootEnv) {
		t.Fatalf(
			"a default state root was pinned into the unit, so a later config change could not "+
				"move it:\n%s", plain.Content,
		)
	}

	relocated, err := plan("/usr/local/bin/norn", map[string]string{config.StateRootEnv: "/srv/norn"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(string(relocated.Content), "/srv/norn") {
		t.Fatalf("a relocated state root never reached the unit:\n%s", relocated.Content)
	}
}

func TestABinaryInsideATemporaryBuildDirectoryIsRefused(t *testing.T) {
	_, err := binaryPath()
	if err == nil {
		t.Skipf("this test binary is not running from a temporary build directory")
	}

	if !strings.Contains(err.Error(), "temporary build directory") {
		t.Fatalf("refusal said %q, want it to explain that the path will vanish", err)
	}
}

func TestAPlanWritesItsUnitFileWhereTheServiceManagerLooksForIt(t *testing.T) {
	prepared, err := plan("/usr/local/bin/norn", nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	prepared.Path = filepath.Join(t.TempDir(), "nested", filepath.Base(prepared.Path))

	if err := prepared.Write(); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	written, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if string(written) != string(prepared.Content) {
		t.Fatalf("the unit on disk differs from the planned one")
	}
}
