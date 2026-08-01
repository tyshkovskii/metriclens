# syntax=docker/dockerfile:1

# --- Stage 1: build the frontend static assets ---
FROM node:26-alpine AS frontend

WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# --- Stage 2: build the backend with the frontend embedded ---
FROM golang:1.26.5-alpine AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
COPY --from=frontend /src/frontend/dist ./internal/web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/metriclens ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/metriclensctl ./cmd/metriclensctl

# --- Stage 3: certificate bundle for the scratch runtime ---
FROM alpine:3.24 AS certs

RUN apk add --no-cache ca-certificates

# --- Stage 4: minimal runtime image ---
FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/metriclens /usr/local/bin/metriclens
COPY --from=build /out/metriclensctl /usr/local/bin/metriclensctl

EXPOSE 9999

ENTRYPOINT ["/usr/local/bin/metriclens"]
