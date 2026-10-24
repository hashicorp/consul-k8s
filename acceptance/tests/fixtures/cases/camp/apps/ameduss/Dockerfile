# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.24-alpine AS build

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build the binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ameduss .

# ---- Runtime stage ----
FROM alpine:3.20

RUN adduser -D -u 10001 ameduss
USER ameduss

WORKDIR /app
COPY --from=build /out/ameduss /app/ameduss

# Run as an HTTP (Streamable HTTP) MCP server inside the container.
ENV MCP_TRANSPORT=http \
    MCP_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/ameduss"]

