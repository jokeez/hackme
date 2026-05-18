# Runtime layout under /opt/hackme: binaries + scripts (worker_loop) + tasks (fuzz gates).
# Set HACKME_WORKING_DIR=/opt/hackme in compose so cwd resolves ./scripts and ./data.
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hackme .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/coordinator ./cmd/coordinator

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl bash jq \
  && rm -rf /var/lib/apt/lists/*
RUN install -d /opt/hackme
COPY --from=build /out/hackme /opt/hackme/hackme
COPY --from=build /out/coordinator /opt/hackme/coordinator
COPY --from=build /src/scripts /opt/hackme/scripts
COPY --from=build /src/tasks /opt/hackme/tasks
RUN ln -sf /opt/hackme/hackme /usr/local/bin/hackme \
  && ln -sf /opt/hackme/coordinator /usr/local/bin/coordinator
WORKDIR /opt/hackme
EXPOSE 8080 8081
