package entity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	ErrToolchainUnusable = errors.New("this machine cannot build what this run needs")
	ErrMachineNotReady   = errors.New("this machine is not ready to take work; fix what is listed above")
)

const (
	ManifestScanDepth  = 1
	ToolVersionLineMax = 200
	SnapBinDir         = "/snap/"
)

type BuildTool struct {
	Name    string
	Version []string
	Install string
}

var (
	toolGo = BuildTool{
		Name:    "go",
		Version: []string{"go", "version"},
		Install: "install Go from the official tarball at https://go.dev/doc/install, or with apt",
	}
	toolNode = BuildTool{
		Name:    "node",
		Version: []string{"node", "--version"},
		Install: "install Node.js from https://nodejs.org or with your distribution's package manager",
	}
	toolNPM = BuildTool{
		Name:    "npm",
		Version: []string{"npm", "--version"},
		Install: "npm comes with Node.js; install Node.js from https://nodejs.org",
	}
	toolPNPM = BuildTool{
		Name:    "pnpm",
		Version: []string{"pnpm", "--version"},
		Install: "run `corepack enable pnpm`, or `npm install -g pnpm`",
	}
	toolYarn = BuildTool{
		Name:    "yarn",
		Version: []string{"yarn", "--version"},
		Install: "run `corepack enable yarn`, or `npm install -g yarn`",
	}
	toolBun = BuildTool{
		Name:    "bun",
		Version: []string{"bun", "--version"},
		Install: "run `curl -fsSL https://bun.sh/install | bash`",
	}
	toolCargo = BuildTool{
		Name:    "cargo",
		Version: []string{"cargo", "--version"},
		Install: "install Rust with rustup from https://rustup.rs",
	}
	toolMake = BuildTool{
		Name:    "make",
		Version: []string{"make", "--version"},
		Install: "install make, for example `sudo apt install make`",
	}
	toolPython = BuildTool{
		Name:    "python3",
		Version: []string{"python3", "--version"},
		Install: "install Python 3, for example `sudo apt install python3`",
	}
	toolUV = BuildTool{
		Name:    "uv",
		Version: []string{"uv", "--version"},
		Install: "run `curl -LsSf https://astral.sh/uv/install.sh | sh`",
	}
	toolBundler = BuildTool{
		Name:    "bundle",
		Version: []string{"bundle", "--version"},
		Install: "install Ruby and run `gem install bundler`",
	}
)

func ManifestFiles() []string {
	return []string{
		"go.mod", "package.json", "bun.lock", "bun.lockb", "pnpm-lock.yaml", "yarn.lock",
		"package-lock.json", "Cargo.toml", "Makefile", "GNUmakefile", "pyproject.toml",
		"requirements.txt", "uv.lock", "Gemfile",
	}
}

type ManifestRoot struct {
	RelPath string
	Path    string
}

type Manifest struct {
	RelPath        string
	Files          []string
	PackageManager string
}

func (m Manifest) has(name string) bool {
	return slices.Contains(m.Files, name)
}

type Requirement struct {
	Tool   BuildTool
	Needed []string
}

func Requirements(manifests []Manifest) []Requirement {
	var required []Requirement

	need := func(tool BuildTool, relPath string) {
		at := slices.IndexFunc(required, func(held Requirement) bool { return held.Tool.Name == tool.Name })
		if at == -1 {
			required = append(required, Requirement{Tool: tool, Needed: []string{relPath}})

			return
		}

		if !slices.Contains(required[at].Needed, relPath) {
			required[at].Needed = append(required[at].Needed, relPath)
		}
	}

	for _, manifest := range manifests {
		for _, tool := range toolsFor(manifest) {
			need(tool, manifest.RelPath)
		}
	}

	return required
}

func toolsFor(manifest Manifest) []BuildTool {
	var tools []BuildTool

	if manifest.has("go.mod") {
		tools = append(tools, toolGo)
	}

	if manifest.has("package.json") {
		tools = append(tools, javascriptTools(manifest)...)
	}

	if manifest.has("Cargo.toml") {
		tools = append(tools, toolCargo)
	}

	if manifest.has("Makefile") || manifest.has("GNUmakefile") {
		tools = append(tools, toolMake)
	}

	if manifest.has("uv.lock") {
		tools = append(tools, toolUV)
	} else if manifest.has("pyproject.toml") || manifest.has("requirements.txt") {
		tools = append(tools, toolPython)
	}

	if manifest.has("Gemfile") {
		tools = append(tools, toolBundler)
	}

	return tools
}

