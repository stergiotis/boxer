#!/bin/bash
# M0 check 11 — the three rclone contracts the SFTP-over-stdio head depends on.
#
# rclone stands in for our own head here: `rclone serve sftp --stdio` is a
# working SFTP-over-stdio server, so driving rclone's sftp *backend* against
# it exercises exactly the wire contract M5's head will have to meet.
set -euo pipefail
work=${M0_WORKDIR:-$(mktemp -d)}
mkdir -p "$work/mount/a/b"
printf 'hello\nworld\n' > "$work/mount/a/b/f.txt"
printf 'zzz'            > "$work/mount/top.md"
ln -sf a/b/f.txt "$work/mount/link.txt"

# The head under test, with the invocations rclone makes recorded.
cat > "$work/head.sh" <<EOF
#!/bin/sh
{ printf 'INVOCATION:'; printf ' [%s]' "\$@"; printf '\n'; } >> $work/argv.txt
exec rclone serve sftp --stdio ${1:-$work/mount}
EOF
cat > "$work/head_filtered.sh" <<EOF
#!/bin/sh
exec rclone serve sftp --stdio --exclude '*.md' $work/mount
EOF
cat > "$work/head_links.sh" <<EOF
#!/bin/sh
exec rclone serve sftp --stdio --links $work/mount
EOF
chmod +x "$work"/head*.sh

echo "== 11a: what rclone's sftp backend runs in place of ssh =="
rm -f "$work/argv.txt"
rclone lsd --transfers 1 ":sftp,ssh=$work/head.sh:" 2>&1 | grep -v NOTICE || true
echo "-- invocations without shell_type --"; cat "$work/argv.txt"
rm -f "$work/argv.txt"
rclone ls --transfers 1 ":sftp,ssh=$work/head.sh,shell_type=unix:" 2>&1 | grep -v NOTICE || true
echo "-- invocations with shell_type=unix --"; cat "$work/argv.txt"

echo "== 11b: serve sftp --stdio honours filters; cat returns the bytes =="
echo "-- unfiltered --"
rclone ls --transfers 1 ":sftp,ssh=$work/head.sh,shell_type=unix:" 2>&1 | grep -v NOTICE || true
echo "-- served with --exclude '*.md' --"
rclone ls --transfers 1 ":sftp,ssh=$work/head_filtered.sh,shell_type=unix:" 2>&1 | grep -v NOTICE || true
echo "-- served with --links (symlinks as .rclonelink) --"
rclone ls --transfers 1 ":sftp,ssh=$work/head_links.sh,shell_type=unix:" 2>&1 | grep -v NOTICE || true
echo "-- cat --"
rclone cat --transfers 1 ":sftp,ssh=$work/head.sh,shell_type=unix:a/b/f.txt" 2>&1 | grep -v NOTICE || true

echo "== 11c: rclone serve s3 over the stdio sftp remote =="
port=${M0_S3_PORT:-18238}
# Throwaway literals for a server bound to loopback for the length of this
# script; not credentials, and nothing they reach outlives the run.
key=AKIAM0PROBE000000000
sec=c2VjcmV0LWtleS1mb3ItbTAtcHJvYmUtb25seQ
rclone serve s3 --addr "127.0.0.1:$port" --auth-key "$key,$sec" --transfers 1 \
  ":sftp,ssh=$work/head.sh,shell_type=unix:" > "$work/s3.log" 2>&1 &
s3pid=$!
trap 'kill $s3pid 2>/dev/null || true' EXIT
sleep 4
cat > "$work/rclone.conf" <<EOF
[m0s3]
type = s3
provider = Other
endpoint = http://127.0.0.1:$port
access_key_id = $key
secret_access_key = $sec
force_path_style = true
EOF
echo "-- buckets (the remote's top-level directories) --"
rclone --config "$work/rclone.conf" lsd m0s3: 2>&1 | grep -v NOTICE || true
echo "-- recursive listing of one bucket, no delimiter --"
rclone --config "$work/rclone.conf" ls m0s3:a 2>&1 | grep -v NOTICE || true
echo "-- cat through S3 --"
rclone --config "$work/rclone.conf" cat m0s3:a/b/f.txt 2>&1 | grep -v NOTICE || true
