package toolkit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	nornMCPPath    = "/mcp"
	dirMode        = 0o700
	fileMode       = 0o600
	executableMode = 0o700
	executableBits = 0o111
	compressedMax  = 2 * channelv1.SkillBundleMaxBytes
)

var (
	errSkillUnreadable = errors.New("its bundle is not an archive norn writes")
	errSkillTampered   = errors.New("what arrived is not what norn stored")
	errSkillOversized  = errors.New("its bundle is larger than norn allows a skill to be")
	errSkillNoManifest = errors.New("its bundle has no " + channelv1.SkillManifestFile)
)

type manifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type httpToolkit struct {
	client *http.Client
	server string
	app    config.App
	driver config.Driver
}

func New(runner config.Runner, app config.App, driver config.Driver) repository.Toolkit {
	return &httpToolkit{
		client: &http.Client{Timeout: driver.ToolkitTimeout},
		server: strings.TrimRight(runner.Server, "/"),
		app:    app,
		driver: driver,
	}
}

func (r *httpToolkit) InstallSkill(ctx context.Context, skill entity.ToolkitSkill, plugin string) error {
	files, err := r.download(ctx, skill)
	if err != nil {
		return err
	}

	if err := r.describe(plugin); err != nil {
		return err
	}

	into := filepath.Join(plugin, entity.ToolkitSkillsDir, skill.Name)

	if err := os.RemoveAll(into); err != nil {
		return fmt.Errorf("clear %s: %w", into, err)
	}

	for _, file := range files {
		target := filepath.Join(into, filepath.FromSlash(file.path))

		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}

		if err := os.WriteFile(target, file.content, file.mode); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}

	return nil
}

type bundled struct {
	path    string
	content []byte
	mode    os.FileMode
}

func (r *httpToolkit) download(ctx context.Context, skill entity.ToolkitSkill) ([]bundled, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, skill.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("its download address is not one this machine can use: %w", err)
	}

	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("its bundle could not be downloaded: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("its bundle could not be downloaded: the store answered %s", response.Status)
	}

	compressed, err := io.ReadAll(io.LimitReader(response.Body, compressedMax+1))
	if err != nil {
		return nil, fmt.Errorf("its bundle could not be downloaded: %w", err)
	}

	if len(compressed) > compressedMax {
		return nil, errSkillOversized
	}

	files, err := unpack(compressed)
	if err != nil {
		return nil, err
	}

	hashed := make([]channelv1.SkillFile, 0, len(files))
	manifested := false

	for _, file := range files {
		hashed = append(hashed, channelv1.SkillFile{Path: file.path, Content: file.content})
		manifested = manifested || file.path == channelv1.SkillManifestFile
	}

	if channelv1.SkillHash(hashed) != skill.ContentHash {
		return nil, errSkillTampered
	}

	if !manifested {
		return nil, errSkillNoManifest
	}

	return files, nil
}

func unpack(compressed []byte) ([]bundled, error) {
	unzipped, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, errSkillUnreadable
	}

	archive := tar.NewReader(unzipped)

	var (
		files []bundled
		total int64
	)

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}

		if err != nil {
			return nil, errSkillUnreadable
		}

		if header.Typeflag != tar.TypeReg || !inside(header.Name) {
			return nil, errSkillUnreadable
		}

		total += header.Size
		if total > channelv1.SkillBundleMaxBytes || len(files) >= channelv1.SkillBundleMaxFiles {
			return nil, errSkillOversized
		}

		content, err := io.ReadAll(io.LimitReader(archive, header.Size))
		if err != nil {
			return nil, errSkillUnreadable
		}

		mode := os.FileMode(fileMode)
		if header.Mode&executableBits != 0 {
			mode = executableMode
		}

		files = append(files, bundled{path: header.Name, content: content, mode: mode})
	}
}

func inside(name string) bool {
	cleaned := path.Clean(name)

	return name != "" && cleaned == name && !path.IsAbs(name) &&
		cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func (r *httpToolkit) describe(plugin string) error {
	dir := filepath.Join(plugin, entity.ToolkitManifestDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(manifest{
		Name:        entity.ToolkitPluginName,
		Description: "The skills norn gave this agent",
		Version:     r.app.Version,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("describe the agent's skills: %w", err)
	}

	target := filepath.Join(dir, entity.ToolkitManifestFile)

	if err := os.WriteFile(target, append(raw, '\n'), fileMode); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}

	return nil
}

type bearing struct {
	token string
	next  http.RoundTripper
}

func (b bearing) RoundTrip(request *http.Request) (*http.Response, error) {
	signed := request.Clone(request.Context())
	signed.Header.Set("Authorization", "Bearer "+b.token)

	return b.next.RoundTrip(signed)
}

func (r *httpToolkit) NornHandler(accessToken string) http.Handler {
	target, err := url.Parse(r.server + nornMCPPath)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "this machine's norn address is not a url", http.StatusBadGateway)
		})
	}

	return &httputil.ReverseProxy{
		Rewrite: func(proxied *httputil.ProxyRequest) {
			proxied.Out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path}
			proxied.Out.Host = target.Host
			proxied.Out.Header.Set("Authorization", "Bearer "+accessToken)
		},
		FlushInterval: -1,
	}
}

func (r *httpToolkit) ReachNorn(ctx context.Context, accessToken string) error {
	ctx, stop := context.WithTimeout(ctx, r.driver.ToolkitTimeout)
	defer stop()

	client := mcp.NewClient(&mcp.Implementation{Name: entity.ToolkitPluginName, Version: r.app.Version}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: r.server + nornMCPPath,
		HTTPClient: &http.Client{
			Transport: bearing{token: accessToken, next: http.DefaultTransport},
		},
		MaxRetries: -1,
	}, nil)
	if err != nil {
		return fmt.Errorf("norn's tools at %s could not be reached: %w", r.server+nornMCPPath, err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.ListTools(ctx, nil); err != nil {
		return fmt.Errorf("norn's tools at %s refused this machine: %w", r.server+nornMCPPath, err)
	}

	return nil
}
