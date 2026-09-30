package sandbox

import (
	"strings"
)

const (
	seatbeltBinary = "/usr/bin/sandbox-exec"
	bwrapBinary    = "bwrap"
	dnsSocket      = "/private/var/run/mDNSResponder"
	resolverDir    = "/run/systemd/resolve"
)

type hostPolicy struct {
	home      string
	state     string
	socket    string
	readable  []string
	writable  []string
	protected []string
}

func seatbelt(policy hostPolicy) string {
	var profile strings.Builder

	profile.WriteString("(version 1)\n(allow default)\n")

	rule(&profile, "deny", "file-read-data file-read-xattr", subpaths(policy.home, policy.state))
	rule(&profile, "allow", "file-read-data file-read-xattr", subpaths(append(policy.readable, policy.writable...)...))
	rule(&profile, "deny", "file-write*", subpaths("/"))
	rule(&profile, "allow", "file-write*", subpaths(
		append([]string{"/dev", "/private/tmp", "/private/var/folders"}, policy.writable...)...,
	))
	rule(&profile, "deny", "file-write*", subpaths(policy.protected...))

	profile.WriteString("(deny network-outbound (remote unix-socket))\n")
	for _, socket := range []string{dnsSocket, policy.socket} {
		profile.WriteString("(allow network-outbound (remote unix-socket (path-literal " + quoted(socket) + ")))\n")
	}
	profile.WriteString("(deny mach-lookup (global-name-regex #\"^com\\.apple\\.security\") " +
		"(global-name \"com.apple.SecurityServer\") " +
		"(global-name \"com.apple.coreservices.launchservicesd\"))\n")
	profile.WriteString("(deny appleevent-send)\n")

	return profile.String()
}

func rule(profile *strings.Builder, verdict, operations string, filters []string) {
	if len(filters) == 0 {
		return
	}

	profile.WriteString("(" + verdict + " " + operations + " " + strings.Join(filters, " ") + ")\n")
}

func subpaths(paths ...string) []string {
	filters := make([]string, 0, len(paths))

	for _, path := range paths {
		if path != "" {
			filters = append(filters, "(subpath "+quoted(path)+")")
		}
	}

	return filters
}

func quoted(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
}

func bubblewrap(policy hostPolicy, binary string, command []string) []string {
	args := []string{
		binary, "--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
		"--tmpfs", "/tmp", "--tmpfs", "/run", "--ro-bind-try", resolverDir, resolverDir,
		"--tmpfs", policy.home,
	}

	if policy.state != "" {
		args = append(args, "--tmpfs", policy.state)
	}

	for _, path := range policy.readable {
		args = append(args, "--ro-bind-try", path, path)
	}

	for _, path := range policy.writable {
		args = append(args, "--bind-try", path, path)
	}

	if policy.socket != "" {
		args = append(args, "--bind-try", policy.socket, policy.socket)
	}

	for _, path := range policy.protected {
		args = append(args, "--ro-bind", path, path)
	}

	return append(append(args, "--"), command...)
}
