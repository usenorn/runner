package entity

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	ToolkitServerName   = "norn"
	ToolkitPluginName   = "norn-agent"
	ToolkitSkillsDir    = "skills"
	ToolkitManifestDir  = ".claude-plugin"
	ToolkitManifestFile = "plugin.json"

	ToolkitStdio = "stdio"
	ToolkitHTTP  = "http"
	ToolkitSSE   = "sse"

	ToolkitSkillKind  = "skill"
	ToolkitServerKind = "mcp server"
)

var (
	ErrToolkitUnavailable = errors.New(
		"this run needs capabilities this machine could not get ready, so the coding agent was " +
			"not started",
	)
	ErrToolkitReserved       = errors.New("the name norn belongs to norn's own tools and cannot be replaced")
	ErrToolkitNameInvalid    = errors.New("that is not a name norn gives a skill or a server")
	ErrToolkitTransport      = errors.New("this machine does not know that transport")
	ErrToolkitCommandMissing = errors.New("its command is not installed on this machine")

	toolkitName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

type ToolkitSkill struct {
	Name        string
	ContentHash string
	DownloadURL string
}

type ToolkitServer struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Headers   map[string]string
}

type Toolkit struct {
	Instructions string
	Skills       []ToolkitSkill
	Servers      []ToolkitServer
}

type ToolkitGap struct {
	Kind   string
	Name   string
	Reason error
}

func ToolkitOf(start channelv1.Start) Toolkit {
	toolkit := Toolkit{
		Instructions: strings.TrimSpace(start.Instructions),
		Skills:       make([]ToolkitSkill, 0, len(start.Toolkit.Skills)),
		Servers:      make([]ToolkitServer, 0, len(start.Toolkit.MCPServers)),
	}

	for _, skill := range start.Toolkit.Skills {
		toolkit.Skills = append(toolkit.Skills, ToolkitSkill(skill))
	}

	for _, server := range start.Toolkit.MCPServers {
		toolkit.Servers = append(toolkit.Servers, ToolkitServer{
			Name:      server.Name,
			Transport: server.Transport,
			Command:   server.Command,
			Args:      slices.Clone(server.Args),
			Env:       maps.Clone(server.Env),
			URL:       server.URL,
			Headers:   maps.Clone(server.Headers),
		})
	}

	return toolkit
}

func (s ToolkitSkill) Valid() bool {
	return toolkitName.MatchString(s.Name)
}

func (t Toolkit) Gaps() []ToolkitGap {
	var gaps []ToolkitGap

	for _, skill := range t.Skills {
		if !skill.Valid() {
			gaps = append(gaps, ToolkitGap{Kind: ToolkitSkillKind, Name: skill.Name, Reason: ErrToolkitNameInvalid})
		}
	}

	for _, server := range t.Servers {
		switch {
		case server.Name == ToolkitServerName:
			gaps = append(gaps, ToolkitGap{Kind: ToolkitServerKind, Name: server.Name, Reason: ErrToolkitReserved})
		case !toolkitName.MatchString(server.Name):
			gaps = append(gaps, ToolkitGap{Kind: ToolkitServerKind, Name: server.Name, Reason: ErrToolkitNameInvalid})
		case server.Transport != ToolkitStdio && server.Transport != ToolkitHTTP && server.Transport != ToolkitSSE:
			gaps = append(gaps, ToolkitGap{Kind: ToolkitServerKind, Name: server.Name, Reason: ErrToolkitTransport})
		}
	}

	return gaps
}

func (g ToolkitGap) String() string {
	return fmt.Sprintf("the %s %s (%s)", g.Kind, g.Name, g.Reason)
}

func Shortfall(gaps []ToolkitGap) error {
	if len(gaps) == 0 {
		return nil
	}

	named := make([]string, 0, len(gaps))

	for _, gap := range gaps {
		named = append(named, gap.String())
	}

	return fmt.Errorf("%w: %s", ErrToolkitUnavailable, strings.Join(named, "; "))
}