func javascriptTools(manifest Manifest) []BuildTool {
	manager, _, _ := strings.Cut(manifest.PackageManager, "@")

	switch {
	case manager == "bun" || manifest.has("bun.lock") || manifest.has("bun.lockb"):
		return []BuildTool{toolBun}
	case manager == "pnpm" || manifest.has("pnpm-lock.yaml"):
		return []BuildTool{toolNode, toolPNPM}
	case manager == "yarn" || manifest.has("yarn.lock"):
		return []BuildTool{toolNode, toolYarn}
	default:
		return []BuildTool{toolNode, toolNPM}
	}
}

type ToolState string

const (
	ToolReady   ToolState = "ready"
	ToolMissing ToolState = "missing"
	ToolSnap    ToolState = "snap"
	ToolBroken  ToolState = "broken"
)

type ToolCheck struct {
	Requirement
	State   ToolState
	Path    string
	Version string
	Detail  string
}

func (c ToolCheck) Usable() bool {
	return c.State == ToolReady
}

func (c ToolCheck) Line() string {
	needed := strings.Join(c.Needed, ", ")

	switch c.State {
	case ToolReady:
		return fmt.Sprintf("%s: %s (for %s)", c.Tool.Name, c.Version, needed)
	case ToolMissing:
		return fmt.Sprintf("%s is not installed, and %s needs it: %s", c.Tool.Name, needed, c.Tool.Install)
	case ToolSnap:
		return fmt.Sprintf(
			"%s is installed as a snap (%s), and a snap cannot start inside the sandbox runs work "+
				"in; %s needs it, so remove the snap and %s",
			c.Tool.Name, c.Path, needed, c.Tool.Install,
		)
	default:
		return fmt.Sprintf(
			"%s is installed at %s but does not run inside the sandbox (%s); %s needs it, so %s",
			c.Tool.Name, c.Path, c.Detail, needed, c.Tool.Install,
		)
	}
}

func InstalledAsSnap(path string) bool {
	return strings.HasPrefix(path, SnapBinDir)
}

type ToolchainReport []ToolCheck

func (r ToolchainReport) Unusable() []ToolCheck {
	var broken []ToolCheck

	for _, check := range r {
		if !check.Usable() {
			broken = append(broken, check)
		}
	}

	return broken
}

func (r ToolchainReport) Problem() error {
	broken := r.Unusable()
	if len(broken) == 0 {
		return nil
	}

	lines := make([]string, 0, len(broken))
	for _, check := range broken {
		lines = append(lines, check.Line())
	}

	return fmt.Errorf(
		"%w. Fix these on the machine, check with `norn runner doctor`, and start the run again:\n- %s",
		ErrToolchainUnusable, strings.Join(lines, "\n- "),
	)
}

func (r ToolchainReport) Ready() string {
	var ready []string

	for _, check := range r {
		if check.Usable() {
			ready = append(ready, fmt.Sprintf("%s (%s)", check.Tool.Name, check.Version))
		}
	}

	if len(ready) == 0 {
		return ""
	}

	return "this run builds with " + strings.Join(ready, ", ")
}

func ToolVersion(output string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")

	if len(line) > ToolVersionLineMax {
		return line[:ToolVersionLineMax]
	}

	return line
}

type ToolchainProbe struct {
	Box         Sandbox
	Workdir     string
	Environment []string
	Roots       []ManifestRoot
}

func ManifestRootsOf(snapshot Snapshot) []ManifestRoot {
	roots := make([]ManifestRoot, 0, len(snapshot.Repositories))

	for _, repository := range snapshot.Repositories {
		roots = append(roots, ManifestRoot{RelPath: repository.RelPath, Path: repository.Path})
	}

	return roots
}

type DoctorCodebase struct {
	Name    string
	Root    string
	Report  ToolchainReport
	Failure string
}

type Doctor struct {
	Sandbox      string
	Identity     GitIdentity
	PullRequests ForgeKind
	Codebases    []DoctorCodebase
}

func (d Doctor) Healthy() bool {
	if d.Sandbox != "" || !d.Identity.Complete() || d.PullRequests == "" {
		return false
	}

	for _, codebase := range d.Codebases {
		if codebase.Failure != "" || len(codebase.Report.Unusable()) > 0 {
			return false
		}
	}

	return true
}
