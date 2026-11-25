# syntax=docker/dockerfile:1
# Build context = the REPO ROOT:  docker build -f dashboard/Dockerfile -t color-dashboard:dev .
# ---------- Stage 1: build the Vite frontend into static files ----------
FROM node:24-alpine AS web
WORKDIR /web
# install exact dependency versions from the lock file (cached unless package*.json change)
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
# copy the source and produce /web/dist
COPY frontend/ ./
RUN npm run build

# ---------- Stage 2: compile the Go dashboard ----------
FROM golang:1.26-alpine AS build
WORKDIR /src
# download modules first so this layer is cached between code changes
COPY dashboard/go.mod dashboard/go.sum ./
RUN go mod download
COPY dashboard/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dashboard .

# ---------- Stage 3: minimal runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashboard /dashboard
# the frontend files the Go server serves
COPY --from=web /web/dist /web
ENV STATIC_DIR=/web LISTEN=:8081
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/dashboard"]
