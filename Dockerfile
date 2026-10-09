# syntax=docker/dockerfile:1.24

ARG GO_VERSION=1.26.9

FROM docker.io/library/golang:${GO_VERSION}-bookworm AS build
ARG DEBIAN_FRONTEND=noninteractive

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020

COPY cmd ./cmd
COPY internal ./internal
COPY locales ./locales
COPY static ./static
RUN templ generate
RUN go run ./cmd/igloo-assets
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/igloo ./cmd/igloo \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/igloo-adduser ./cmd/adduser \
    && mkdir -p /out/static /out/locales \
    && cp -a static/. /out/static/ \
    && cp -a locales/. /out/locales/

FROM docker.io/library/debian:bookworm-slim AS runtime
ARG DEBIAN_FRONTEND=noninteractive
# renovate: datasource=pypi packageName=pip versioning=pep440
ARG PIP_VERSION=26.1.1

ADD https://github.com/yt-dlp/yt-dlp/archive/master.tar.gz /tmp/yt-dlp.tar.gz
ADD https://codeberg.org/mikf/gallery-dl/archive/master.tar.gz /tmp/gallery-dl.tar.gz

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && install -d /usr/share/postgresql-common/pgdg \
    && curl --fail --silent --show-error https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
    && printf '%s\n' 'deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main' > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-common \
    && printf '%s\n' 'create_main_cluster = false' > /etc/postgresql-common/createcluster.conf \
    && apt-get install -y --no-install-recommends postgresql-18 ffmpeg python3 python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && python3 -m venv /opt/igloo-py \
    && /opt/igloo-py/bin/pip install --no-cache-dir --upgrade "pip==${PIP_VERSION}" \
    && /opt/igloo-py/bin/pip install --no-cache-dir /tmp/yt-dlp.tar.gz /tmp/gallery-dl.tar.gz curl-cffi "TikTokLive>=7,<8" \
    && rm /tmp/yt-dlp.tar.gz /tmp/gallery-dl.tar.gz

ENV PATH="/usr/lib/postgresql/18/bin:/opt/igloo-py/bin:${PATH}" \
    IGLOO_PYTHON=/opt/igloo-py/bin/python3 \
    HOME=/tmp \
    IGLOO_DATA_DIR=/igloo/data \
    IGLOO_CONFIG_DIR=/igloo/config \
    IGLOO_REPO_DIR=/app \
    IGLOO_PORT=5001 \
    IGLOO_ENABLED_PLATFORMS=all

WORKDIR /app
COPY --from=build /out/igloo /usr/local/bin/igloo
COPY --from=build /out/igloo-adduser /usr/local/bin/igloo-adduser
COPY --from=build /out/locales /app/locales
COPY --from=build /out/static /app/static
COPY --chmod=755 scripts/container-entrypoint.sh /usr/local/bin/igloo-entrypoint

RUN chmod -R a+rX /app/locales /app/static \
    && groupadd --gid 10001 igloo \
    && useradd --uid 10001 --gid 10001 --home-dir /tmp --no-create-home --shell /usr/sbin/nologin igloo \
    && mkdir -p /igloo/data /igloo/config \
    && : > /igloo/data/.igloo-state-root \
    && chown -R 10001:10001 /igloo

VOLUME ["/igloo"]
EXPOSE 5001
USER 10001:10001

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/bin/python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:5001/api/health/live', timeout=4).read()"]

ENTRYPOINT ["/usr/local/bin/igloo-entrypoint"]
CMD ["igloo"]
