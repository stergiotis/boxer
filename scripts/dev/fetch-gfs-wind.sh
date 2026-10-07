#!/bin/bash
# Fetch NOAA GFS 1.0° 10 m wind for one run into one GRIB2 file (ADR-0292
# SD5): UGRD and VGRD at 10 m above ground, forecast hours 0 to 48 every 3,
# cut by HTTP byte range from NOAA's open-data bucket using each file's .idx
# sidecar, so only the 34 messages needed are downloaded. GFS output is a
# work of the US Government.
#
#   scripts/dev/fetch-gfs-wind.sh <yyyymmdd> <hh> <out.grib2>
#
# The bucket keeps recent runs only; an older one fails at the first .idx.
set -euo pipefail

if [ $# -ne 3 ]; then
	echo "usage: $0 <yyyymmdd> <hh> <out.grib2>" >&2
	exit 64
fi
day=$1 hour=$2 out=$3
base="https://noaa-gfs-bdp-pds.s3.amazonaws.com/gfs.${day}/${hour}/atmos"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
: >"$out"

for fh in $(seq -f '%03g' 0 3 48); do
	file="gfs.t${hour}z.pgrb2.1p00.f${fh}"
	curl -fsS --retry 3 -o "$tmp" "$base/$file.idx"
	for var in UGRD VGRD; do
		# An .idx line is n:offset:d=...:VAR:LEVEL:...; a message ends where
		# the next one starts, and the file's last message at its end.
		range=$(awk -F: -v v="$var" '
			found { print start "-" $2 - 1; done = 1; exit }
			$4 == v && $5 == "10 m above ground" { start = $2; found = 1 }
			END { if (found && !done) print start "-" }' "$tmp")
		if [ -z "$range" ]; then
			echo "$file: no $var at 10 m above ground" >&2
			exit 1
		fi
		curl -fsS --retry 3 -r "$range" "$base/$file" >>"$out"
	done
done
echo "wrote $out: $(wc -c <"$out") bytes, 34 messages from $base" >&2
