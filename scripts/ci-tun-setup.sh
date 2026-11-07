#!/usr/bin/env bash
# Prepares a Linux runner so the data-plane checks can actually run.
#
# A GitHub-hosted runner is a VM: the tun driver is usually available, but the
# device node is missing and the unprivileged runner user has no CAP_NET_ADMIN.
# This script creates the node and grants the capability to the two things that
# need it:
#
#   * python3, used by scripts/tun-check.sh to ask the kernel;
#   * ET_BIN (easytier-core), which creates the interface when the smoke runs.
#
# Granting a capability to an interpreter is acceptable on a disposable CI VM and
# must not be copied to a shared machine. Nothing here runs without sudo, and the
# script exits 3 when the runner cannot be provisioned, which callers must treat
# as "unverified", never as a pass.
set -uo pipefail
cd "$(dirname "$0")/.."

say() { echo "ci-tun-setup: $*"; }

if ! command -v sudo >/dev/null 2>&1; then
  say "no sudo on this runner; cannot provision"
  exit 3
fi

if ! grep -qw tun /proc/misc 2>/dev/null; then
  sudo modprobe tun >/dev/null 2>&1 || true
  if ! grep -qw tun /proc/misc 2>/dev/null; then
    say "the tun driver is not available in this kernel"
    exit 3
  fi
fi

if [ ! -c /dev/net/tun ]; then
  sudo mkdir -p /dev/net
  sudo mknod /dev/net/tun c 10 200 || { say "mknod /dev/net/tun failed"; exit 3; }
  sudo chmod 666 /dev/net/tun || true
  say "created /dev/net/tun"
fi

grant() {
  local target="$1"
  [ -z "$target" ] && return 0
  [ -x "$target" ] || return 0
  sudo setcap cap_net_admin+ep "$target" >/dev/null 2>&1 || true
}

PYTHON_BIN=$(readlink -f "$(command -v python3 || true)")
grant "$PYTHON_BIN"
ET_BIN_PATH=${ET_BIN:-}
if [ -n "$ET_BIN_PATH" ] && [ -x "$ET_BIN_PATH" ]; then
  grant "$(readlink -f "$ET_BIN_PATH")"
fi

# The only verdict that counts is the kernel's, asked the same way the gate asks it.
if ./scripts/tun-check.sh; then
  say "runner can create a tun interface"
  exit 0
fi
say "provisioning did not give an unprivileged process the capability"
exit 3
