#!/bin/sh
set -eu

case "$(uname -s)" in
  Linux) ;;
  *)
    echo "systemd check skipped: non-Linux host"
    exit 0
    ;;
esac

if ! command -v systemd-analyze >/dev/null 2>&1; then
  echo "systemd check skipped: systemd-analyze is unavailable"
  exit 0
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(dirname "$script_dir")
unit="$root_dir/packaging/systemd/autozeagent.service"

require_line() {
  if ! grep -Fqx "$1" "$unit"; then
    echo "systemd check failed: missing '$1' in $unit" >&2
    exit 1
  fi
}

require_line "User=autozeagent"
require_line "Group=autozeagent"
require_line "KillMode=control-group"
require_line "RuntimeDirectory=autozeagent"
require_line "RuntimeDirectoryMode=0750"
require_line "StateDirectory=autozeagent"
require_line "PrivateTmp=true"
require_line "NoNewPrivileges=true"
require_line "ExecStartPre=/usr/local/bin/autozeagent config validate --mode system"
require_line "ExecStart=/usr/local/bin/autozeagentd --mode system"
require_line "ExecStartPost=/usr/local/bin/autozeagent health --mode system"

verify_dir=$(mktemp -d)
trap 'rm -rf "$verify_dir"' 0 HUP INT TERM
verify_unit="$verify_dir/autozeagent.service"

# systemd-analyze also checks host-specific users, directories, and Exec*
# binaries. Replace only those values in the temporary copy; canonical
# values are checked above before host-independent unit verification.
sed \
  -e 's#^User=.*#User=root#' \
  -e 's#^Group=.*#Group=root#' \
  -e 's#^WorkingDirectory=.*#WorkingDirectory=/#' \
  -e 's#^ExecStartPre=.*#ExecStartPre=/bin/true#' \
  -e 's#^ExecStart=.*#ExecStart=/bin/true#' \
  -e 's#^ExecStartPost=.*#ExecStartPost=/bin/true#' \
  "$unit" > "$verify_unit"

systemd-analyze verify "$verify_unit"
echo "systemd unit check passed: $unit"
