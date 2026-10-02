FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# Precompress hashed assets so the server sends them gzipped with zero CPU cost.
RUN npm run build && find dist/assets -type f \( -name '*.js' -o -name '*.css' \) -exec sh -c 'gzip -9c "$1" > "$1.gz"' _ {} \;

FROM golang:1.24-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/monitor ./cmd/dashboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/monitor /monitor
COPY --from=frontend /web/dist /app/web
EXPOSE 8080
ENTRYPOINT ["/monitor"]
