# syntax=docker/dockerfile:1.24

ARG GO_VERSION=1.26.11

FROM docker.io/library/golang:${GO_VERSION}-trixie AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020

COPY cmd ./cmd
COPY internal ./internal
COPY locales ./locales
COPY static ./static
RUN templ generate
RUN go run ./cmd/igloo-assets && chmod -R a+rX static locales
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/igloo ./cmd/igloo \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/igloo-adduser ./cmd/adduser

# Igloo only probes media, grabs JPEG frames and copies streams, so FFmpeg's
# built-in codecs cover it; OpenSSL adds https input and dav1d adds AV1 decoding.
FROM docker.io/library/debian:trixie-slim AS ffmpeg
ARG DEBIAN_FRONTEND=noninteractive
# renovate: datasource=github-tags packageName=FFmpeg/FFmpeg versioning=regex:^n(?<major>\d+)\.(?<minor>\d+)(\.(?<patch>\d+))?$
ARG FFMPEG_VERSION=n9.1

ADD https://github.com/FFmpeg/FFmpeg/archive/refs/tags/${FFMPEG_VERSION}.tar.gz /tmp/ffmpeg.tar.gz

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential nasm pkgconf libssl-dev libdav1d-dev zlib1g-dev \
    && mkdir /tmp/ffmpeg \
    && tar -xzf /tmp/ffmpeg.tar.gz -C /tmp/ffmpeg --strip-components=1 \
    && cd /tmp/ffmpeg \
    && ./configure --prefix=/usr/local --enable-shared --disable-static --enable-rpath \
        --disable-debug --disable-doc --disable-ffplay \
        --disable-autodetect --enable-zlib --enable-openssl --enable-libdav1d \
    && make -j"$(nproc)" \
    && make install

FROM gcr.io/distroless/python3-debian13 AS runtime-base

FROM docker.io/library/debian:trixie-slim AS runtime-files
ARG DEBIAN_FRONTEND=noninteractive
# renovate: datasource=pypi packageName=pip versioning=pep440
ARG PIP_VERSION=26.1.1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libdav1d7 postgresql-common python3 python3-venv \
    && /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y \
    && printf '%s\n' 'create_main_cluster = false' > /etc/postgresql-common/createcluster.conf \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-18 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ffmpeg /usr/local/bin/ffmpeg /usr/local/bin/ffprobe /usr/local/bin/
COPY --from=ffmpeg /usr/local/lib/ /usr/local/lib/

# Gather PostgreSQL, FFmpeg and a shell (pg_ctl and initdb run /bin/sh) with
# the shared libraries the distroless base lacks, plus their package records
# for the SBOM. The LLVM JIT plugin stays out; PostgreSQL runs without JIT.
RUN --mount=type=bind,from=runtime-base,target=/base <<'EOF'
set -eu
pg=/usr/lib/postgresql/18
mkdir -p /rootfs/usr/local/bin /rootfs/usr/local/lib /rootfs/usr/bin /rootfs$pg/bin /rootfs$pg/lib \
  /rootfs/usr/share/postgresql /rootfs/var/lib/dpkg/status.d /rootfs/etc
cp -a /usr/local/bin/ffmpeg /usr/local/bin/ffprobe /rootfs/usr/local/bin/
cp -a /usr/local/lib/lib*.so* /rootfs/usr/local/lib/
cp /usr/bin/dash /rootfs/usr/bin/sh
for tool in initdb pg_ctl postgres psql pg_dump pg_restore; do
  cp -a "$pg/bin/$tool" "/rootfs$pg/bin/"
  echo "$pg/bin/$tool"
done > /tmp/sources
find "$pg/lib" -maxdepth 1 -name '*.so' ! -name 'llvmjit*' -exec cp -a {} "/rootfs$pg/lib/" \; -print >> /tmp/sources
cp -a /usr/share/postgresql/18 /rootfs/usr/share/postgresql/
rm -rf /rootfs/usr/share/postgresql/18/man
printf '%s\n' /usr/share/postgresql/18 /usr/bin/dash >> /tmp/sources

