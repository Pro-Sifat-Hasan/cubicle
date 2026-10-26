# Cubicle server image: gateway + agent worker in one container.
# Build from the repo root:  docker build -f deploy/docker/Dockerfile -t cubicle .

FROM golang:1.27-alpine AS gateway
WORKDIR /src
COPY gateway/go.mod gateway/go.sum ./
RUN go mod download
COPY gateway/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/cubicle-gateway ./cmd/cubicle-gateway

FROM python:3.14-slim AS runtime
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1
RUN useradd --create-home --uid 10001 cubicle && mkdir /data && chown cubicle:cubicle /data
COPY agents/ /tmp/agents/
RUN pip install /tmp/agents && rm -rf /tmp/agents
COPY --from=gateway /out/cubicle-gateway /usr/local/bin/cubicle-gateway

USER cubicle
# Inside the container the gateway listens on all interfaces; publish it only on
# the host's loopback (see compose.yml) or behind a TLS reverse proxy.
ENV CUBICLE_HOME=/data \
    CUBICLE_ADDR=0.0.0.0:7878 \
    CUBICLE_ALLOW_INSECURE_REMOTE=1 \
    CUBICLE_WORKER_COMMAND=cubicle-agents
VOLUME ["/data"]
EXPOSE 7878
HEALTHCHECK --interval=30s --timeout=3s CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:7878/healthz', timeout=2)"
ENTRYPOINT ["cubicle-gateway", "serve"]
