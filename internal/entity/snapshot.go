package entity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RunWorkspaceDir  = "workspace"
	RunMetadataDir   = "metadata"
	SnapshotPatchDir = "local-changes"

	RepositoryRoot  = "."
	BranchNamespace = "norn"
	IgnoreFileName  = ".nornignore"
	SettingsDir     = ".norn"
	SettingsFile    = "codebase.yaml"
	PlanFile        = "run-plan.yaml"

	ShortSHALength = 12
)

var (
	ErrSnapshotExists         = errors.New("that snapshot already exists; remove it before taking another")
	ErrSnapshotMissing        = errors.New("there is no snapshot by that name")
	ErrSnapshotEmpty          = errors.New("this folder holds nothing that can be snapshotted")
	ErrSnapshotBaseMissing    = errors.New("that repository has no commit to start from")
	ErrSnapshotDirtyConflict  = errors.New("the local changes do not apply onto the base commit")
	ErrSnapshotTooLarge       = errors.New("this folder holds more than norn will copy into an execution")
	ErrSnapshotIssueKeyEmpty  = errors.New("a snapshot needs the key of the issue it is for")
	ErrSnapshotWorktreeExists = errors.New("that branch is already checked out somewhere else")
)

type GitMode string

const (
	GitModeWorktree GitMode = "worktree"
	GitModeClone    GitMode = "clone"
)

func GitModes() []GitMode {
	return []GitMode{GitModeWorktree, GitModeClone}
}

func (m GitMode) Valid() bool {
	return slices.Contains(GitModes(), m)
}

type BasePolicy string

const (
	BaseOriginDefault BasePolicy = "origin/default"
	BaseHead          BasePolicy = "head"
)

func BasePolicies() []BasePolicy {
	return []BasePolicy{BaseOriginDefault, BaseHead}
}

func (p BasePolicy) Valid() bool {
	return slices.Contains(BasePolicies(), p)
}

type LocalChanges string

const (
	LocalChangesExclude LocalChanges = "exclude"
	LocalChangesInclude LocalChanges = "include"
)

func LocalChangesChoices() []LocalChanges {
	return []LocalChanges{LocalChangesExclude, LocalChangesInclude}
}

func (c LocalChanges) Valid() bool {
	return slices.Contains(LocalChangesChoices(), c)
}

type MaterialiseMethod string

const (
	MaterialiseReflink MaterialiseMethod = "reflink"
	MaterialiseCopy    MaterialiseMethod = "copy"
	MaterialiseLink    MaterialiseMethod = "symlink"
)

type SnapshotPolicy struct {
	GitMode      GitMode
	Base         BasePolicy
	LocalChanges LocalChanges
	Fetch        bool
}

type LocalPatch struct {
	BaseSHA   string
	Commit    string
	PatchFile string
	Files     int
}

type SnapshotRepository struct {
	Name    string
	RelPath string
	Kind    RepositoryKind
	Source  string
	Path    string
	Mode    GitMode
	Base    BasePolicy
	BaseSHA string
	Branch  string
	Local   *LocalPatch
}

type SharedFile struct {
	RelPath string
	Method  MaterialiseMethod
	Size    int64
}

type Snapshot struct {
	Name         string
	Run          string
	Workspace    string
	IssueKey     string
	Branch       string
	Attempt      int
	CodebaseID   uuid.UUID
	CodebaseRoot string
	Repositories []SnapshotRepository
	Shared       []SharedFile
	Bytes        int64
	Warnings     []string
	TakenAt      time.Time
	Took         time.Duration
}

func BranchFor(issueKey, repository string, attempt int) string {
	branch := BranchNamespace + "/" + sanitiseRef(issueKey) + "/" + sanitiseRef(repository)

	if attempt > 1 {
		branch += fmt.Sprintf("-r%d", attempt)
	}

	return branch
}

func RunNameFor(issueKey string, attempt int) string {
	return fmt.Sprintf("snap-%s-%d", sanitiseRef(issueKey), max(attempt, 1))
}

func LocalChangesMessage(sha string) string {
	return "norn: local changes at " + ShortSHA(sha)
}

func ShortSHA(sha string) string {
	if len(sha) <= ShortSHALength {
		return sha
	}

	return sha[:ShortSHALength]
}

func sanitiseRef(value string) string {
	var builder strings.Builder

	for _, letter := range value {
		switch {
		case letter >= 'a' && letter <= 'z',
			letter >= 'A' && letter <= 'Z',
			letter >= '0' && letter <= '9',
			letter == '.', letter == '_', letter == '-':
			builder.WriteRune(letter)
		default:
			builder.WriteRune('-')
		}
	}

	cleaned := builder.String()

	for strings.Contains(cleaned, "..") {
		cleaned = strings.ReplaceAll(cleaned, "..", ".")
	}

	cleaned = strings.TrimSuffix(strings.Trim(cleaned, "-."), ".lock")

	if cleaned = strings.Trim(cleaned, "-."); cleaned == "" {
		return "repository"
	}

	return cleaned
}
