# Multi-stage build for Clarion Go binaries.
# Final image: Alpine + static binaries (~25MB).

FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion ./cmd/clarion
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion-worker ./cmd/worker
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion-postprocessor ./cmd/postprocessor

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /clarion /clarion-worker /clarion-postprocessor /usr/local/bin/
COPY clarion.example.toml /etc/clarion/clarion.toml
ENTRYPOINT ["clarion"]
