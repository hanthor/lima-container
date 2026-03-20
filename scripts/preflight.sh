#!/usr/bin/env bash
set -Eeuo pipefail

warn() { printf 'WARN: %s\n' "$1" >&2; }
fail() { printf 'ERROR: %s\n' "$1" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || fail "Linux host is required."

# KVM checks (mirrors upstream qemus/qemu reset.sh detection logic)
if [ ! -e /dev/kvm ]; then
  warn "/dev/kvm not found; KVM acceleration unavailable - VM will run ~10x slower via TCG."
elif ! sh -c 'echo -n > /dev/kvm' 2>/dev/null; then
  warn "/dev/kvm is not writable; add --device /dev/kvm with correct permissions."
else
  # Verify CPU has virtualization extensions
  cpu_flags=$(awk '/^flags/{print;exit}' /proc/cpuinfo 2>/dev/null | sed 's/^[^:]*: //' || true)
  if ! grep -qw "vmx\|svm" <<< "${cpu_flags:-}"; then
    warn "CPU vmx/svm flags not found; KVM may not be available (check BIOS virtualization settings)."
  else
    printf 'KVM acceleration available (hardware virtualization active).\n'
  fi
fi

[ -e /dev/net/tun ] || warn "/dev/net/tun not found; some Lima networking modes may fail."

if [ -r /proc/sys/kernel/unprivileged_userns_clone ]; then
  value="$(cat /proc/sys/kernel/unprivileged_userns_clone)"
  [ "$value" = "1" ] || warn "kernel.unprivileged_userns_clone is $value (expected 1)."
fi

if [ -r /sys/fs/cgroup/cgroup.controllers ]; then
  :
else
  warn "cgroup v2 controller file missing; rootless Lima features may fail."
fi

printf 'Preflight checks completed.\n'