find /rootfs -type f \( -perm -u+x -o -name '*.so*' \) -exec ldd {} + 2>/dev/null > /tmp/ldd || true
if grep 'not found' /tmp/ldd; then exit 1; fi
awk '$2 == "=>" && $3 ~ /^\// { print $3 }' /tmp/ldd | sort -u | while read -r lib; do
  case "$lib" in /usr/local/lib/*) continue ;; esac
  real="$(readlink -f "$lib")"
  dir="$(dirname "$real")"
  name="$(basename "$lib")"
  [ -e "/base$dir/$name" ] && continue
  mkdir -p "/rootfs$dir"
  cp -L "$lib" "/rootfs$dir/$name"
  echo "$real" >> /tmp/sources
done

xargs dpkg -S < /tmp/sources 2>/dev/null | cut -d: -f1 | tr ',' '\n' | tr -d ' ' | sort -u | while read -r package; do
  [ -e "/base/var/lib/dpkg/status.d/$package" ] || dpkg-query -s "$package" > "/rootfs/var/lib/dpkg/status.d/$package"
done

{ cat /base/etc/passwd; printf '%s\n' 'igloo:x:10001:10001:Igloo:/tmp:/sbin/nologin' 'postgres:x:999:999:PostgreSQL:/var/empty:/sbin/nologin'; } > /rootfs/etc/passwd
{ cat /base/etc/group; printf '%s\n' 'igloo:x:10001:' 'postgres:x:999:'; } > /rootfs/etc/group

mkdir -p /state/igloo/data /state/igloo/config
: > /state/igloo/data/.igloo-state-root
EOF

ADD https://github.com/yt-dlp/yt-dlp/archive/master.tar.gz /tmp/yt-dlp.tar.gz
ADD https://codeberg.org/mikf/gallery-dl/archive/master.tar.gz /tmp/gallery-dl.tar.gz
COPY requirements-runtime.txt /tmp/requirements-runtime.txt

RUN python3 -m venv /opt/igloo-py \
    && /opt/igloo-py/bin/pip install --no-cache-dir --upgrade "pip==${PIP_VERSION}" \
    && grep -v ' @ ' /tmp/requirements-runtime.txt > /tmp/requirements-extra.txt \
    && /opt/igloo-py/bin/pip install --no-cache-dir /tmp/yt-dlp.tar.gz /tmp/gallery-dl.tar.gz -r /tmp/requirements-extra.txt \
    && /opt/igloo-py/bin/pip uninstall -y pip

FROM runtime-base

COPY --from=runtime-files /rootfs/ /
COPY --from=runtime-files /opt/igloo-py /opt/igloo-py
COPY --from=runtime-files --chown=10001:10001 /state/igloo /igloo
COPY --from=build /out/igloo /out/igloo-adduser /usr/local/bin/
COPY --from=build /src/locales /app/locales
COPY --from=build /src/static /app/static
COPY --chmod=755 scripts/container-entrypoint.sh /usr/local/bin/igloo-entrypoint

ENV PATH="/usr/local/bin:/opt/igloo-py/bin:/usr/lib/postgresql/18/bin:/usr/bin:/bin" \
    IGLOO_PYTHON=/opt/igloo-py/bin/python3 \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt \
    LANG=C.UTF-8 \
    HOME=/tmp \
    IGLOO_DATA_DIR=/igloo/data \
    IGLOO_CONFIG_DIR=/igloo/config \
    IGLOO_REPO_DIR=/app \
    IGLOO_PORT=5001 \
    IGLOO_ENABLED_PLATFORMS=all

WORKDIR /app
VOLUME ["/igloo"]
EXPOSE 5001
USER 10001:10001

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/bin/python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:5001/api/health/live', timeout=4).read()"]

ENTRYPOINT ["/usr/local/bin/igloo-entrypoint"]
CMD ["igloo"]
