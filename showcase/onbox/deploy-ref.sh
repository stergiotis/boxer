#!/bin/sh
# Operator out-of-cadence deploy of an EXACT revision (ADR-0085 SD10), run in the
# SAME systemd unit env + sandbox as the poll timer — so the on-box build finds
# the toolchain (no hand-reconstructed PATH/RUSTUP_HOME, the `cargo: command not
# found` trap). A thin wrapper over `systemctl start imzero2-deploy@<ref>`.
#
#   sudo /opt/imzero2/deploy-ref.sh <commit|branch|tag>          # verified (signed commit)
#   sudo /opt/imzero2/deploy-ref.sh --break-glass <commit|...>   # UNSIGNED, interim escape
#
# The unit is Type=oneshot, so `systemctl start` blocks until the deploy finishes
# and propagates its exit status. The deploy's log goes to the journal (the unit
# below), shown here on failure. The release is named by `git describe`, which is
# its version floor: the next NEWER signed tag supersedes it on the following
# poll, but a re-tag of the base or an older tag will not revert it (SD12).
set -eu

unit_base=imzero2-deploy
case "${1:-}" in
	--break-glass) unit_base=imzero2-deploy-breakglass; shift ;;
	-*) echo "deploy-ref: unknown option '$1'" >&2; exit 2 ;;
esac
if [ "$#" -ne 1 ] || [ -z "${1:-}" ]; then
	echo "usage: $0 [--break-glass] <commit|branch|tag>" >&2
	exit 2
fi

ref=$1
inst=$(systemd-escape -- "$ref")
unit="${unit_base}@${inst}.service"

echo "deploy-ref: starting ${unit} (ref=${ref}) — same env + sandbox as the timer" >&2
if systemctl start "$unit"; then
	echo "deploy-ref: ${ref} deployed" >&2
else
	rc=$?
	echo "deploy-ref: ${ref} FAILED (rc=${rc}); recent log:" >&2
	journalctl -u "$unit" --no-pager -n 40 >&2 || true
	exit "$rc"
fi
