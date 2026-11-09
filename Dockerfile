# syntax=docker/dockerfile:1
# Two projects, one artifact.
#
# The client builds with Node, the daemon builds with Go, and they never read each
# other's source. They meet in the server stage: the built client is copied into
# internal/webui/dist and compiled into the binary, so the runtime image is a single
# file plus a data volume - nothing to keep in sync, nothing to mount.
FROM node:20-alpine AS client
WORKDIR /src/client
COPY client/package.json client/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY client/ ./
RUN npm run build

FROM golang:1.23-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=client /src/client/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/easytier-webd ./cmd/easytier-webd
# The runtime image is distroless (no shell, nonroot uid 65532). Create the data
# directory here so the named volume inherits that owner instead of root.
RUN mkdir -p /out/data /out/data/backups

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=server /out/easytier-webd /app/easytier-webd
COPY --chown=65532:65532 --from=server /out/data /data
VOLUME ["/data"]
ENV EASYTIER_DB_DRIVER=sqlite
ENV EASYTIER_DB_DSN=/data/et.db
ENV EASYTIER_DB_BACKUP_DIR=/data/backups
EXPOSE 11211 22020
ENTRYPOINT ["/app/easytier-webd"]
# No --web-dir: the client is inside the binary.
CMD ["--http", "0.0.0.0:11211", "--config-server", "0.0.0.0:22020"]
