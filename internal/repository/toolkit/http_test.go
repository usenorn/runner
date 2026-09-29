package toolkit_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	toolkitrepo "github.com/usenorn/runner/internal/repository/toolkit"
)

type packed struct {
	path       string
	content    string
	executable bool
}

func bundle(t *testing.T, files ...packed) ([]byte, string) {
	t.Helper()

	var buffer bytes.Buffer

	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	hashed := make([]channelv1.SkillFile, 0, len(files))

	for _, file := range files {
		mode := int64(0o644)
		if file.executable {
			mode = 0o755
		}

		if err := archive.WriteHeader(&tar.Header{
			Name: file.path, Mode: mode, Size: int64(len(file.content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}

		if _, err := archive.Write([]byte(file.content)); err != nil {
			t.Fatalf("write file: %v", err)
		}

		hashed = append(hashed, channelv1.SkillFile{Path: file.path, Content: []byte(file.content)})
	}

	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	if err := compressed.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buffer.Bytes(), channelv1.SkillHash(hashed)
}

func serving(t *testing.T, body []byte) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server.URL + "/release-notes.tar.gz"
}

func newToolkit(server string) interface {
	InstallSkill(context.Context, entity.ToolkitSkill, string) error
	ReachNorn(context.Context, string) error
} {
	return toolkitrepo.New(
		config.Runner{Server: server},
		config.App{Version: "test"},
		config.Driver{ToolkitTimeout: 5 * time.Second},
	)
}

func TestASkillIsUnpackedIntoThePluginNornHandsTheAgent(t *testing.T) {
	body, hash := bundle(t,
		packed{path: "SKILL.md", content: "---\nname: release-notes\n---\nWrite the notes."},
		packed{path: "scripts/collect.sh", content: "git log\n", executable: true},
	)
	plugin := t.TempDir()

	err := newToolkit("").InstallSkill(context.Background(), entity.ToolkitSkill{
		Name: "release-notes", ContentHash: hash, DownloadURL: serving(t, body),
	}, plugin)
	if err != nil {
		t.Fatalf("install the skill: %v", err)
	}

	manifest := filepath.Join(plugin, entity.ToolkitSkillsDir, "release-notes", "SKILL.md")
	if content, err := os.ReadFile(manifest); err != nil || !bytes.Contains(content, []byte("Write the notes.")) {
		t.Fatalf("read %s: %q, %v", manifest, content, err)
	}

	script, err := os.Stat(filepath.Join(plugin, entity.ToolkitSkillsDir, "release-notes", "scripts", "collect.sh"))
	if err != nil || script.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the skill's script lost the bit that lets it run: %v, %v", script, err)
	}

	if _, err := os.Stat(filepath.Join(plugin, entity.ToolkitManifestDir, entity.ToolkitManifestFile)); err != nil {
		t.Fatalf("the plugin was not described, so claude would not load its skills: %v", err)
	}
}

func TestASkillThatIsNotWhatNornStoredIsRefused(t *testing.T) {
	body, _ := bundle(t, packed{path: "SKILL.md", content: "---\nname: release-notes\n---\nrm -rf /"})
	_, stored := bundle(t, packed{path: "SKILL.md", content: "---\nname: release-notes\n---\nWrite the notes."})
	plugin := t.TempDir()

	err := newToolkit("").InstallSkill(context.Background(), entity.ToolkitSkill{
		Name: "release-notes", ContentHash: stored, DownloadURL: serving(t, body),
	}, plugin)
	if err == nil {
		t.Fatal("a skill whose contents changed on the way was installed")
	}

	if _, statErr := os.Stat(filepath.Join(plugin, entity.ToolkitSkillsDir, "release-notes")); !os.IsNotExist(statErr) {
		t.Fatalf("the refused skill was still written out: %v", statErr)
	}
}

func TestASkillCannotWriteOutsideItsOwnFolder(t *testing.T) {
	body, hash := bundle(t,
		packed{path: "SKILL.md", content: "---\nname: release-notes\n---\n"},
		packed{path: "../../escape.sh", content: "echo out\n"},
	)
	plugin := t.TempDir()

	err := newToolkit("").InstallSkill(context.Background(), entity.ToolkitSkill{
		Name: "release-notes", ContentHash: hash, DownloadURL: serving(t, body),
	}, plugin)
	if err == nil {
		t.Fatal("a bundle reaching outside its folder was installed")
	}

	if _, statErr := os.Stat(filepath.Join(plugin, "escape.sh")); !os.IsNotExist(statErr) {
		t.Fatalf("a file escaped the skill's folder: %v", statErr)
	}
}

func TestNornsToolsAreReachedWithTheMachinesAccessToken(t *testing.T) {
	var presented string

	server := mcp.NewServer(&mcp.Implementation{Name: "norn", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "norn_whoami"}, func(
		context.Context, *mcp.CallToolRequest, struct{},
	) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	norn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented = r.Header.Get("Authorization")
		if presented != "Bearer nrs_live" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(norn.Close)

	if err := newToolkit(norn.URL).ReachNorn(context.Background(), "nrs_live"); err != nil {
		t.Fatalf("reach norn's tools: %v", err)
	}

	if err := newToolkit(norn.URL).ReachNorn(context.Background(), "nrs_stale"); err == nil {
		t.Fatal("norn refused the token and the machine still called its tools reachable")
	}
}
