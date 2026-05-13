# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26

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
# renovate: datasource=pypi packageName=yt-dlp versioning=pep440
ARG YT_DLP_VERSION=2026.3.17
# renovate: datasource=pypi packageName=gallery-dl versioning=pep440
ARG GALLERY_DL_VERSION=1.32.1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && python3 -m venv /opt/igloo-py \
    && /opt/igloo-py/bin/pip install --no-cache-dir --upgrade "pip==${PIP_VERSION}" \
    && /opt/igloo-py/bin/pip install --no-cache-dir \
        "yt-dlp==${YT_DLP_VERSION}" \
        "gallery-dl==${GALLERY_DL_VERSION}"

ENV PATH="/opt/igloo-py/bin:${PATH}" \
    IGLOO_DATA_DIR=/data \
    IGLOO_CONFIG_DIR=/config \
    IGLOO_REPO_DIR=/app \
    IGLOO_PORT=5001 \
    IGLOO_ENABLED_PLATFORMS=all

WORKDIR /app
COPY --from=build /out/igloo /usr/local/bin/igloo
COPY --from=build /out/igloo-adduser /usr/local/bin/igloo-adduser
COPY --from=build /out/locales /app/locales
COPY --from=build /out/static /app/static

RUN mkdir -p /data /config

VOLUME ["/data", "/config"]
EXPOSE 5001

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/bin/python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:5001/api/health/live', timeout=4).read()"]

CMD ["igloo"]
