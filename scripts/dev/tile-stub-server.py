#!/usr/bin/env python3
"""A stand-in XYZ tile server for headless map scenes: every /{z}/{x}/{y}.png
is a generated 256 x 256 PNG — a flat colour keyed by the tile's coordinates
with a one-pixel darker border and the zoom as a small bar pattern — so a
scene needs no network and captures the same pixels every run.

Usage: tile-stub-server.py PORT   (serves until killed; prints "ready" once
listening). The map side points BOXER_MAP_TILE_URL at
http://127.0.0.1:PORT/{z}/{x}/{y}.png.
"""
import http.server
import re
import struct
import sys
import zlib

SIZE = 256
PATH = re.compile(r"^/(\d+)/(-?\d+)/(-?\d+)\.png$")


def png(z, x, y):
    # A hue from the coordinates, kept light so overlays read on top of it.
    h = (z * 7919 + x * 104729 + y * 1299709) & 0xFFFFFF
    r, g, b = 170 + (h & 0x3F), 170 + ((h >> 8) & 0x3F), 170 + ((h >> 16) & 0x3F)
    dark = bytes((r - 90, g - 90, b - 90, 255))
    fill = bytes((r, g, b, 255))
    rows = []
    for j in range(SIZE):
        if j == 0 or j == SIZE - 1:
            row = dark * SIZE
        else:
            row = dark + fill * (SIZE - 2) + dark
            # The zoom as z short bars at the top-left, two px apart.
            if 4 <= j < 10:
                row = bytearray(row)
                for k in range(z):
                    for px in range(4 + 3 * k, 6 + 3 * k):
                        if px < SIZE - 1:
                            row[px * 4:px * 4 + 4] = dark
                row = bytes(row)
        rows.append(b"\x00" + row)
    raw = b"".join(rows)

    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 6))
            + chunk(b"IEND", b""))


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        m = PATH.match(self.path)
        if not m:
            self.send_response(404)
            self.end_headers()
            return
        body = png(*(int(v) for v in m.groups()))
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):  # quiet
        pass


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print("ready", srv.server_address[1], flush=True)
    srv.serve_forever()
