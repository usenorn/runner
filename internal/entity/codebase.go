package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ScanDepthDefault = 5
	ScanDepthMax     = 12

	CodebaseNameMaxLen      = 200
	CodebaseRootPathMaxLen  = 1024
	RepositoryNameMaxLen    = 200
	RepositoryRelPathMaxLen = 1024
	RepositoryBranchMaxLen  = 255
	RemoteHashMaxLen        = 128
	RemoteHostMaxLen        = 255
	RemotePathTailMaxLen    = 255
	SharedFileMaxLen        = 255
	ToolNameMaxLen          = 64
	ToolVersionMaxLen       = 64

	MaxRepositories = 200
	MaxSharedFiles  = 100
	MaxTools        = 32
)

var (
	ErrCodebaseNotConnected     = errors.New("this folder is not connected to norn")
	ErrCodebaseAlreadyConnected = errors.New("this machine already has a folder connected there")
	ErrCodebaseOverlaps         = errors.New("this folder overlaps one that is already connected")
	ErrCodebaseEmpty            = errors.New("this folder holds no git repositories")
	ErrCodebaseRootMissing      = errors.New("that folder does not exist")
	ErrCodebaseNotAFolder       = errors.New("that path is not a folder")
	ErrCodebaseNotDrifted       = errors.New("this folder has no drift to confirm")
	ErrCodebaseNotRunner        = errors.New("norn does not recognise this machine as a runner")
	ErrCodebaseRefused          = errors.New("norn refused this folder")
	ErrCodebaseTooLarge         = errors.New("this folder holds more repositories than norn accepts")
	ErrScanDeclined             = errors.New("the scan was not confirmed")
	ErrScanNotInteractive       = errors.New("there is no terminal to confirm the scan on")
	ErrGitMissing               = errors.New("git is not installed on this machine")
)

type RepositoryKind string

const (
	RepositoryNormal    RepositoryKind = "repository"
	RepositoryWorktree  RepositoryKind = "worktree"
	RepositoryNested    RepositoryKind = "nested"
	RepositorySubmodule RepositoryKind = "submodule"
	RepositoryBare      RepositoryKind = "bare"
)

func RepositoryKinds() []RepositoryKind {
	return []RepositoryKind{
		RepositoryNormal,
		RepositoryWorktree,
		RepositoryNested,
		RepositorySubmodule,
		RepositoryBare,
	}
}

func (k RepositoryKind) Valid() bool {
	return slices.Contains(RepositoryKinds(), k)
}

func (k RepositoryKind) Listed() bool {
	return k == RepositoryNormal || k == RepositoryWorktree || k == RepositoryNested
}

type Runtime string

const (
	RuntimeProcess Runtime = "process"
	RuntimeDocker  Runtime = "docker"
	RuntimeKVM     Runtime = "kvm"
)

func Runtimes() []Runtime {
	return []Runtime{RuntimeProcess, RuntimeDocker, RuntimeKVM}
}

func (r Runtime) Valid() bool {
	return slices.Contains(Runtimes(), r)
}

type RemoteFingerprint struct {
	Hash     string
	Host     string
	PathTail string
}

func (f RemoteFingerprint) Known() bool {
	return f != RemoteFingerprint{}
}

func (f RemoteFingerprint) Label() string {
	switch {
	case f.Host != "" && f.PathTail != "":
		return f.Host + "/" + f.PathTail
	case f.PathTail != "":
		return f.PathTail
	default:
		return f.Host
	}
}

type GitFacts struct {
	Dir            string
	GitDir         string
	CommonDir      string
	TopLevel       string
	Superproject   string
	RemoteURL      string
	DefaultBranch  string
	Bare           bool
	InsideWorkTree bool
}

type Repository struct {
	Name          string
	RelPath       string
	Kind          RepositoryKind
	DefaultBranch string
	Remote        RemoteFingerprint
	CommonDir     string
	Parent        string
}

type Tool struct {
	Name    string
	Version string
}

type Inventory struct {
	Name         string
	RootPath     string
	Repositories []Repository
	SharedFiles  []string
	Runtimes     []Runtime
	Tools        []Tool
	ScannedAt    time.Time
}

func (i Inventory) Listed() []Repository {
	listed := make([]Repository, 0, len(i.Repositories))

	for _, repository := range i.Repositories {
		if repository.Kind.Listed() {
			listed = append(listed, repository)
		}
	}

	return listed
}

func (i Inventory) Ignored() []Repository {
	ignored := make([]Repository, 0, len(i.Repositories))

	for _, repository := range i.Repositories {
		if !repository.Kind.Listed() {
			ignored = append(ignored, repository)
		}
	}

	return ignored
}

type Codebase struct {
	ID          uuid.UUID
	Name        string
	RootPath    string
	Confirmed   Inventory
	Reported    Inventory
	ConfirmedAt time.Time
	ReportedAt  time.Time
}

func (c Codebase) Drifted() bool {
	return DriftBetween(c.Confirmed, c.Reported).Any()
}

type Drift struct {
	Added   []string
	Removed []string
	Changed []string
}

func (d Drift) Any() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Changed) > 0
}

func (d Drift) Count() int {
	return len(d.Added) + len(d.Removed) + len(d.Changed)
}

func reported(repository Repository) Repository {
	return Repository{
		Name:          repository.Name,
		RelPath:       repository.RelPath,
		DefaultBranch: repository.DefaultBranch,
		Remote:        repository.Remote,
	}
}

