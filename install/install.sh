#!/usr/bin/env bash
set -euo pipefail

REPO=${NORN_RUNNER_REPO:-usenorn/runner}
RELEASES_URL=${NORN_RUNNER_RELEASES_URL:-https://github.com/$REPO/releases}
BINARY=norn

VERSION=${NORN_RUNNER_VERSION:-}
PREFIX=${NORN_RUNNER_PREFIX:-}
TOKEN=${NORN_TOKEN:-}
COMMAND=install
SERVICE=yes
WORK=

say() { printf '  %s\n' "$*"; }
step() { printf '  %-28s' "$*"; }
done_() { printf 'done\n'; }
die() {
	printf '\nerror: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
usage: install.sh [options]

  Installs the Norn Runner, registers it with this machine's service manager,
  and connects it to Norn when a token is given.

  --version <v>     install a particular release, for example v0.1.0
  --prefix <dir>    install the binary here instead of /usr/local/bin
  --token <token>   connect to Norn with this agent token once installed
  --no-service      install the binary only; do not register a service
  --uninstall       disconnect, unregister the service and remove the binary

  NORN_TOKEN is read when --token is not given, and is the safer way to pass
  one: an argument is visible to anyone who can run ps while this is running.
USAGE
}

have() { command -v "$1" >/dev/null 2>&1; }

cleanup() {
	if [ -n "$WORK" ]; then
		rm -rf "$WORK"
	fi
}

trap cleanup EXIT

while [ $# -gt 0 ]; do
	case $1 in
	--version)
		VERSION=${2:-}
		shift 2
		;;
	--prefix)
		PREFIX=${2:-}
		shift 2
		;;
	--token)
		TOKEN=${2:-}
		shift 2
		;;
	--no-service)
		SERVICE=no
		shift
		;;
	--uninstall)
		COMMAND=uninstall
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown option $1; run with --help" ;;
	esac
done

platform() {
	local os arch
	os=$(uname -s)
	arch=$(uname -m)

	case $os in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) die "the runner does not run on $os yet; macOS and Linux are supported" ;;
	esac

	case $arch in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "the runner has no build for $arch; amd64 and arm64 are supported" ;;
	esac

	printf '%s_%s' "$os" "$arch"
}

latest_version() {
	local resolved
	resolved=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest") ||
		die "could not ask $RELEASES_URL which release is current"

	case $resolved in
	*/tag/*) printf '%s' "${resolved##*/tag/}" ;;
	*) die "$RELEASES_URL has no stable release yet. Prereleases are not installed by default; name one with --version" ;;
	esac
}

sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d' ' -f1
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{ print $NF }'
	else
		die "no sha256 tool on this machine, so the download cannot be verified"
	fi
}

install_dir() {
	if [ -n "$PREFIX" ]; then
		printf '%s' "$PREFIX"
		return
	fi

	if [ -w /usr/local/bin ]; then
		printf '/usr/local/bin'
		return
	fi

	printf '%s/.local/bin' "$HOME"
}

place() {
	local from=$1 to=$2 dir
	dir=$(dirname "$to")

	mkdir -p "$dir" 2>/dev/null || true

	if [ -w "$dir" ]; then
		cp "$from" "$to.incoming"
		chmod 0755 "$to.incoming"
		mv -f "$to.incoming" "$to"
		return
	fi

	have sudo || die "$dir is not writable and sudo is not available"

	sudo mkdir -p "$dir"
	sudo cp "$from" "$to.incoming"
	sudo chmod 0755 "$to.incoming"
	sudo mv -f "$to.incoming" "$to"
}

remove() {
	local target=$1

	[ -e "$target" ] || return 0

	if [ -w "$(dirname "$target")" ]; then
		rm -f "$target"
		return
	fi

	have sudo || die "$target is left behind: its directory is not writable and sudo is not available"

	sudo rm -f "$target"
}

await_daemon() {
	local binary=$1 waited=0

	while [ "$waited" -lt 30 ]; do
		if "$binary" runner status >/dev/null 2>&1; then
			return 0
		fi

		sleep 1
		waited=$((waited + 1))
	done

	return 1
}

do_install() {
	have curl || die "curl is not installed"
	have tar || die "tar is not installed"

	local target
	target=$(platform)

	if [ -z "$VERSION" ]; then
		step "finding the newest release"
		VERSION=$(latest_version)
		done_
	fi

	local number=${VERSION#v}
	local archive="norn-runner_${number}_${target}.tar.gz"
	local download="$RELEASES_URL/download/$VERSION"
	WORK=$(mktemp -d)

	step "downloading $VERSION"
	curl -fsSL -o "$WORK/$archive" "$download/$archive" ||
		die "could not download $archive from $download"
	curl -fsSL -o "$WORK/checksums.txt" "$download/checksums.txt" ||
		die "could not download the checksums from $download"
	done_

	step "verifying the download"
	local expected actual
	expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$WORK/checksums.txt")
	[ -n "$expected" ] || die "checksums.txt has no entry for $archive"

	actual=$(sha256_of "$WORK/$archive")
	[ "$expected" = "$actual" ] ||
		die "$archive does not match its published checksum, so it was not downloaded intact"
	done_

	tar -xzf "$WORK/$archive" -C "$WORK"
	[ -f "$WORK/$BINARY" ] || die "$archive does not contain a $BINARY binary"

	local dir binary
	dir=$(install_dir)
	binary="$dir/$BINARY"

	step "installing the binary"
	place "$WORK/$BINARY" "$binary"
	done_

	say "installed at $binary"

	case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "$dir is not on your PATH; add it, or run $binary directly" ;;
	esac

	if [ "$SERVICE" = no ]; then
		say "installed $("$binary" --version). Register the service with '$BINARY runner install'"
		return
	fi

	step "registering the service"
	"$binary" runner install >/dev/null
	done_

	await_daemon "$binary" ||
		die "the service was registered but no runner answered; check '$BINARY runner status'"

	if [ -z "$TOKEN" ]; then
		say "installed $("$binary" --version)"
		say "connect it with: $BINARY runner connect --token nrn_…"
		return
	fi

	step "connecting to norn"
	NORN_TOKEN="$TOKEN" "$binary" runner connect >/dev/null ||
		die "the runner is installed but could not connect; run '$BINARY runner connect --token …'"
	done_

	"$binary" runner status
}

do_uninstall() {
	local dir binary
	dir=$(install_dir)
	binary="$dir/$BINARY"

	if [ ! -x "$binary" ]; then
		if have "$BINARY"; then
			binary=$(command -v "$BINARY")
		else
			die "no $BINARY binary found to remove"
		fi
	fi

	step "disconnecting"
	"$binary" runner disconnect >/dev/null 2>&1 || true
	done_

	step "unregistering the service"
	"$binary" runner uninstall >/dev/null 2>&1 || true
	done_

	step "removing the binary"
	remove "$binary"
	done_

	say "removed $binary"

	say "the runner is gone from this machine. Norn still lists it; revoke it there to retire it"
}

printf '\n  Norn Runner\n\n'

case $COMMAND in
install) do_install ;;
uninstall) do_uninstall ;;
esac

printf '\n'
