#!/bin/bash
# Install a pinned rclone release binary, checked against a pinned SHA-256.
#
#   scripts/dev/install-rclone.sh [<dir>]     # default: ~/.local/bin
#
# The lading rclone head (`boxer fs sftp-stdio`, doc/howto/lading-snapshot-store.md
# §6) needs the sftp backend's `ssh` option, which arrived in rclone 1.64.
# Distro packages can be older -- Ubuntu's 1.60 ignores the option and dials
# an ssh server instead -- so this fetches the release the store was measured
# against. Bumping the version means replacing every checksum below with the
# lines from https://downloads.rclone.org/v<version>/SHA256SUMS.
set -euo pipefail

version=v1.74.3
dir=${1:-$HOME/.local/bin}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=osx ;;
    *) printf 'unsupported OS: %s\n' "$(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) printf 'unsupported architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac
name="rclone-$version-$os-$arch"
case "$name" in
    rclone-v1.74.3-linux-amd64) checksum=dbee7ccd7a5d617e4ed4cd4555c16669b511abfe8d31164f61be35ac9e999bd2 ;;
    rclone-v1.74.3-linux-arm64) checksum=8f8d47446e061f80c3256659fe8e21f56d72d96aaefe1275d088ea5eb6b42aa7 ;;
    rclone-v1.74.3-osx-amd64) checksum=417cabd402d57806d597bd0ba8fb33a434ca8c2a1a5aa98de5a0bd4b52b39202 ;;
    rclone-v1.74.3-osx-arm64) checksum=33a435ab17023b686918ce9a3975aceb75fe1796c694f38f1993024be1f063f5 ;;
esac

missing=0
for tool in curl unzip; do
    if ! command -v "$tool" >/dev/null; then printf 'missing prerequisite: %s\n' "$tool" >&2; missing=1; fi
done
if command -v sha256sum >/dev/null; then
    sha256=(sha256sum)
elif command -v shasum >/dev/null; then
    sha256=(shasum -a 256)
else
    printf 'missing prerequisite: sha256sum or shasum\n' >&2; missing=1
fi
[[ "$missing" == 0 ]] || exit 1

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl --fail --silent --show-error --location --retry 2 -o "$tmp/$name.zip" "https://downloads.rclone.org/$version/$name.zip"
printf '%s  %s\n' "$checksum" "$tmp/$name.zip" | "${sha256[@]}" --check
unzip -q "$tmp/$name.zip" -d "$tmp"
mkdir -p "$dir"
install -m 0755 "$tmp/$name/rclone" "$dir/rclone"
printf 'installed %s\n' "$("$dir/rclone" version | head -1)" >&2

# A distro rclone earlier on PATH would still be the one `rclone mount` runs.
found=$(command -v rclone || true)
if [[ "$found" != "$dir/rclone" ]]; then
    printf 'note: `rclone` on PATH resolves to %s, not %s; put %s first in PATH\n' \
        "${found:-nothing}" "$dir/rclone" "$dir" >&2
fi
# A shell that ran rclone before keeps the old path in its hash table, and
# `which` does not consult it: `type rclone` shows what that shell will run.
printf 'note: in a shell that has already run rclone, run `hash -r` first\n' >&2
