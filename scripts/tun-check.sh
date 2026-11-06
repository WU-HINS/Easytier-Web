#!/usr/bin/env bash
# Reports whether this machine can create a tun interface, which is what the
# data-plane checks (scripts/overlay-smoke.sh) need.
#
#   0  capable: a tun device can be opened and configured
#   3  not capable: the reason is printed on stdout, callers must treat it as
#      "unverified", never as a pass
#   1  unexpected error (the check itself could not run)
#
# The distinction matters: "no tun" and "the test passed" look identical if you
# only look at an exit code of a wrapper that swallows both.
set -uo pipefail

if ! grep -qw tun /proc/misc 2>/dev/null; then
  echo "the tun driver is not loaded (no entry in /proc/misc)"
  exit 3
fi

if [ ! -c /dev/net/tun ]; then
  echo "no character device at /dev/net/tun"
  exit 3
fi

python3 - <<'PY'
import fcntl, os, struct, sys
try:
    fd = os.open('/dev/net/tun', os.O_RDWR)
except OSError as err:
    print('cannot open /dev/net/tun: %s' % err.strerror)
    sys.exit(3)
try:
    ifr = struct.pack('16sH', b'etcheck0', 0x0001)
    fcntl.ioctl(fd, 0x400454ca, ifr)   # TUNSETIFF
except OSError as err:
    if err.errno == 1:
        print('the tun driver is present but this process lacks CAP_NET_ADMIN (TUNSETIFF: EPERM)')
    else:
        print('TUNSETIFF failed: %s' % err.strerror)
    sys.exit(3)
finally:
    os.close(fd)
print('a tun interface can be created')
sys.exit(0)
PY
status=$?
exit $status
