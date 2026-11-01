# Runtime layout under /opt/hackme: binaries + scripts (worker_loop) + tasks (fuzz gates).
# Set HACKME_WORKING_DIR=/opt/hackme in compose so cwd resolves ./scripts and ./data.
FROM golang:1.26.10-bookworm AS build
ARG HACKME_VERSION=0.1.0-rc18.1
WORKDIR /src
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=local
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${HACKME_VERSION}" -o /out/hackme .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/coordinator ./cmd/coordinator
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/workerpoh-cpu ./cmd/workerpoh
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/minersign ./cmd/minersign
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/workerfuzz ./cmd/workerfuzz
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hackme-wasm-check ./cmd/hackme-wasm-check

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
       ca-certificates curl bash jq python3 bubblewrap \
  && rm -rf /var/lib/apt/lists/*
RUN install -d /opt/hackme /opt/hackme/bin /opt/hackme/logs
COPY --from=build /out/hackme /opt/hackme/hackme
COPY --from=build /out/coordinator /opt/hackme/coordinator
COPY --from=build /out/workerpoh-cpu /opt/hackme/workerpoh-cpu
COPY --from=build /out/workerpoh-cpu /opt/hackme/workerpoh
COPY --from=build /out/minersign /opt/hackme/minersign
COPY --from=build /out/workerfuzz /opt/hackme/workerfuzz
COPY --from=build /out/workerfuzz /opt/hackme/bin/workerfuzz
COPY --from=build /out/hackme-wasm-check /opt/hackme/bin/hackme-wasm-check
COPY --from=build /src/scripts /opt/hackme/scripts
COPY --from=build /src/tasks /opt/hackme/tasks
COPY deploy/docker-entrypoint.sh /usr/local/bin/hackme-entrypoint
# Ensure entrypoint edits always land in the image (BuildKit can keep stale COPY).
ARG ENTRYPOINT_SHA=dev
RUN echo "entrypoint_sha=${ENTRYPOINT_SHA}" >/opt/hackme/.entrypoint_sha \
  && ln -sf /opt/hackme/hackme /usr/local/bin/hackme \
  && ln -sf /opt/hackme/coordinator /usr/local/bin/coordinator \
  && ln -sf /opt/hackme/minersign /usr/local/bin/minersign \
  && ln -sf /opt/hackme/workerfuzz /usr/local/bin/workerfuzz \
  && ln -sf /opt/hackme/scripts/ops/worker_autostart.sh /opt/hackme/worker_autostart.sh \
  && chmod 755 /usr/local/bin/hackme-entrypoint \
  && chmod 755 /opt/hackme/hackme /opt/hackme/coordinator /opt/hackme/workerpoh \
       /opt/hackme/workerpoh-cpu /opt/hackme/minersign /opt/hackme/workerfuzz \
       /opt/hackme/bin/workerfuzz /opt/hackme/bin/hackme-wasm-check \
  && chmod 755 /opt/hackme/scripts/ops/*.sh 2>/dev/null || true
WORKDIR /opt/hackme
ENTRYPOINT ["/usr/local/bin/hackme-entrypoint"]
EXPOSE 8080 8081
