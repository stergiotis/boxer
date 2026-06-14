# Runtime-only image for the imzero2 headless pixel-streaming demo (ADR-0024).
#
# Binaries are built on the HOST with the project's own scripts and baked in,
# so this image carries NO Rust/Go toolchain — only the runtime deps:
#   - Mesa software rendering (lavapipe Vulkan + llvmpipe GL) for wgpu offscreen
#   - a static ffmpeg with a software H.264 encoder (libx264) for the encode path
#   - Noto fonts + fontconfig (the egui text stack)
#
# Fedora 44 base matches the build host's glibc (2.43) so the dynamically linked
# Rust client runs as-is; Debian/Ubuntu ship older glibc. The Go launcher is
# CGO-free (static) and portable regardless.
#
# Build via ./build.sh (stages the prebuilt binaries into _stage/ first).
FROM fedora:44

# Software GPU stack (pure CPU — no /dev/dri needed) + fonts + init + curl.
RUN dnf -y install \
        mesa-vulkan-drivers mesa-dri-drivers vulkan-loader \
        google-noto-sans-fonts google-noto-sans-cjk-fonts fontconfig \
        tini curl xz \
    && dnf clean all && rm -rf /var/cache/dnf

# Static ffmpeg (GPL build → includes libx264). Avoids Fedora's codec-repo
# split entirely; the grep gates the build if the encoder is missing.
RUN curl -fsSL -o /tmp/ffmpeg.tar.xz \
      https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl.tar.xz \
    && tar -C /tmp -xf /tmp/ffmpeg.tar.xz \
    && cp /tmp/ffmpeg-master-latest-linux64-gpl/bin/ffmpeg /usr/local/bin/ffmpeg \
    && chmod +x /usr/local/bin/ffmpeg \
    && rm -rf /tmp/ffmpeg* \
    && ffmpeg -hide_banner -encoders | grep -q libx264

WORKDIR /app
COPY _stage/main_go   /app/main_go
COPY _stage/imzero2   /app/imzero2
COPY _stage/assets    /app/assets
COPY entrypoint.sh    /app/entrypoint.sh
RUN chmod +x /app/main_go /app/imzero2 /app/entrypoint.sh

# Force software rendering; bind the container interface so the Caddy sidecar
# can reach the carrier. NOTE: v1 has no in-app auth/TLS (ADR-0082 is proposed,
# not built) — TLS + the password live in Caddy, and this port is never
# published to the host directly. Do not expose 8089/8090 publicly.
ENV LIBGL_ALWAYS_SOFTWARE=1 \
    IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089 \
    IMZERO2_HEADLESS_FPS=30 \
    IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libx264 -preset veryfast -tune zerolatency -bf 0 -g 100000"
# LAUNCH intentionally unset → interactive carousel (no clickhouse-local needed).

EXPOSE 8089 8090
ENTRYPOINT ["/usr/bin/tini", "--", "/app/entrypoint.sh"]
