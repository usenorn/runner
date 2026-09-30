package entity

import (
	"path/filepath"
	"slices"
	"strings"
)

const (
	RunHomeDir = "home"
	RunTmpDir  = "tmp"

	GitConfigFile = ".gitconfig"
)

type RunHome struct {
	Root string
	Tmp  string
}

func RunHomeOf(runDir string) RunHome {
	return RunHome{
		Root: filepath.Join(runDir, RunHomeDir),
		Tmp:  filepath.Join(runDir, RunTmpDir),
	}
}

func (h RunHome) Claude() string {
	return filepath.Join(h.Root, ".claude")
}

func (h RunHome) Plans() string {
	return filepath.Join(h.Claude(), "plans")
}

func (h RunHome) GitConfig() string {
	return filepath.Join(h.Root, GitConfigFile)
}

func (h RunHome) variables() []string {
	return []string{
		"HOME=" + h.Root,
		"TMPDIR=" + h.Tmp,
		"XDG_CONFIG_HOME=" + filepath.Join(h.Root, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(h.Root, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(h.Root, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(h.Root, ".local", "state"),
		"CLAUDE_CONFIG_DIR=" + h.Claude(),
	}
}

type Toolchain struct {
	Variable string
	Dir      string
	Caches   []string
}

func Toolchains() []Toolchain {
	return []Toolchain{
		{Variable: "GOPATH", Dir: "go", Caches: []string{filepath.Join("pkg", "mod")}},
		{Variable: "CARGO_HOME", Dir: ".cargo", Caches: []string{"registry", "git"}},
		{Variable: "RUSTUP_HOME", Dir: ".rustup"},
		{Variable: "NVM_DIR", Dir: ".nvm"},
		{Variable: "npm_config_cache", Dir: ".npm", Caches: []string{"."}},
		{Variable: "PYENV_ROOT", Dir: ".pyenv"},
		{Variable: "RBENV_ROOT", Dir: ".rbenv"},
		{Variable: "ASDF_DATA_DIR", Dir: ".asdf"},
		{Variable: "MISE_DATA_DIR", Dir: filepath.Join(".local", "share", "mise")},
		{Variable: "SDKMAN_DIR", Dir: ".sdkman"},
	}
}

func ambient() []string {
	return []string{
		"HOME", "TMPDIR", "TMP", "TEMP", "CLAUDE_CONFIG_DIR",
		"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"SSH_AUTH_SOCK", "SSH_AGENT_PID", "SSH_ASKPASS", "GIT_ASKPASS", "GIT_SSH", "GIT_SSH_COMMAND",
		"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
		"GITLAB_TOKEN", "GLAB_TOKEN", "BITBUCKET_TOKEN",
	}
}

func ambientPrefixes() []string {
	return []string{"XDG_", "NORN_", "GIT_CONFIG_"}
}

func TaskEnvironment(
	runtime Runtime,
	host []string,
	hostHome string,
	home RunHome,
	exists func(path string) bool,
) []string {
	if runtime == RuntimeDocker {
		return append(home.variables(), "HOST=0.0.0.0")
	}

	kept := make([]string, 0, len(host)+len(Toolchains())+len(home.variables()))
	set := map[string]bool{}

	for _, entry := range host {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(ambient(), name) || hasAnyPrefix(name, ambientPrefixes()) {
			continue
		}

		set[name] = true
		kept = append(kept, entry)
	}

	for _, toolchain := range Toolchains() {
		dir := filepath.Join(hostHome, toolchain.Dir)
		if set[toolchain.Variable] || hostHome == "" || !exists(dir) {
			continue
		}

		kept = append(kept, toolchain.Variable+"="+dir)
	}

	return append(kept, home.variables()...)
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

func HostGitConfigs(hostHome, hostConfigHome string) []string {
	if hostConfigHome == "" {
		hostConfigHome = filepath.Join(hostHome, ".config")
	}

	return []string{filepath.Join(hostConfigHome, "git", "config"), filepath.Join(hostHome, GitConfigFile)}
}

func TaskGitConfig(includes []string) string {
	var config strings.Builder

	config.WriteString("[include]\n")

	for _, path := range includes {
		config.WriteString("\tpath = " + path + "\n")
	}

	config.WriteString("[credential]\n\thelper =\n")

	return config.String()
}
