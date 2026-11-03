# syntax=docker/dockerfile:1
# Two-stage build: the client is bundled by Node, the daemon by Go, and the
# final image carries only the binary, the built client and a data volume.
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
COPY --from=client /src/client/dist ./client/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/easytier-webd ./cmd/easytier-webd
# The runtime image is distroless (no shell, nonroot uid 65532). Create the data
# directory here so the named volume inherits that owner instead of root.
RUN mkdir -p /out/data /out/data/backups

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=server /out/easytier-webd /app/easytier-webd
COPY --from=client /src/client/dist /app/client/dist
COPY --chown=65532:65532 --from=server /out/data /data
VOLUME ["/data"]
ENV EASYTIER_DB_DRIVER=sqlite
ENV EASYTIER_DB_DSN=/data/et.db
ENV EASYTIER_DB_BACKUP_DIR=/data/backups
EXPOSE 11211 22020
ENTRYPOINT ["/app/easytier-webd"]
CMD ["--http", "0.0.0.0:11211", "--config-server", "0.0.0.0:22020", "--web-dir", "/app/client/dist"]
