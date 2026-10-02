package toolchain_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	toolchainrepo "github.com/usenorn/runner/internal/repository/toolchain"
)

func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestManifestsAreReadAtTheRootAndOneFolderDown(t *testing.T) {
	root := t.TempDir()

	write(t, filepath.Join(root, "go.mod"), "module example.com/norn\n")
	write(t, filepath.Join(root, "Makefile"), "all:\n")
	write(t, filepath.Join(root, "web", "package.json"), `{"packageManager": "pnpm@10.4.1"}`)
	write(t, filepath.Join(root, "web", "node_modules", "left-pad", "package.json"), "{}")
	write(t, filepath.Join(root, "docs", "deep", "go.mod"), "module nested\n")

	manifests, err := toolchainrepo.New().Manifests(context.Background(), []entity.ManifestRoot{
		{RelPath: "norn", Path: root},
	})
	if err != nil {
		t.Fatalf("read the manifests: %v", err)
	}

	if len(manifests) != 2 {
		t.Fatalf("found %+v, want the root and web only", manifests)
	}

	if manifests[0].RelPath != "norn" || !slices.Equal(manifests[0].Files, []string{"Makefile", "go.mod"}) {
		t.Fatalf("the root reads %+v", manifests[0])
	}

	if manifests[1].RelPath != "norn/web" || manifests[1].PackageManager != "pnpm@10.4.1" {
		t.Fatalf("the web folder reads %+v", manifests[1])
	}
}
