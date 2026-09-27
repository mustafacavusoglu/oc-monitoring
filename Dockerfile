FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/monitor ./cmd/dashboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/monitor /monitor
COPY --from=frontend /web/dist /app/web
ENV HTTP_ADDR=:8080 WEB_DIR=/app/web
EXPOSE 8080
ENTRYPOINT ["/monitor"]
