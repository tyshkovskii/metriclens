# syntax=docker/dockerfile:1

# --- Stage 1: build the frontend static assets ---
FROM node:26-alpine AS frontend

WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# --- Stage 2: build the backend with the frontend embedded ---
FROM golang:1.26.4-alpine AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
# Replace the placeholder embed directory with the compiled frontend so the
# Go binary serves the real UI from the same port as the API.
RUN rm -rf ./internal/web/dist
COPY --from=frontend /src/frontend/dist ./internal/web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/metriclens ./cmd/server

# --- Stage 3: certificate bundle for the scratch runtime ---
FROM alpine:3.24 AS certs

RUN apk add --no-cache ca-certificates

# --- Stage 4: minimal runtime image ---
FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/metriclens /usr/local/bin/metriclens

EXPOSE 9999

ENTRYPOINT ["/usr/local/bin/metriclens"]