func DriftBetween(before, after Inventory) Drift {
	held := make(map[string]Repository, len(before.Repositories))
	for _, repository := range before.Listed() {
		held[repository.RelPath] = reported(repository)
	}

	drift := Drift{}

	for _, repository := range after.Listed() {
		previous, known := held[repository.RelPath]

		switch {
		case !known:
			drift.Added = append(drift.Added, repository.RelPath)
		case previous != reported(repository):
			drift.Changed = append(drift.Changed, repository.RelPath)
		}

		delete(held, repository.RelPath)
	}

	for relPath := range held {
		drift.Removed = append(drift.Removed, relPath)
	}

	sort.Strings(drift.Added)
	sort.Strings(drift.Removed)
	sort.Strings(drift.Changed)

	return drift
}

func SharedFileNames() []string {
	return []string{
		"AGENTS.md",
		"CLAUDE.md",
		"README.md",
		"docker-compose.yml",
		"docker-compose.yaml",
		"Makefile",
		"Taskfile.yml",
		"justfile",
		"mise.toml",
		"flake.nix",
		"devcontainer.json",
		".devcontainer",
		"skills",
		".mcp.json",
		".cursor",
	}
}

func UninterestingDirNames() []string {
	return []string{
		"node_modules",
		"__pycache__",
		".venv",
		".next",
		".svelte-kit",
		".turbo",
		".gradle",
		".terraform",
		".cache",
	}
}

func Classify(root string, facts []GitFacts) []Repository {
	ordered := slices.Clone(facts)
	slices.SortFunc(ordered, func(a, b GitFacts) int { return strings.Compare(a.Dir, b.Dir) })

	repositories := make([]Repository, 0, len(ordered))
	listed := make([]string, 0, len(ordered))

	for _, facts := range ordered {
		repository, ok := classify(root, facts, listed)
		if !ok {
			continue
		}

		if repository.Kind.Listed() {
			listed = append(listed, facts.Dir)
		}

		repositories = append(repositories, repository)
	}

	return repositories
}

func classify(root string, facts GitFacts, listed []string) (Repository, bool) {
	relPath, err := filepath.Rel(root, facts.Dir)
	if err != nil {
		return Repository{}, false
	}

	repository := Repository{
		Name:          filepath.Base(facts.Dir),
		RelPath:       filepath.ToSlash(relPath),
		DefaultBranch: facts.DefaultBranch,
		Remote:        FingerprintRemote(facts.RemoteURL),
	}

	switch {
	case facts.Bare || !facts.InsideWorkTree:
		repository.Kind = RepositoryBare
		repository.DefaultBranch = ""

		return repository, true

	case facts.TopLevel != "" && facts.TopLevel != facts.Dir:
		return Repository{}, false

	case facts.Superproject != "":
		repository.Kind = RepositorySubmodule
		repository.Parent = parentOf(root, facts.Superproject)

		return repository, true

	case facts.CommonDir != "" && facts.CommonDir != facts.GitDir:
		repository.Kind = RepositoryWorktree
		repository.CommonDir = facts.CommonDir
	default:
		repository.Kind = RepositoryNormal
	}

	if enclosing := enclosedBy(facts.Dir, listed); enclosing != "" {
		repository.Kind = RepositoryNested
		repository.Parent = parentOf(root, enclosing)
	}

	return repository, true
}

func parentOf(root, dir string) string {
	relPath, err := filepath.Rel(root, dir)
	if err != nil {
		return ""
	}

	return filepath.ToSlash(relPath)
}

func enclosedBy(dir string, listed []string) string {
	enclosing := ""

	for _, candidate := range listed {
		if !strings.HasPrefix(dir, candidate+string(filepath.Separator)) {
			continue
		}

		if len(candidate) > len(enclosing) {
			enclosing = candidate
		}
	}

	return enclosing
}

func FingerprintRemote(remote string) RemoteFingerprint {
	trimmed := strings.TrimSpace(remote)
	if trimmed == "" {
		return RemoteFingerprint{}
	}

	host, location := splitRemote(trimmed)
	if location == "" {
		return RemoteFingerprint{}
	}

	sum := sha256.Sum256([]byte(host + "/" + location))

	return RemoteFingerprint{
		Hash:     hex.EncodeToString(sum[:]),
		Host:     host,
		PathTail: tailOf(location),
	}
}

func splitRemote(remote string) (string, string) {
	if host, location, ok := splitSCP(remote); ok {
		return host, location
	}

	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme == "" {
		return "", cleanLocation(remote)
	}

	return strings.ToLower(parsed.Hostname()), cleanLocation(parsed.Path)
}

func splitSCP(remote string) (string, string, bool) {
	if strings.Contains(remote, "://") {
		return "", "", false
	}

	colon := strings.Index(remote, ":")
	if colon <= 0 || strings.Contains(remote[:colon], "/") {
		return "", "", false
	}

	host := remote[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	if host == "" {
		return "", "", false
	}

	return strings.ToLower(host), cleanLocation(remote[colon+1:]), true
}

func cleanLocation(location string) string {
	cleaned := strings.ToLower(strings.Trim(location, "/"))
	cleaned = strings.TrimSuffix(cleaned, ".git")

	return strings.Trim(cleaned, "/")
}

func tailOf(location string) string {
	segments := strings.Split(location, "/")
	if len(segments) > 2 {
		segments = segments[len(segments)-2:]
	}

	return path.Join(segments...)
}
