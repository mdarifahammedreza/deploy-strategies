# syntax=docker/dockerfile:1
# ---------- Stage 1: compile the Go program ----------
# Official Go image (Alpine flavour = small download)
FROM golang:1.26-alpine AS build
# all following commands run inside /src
WORKDIR /src
# copy only go.mod first so the dependency layer is cached (this module has no deps)
COPY go.mod ./
# copy the source code
COPY *.go ./
# CGO_ENABLED=0 → fully static binary that runs on a distroless image; -s -w strips debug info
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/color-backend .

# ---------- Stage 2: tiny runtime image ----------
# distroless = no shell, no package manager, runs as non-root user 65532
FROM gcr.io/distroless/static-debian12:nonroot
# build-time arguments: `docker build --build-arg VERSION=v2 ...` picks the version
ARG VERSION=v1
ARG COLOR=royalblue
ARG ERROR_RATE=0
ARG NEVER_READY=false
# turn the build args into environment variables the program reads at runtime
ENV VERSION=${VERSION} COLOR=${COLOR} ERROR_RATE=${ERROR_RATE} NEVER_READY=${NEVER_READY}
# copy just the compiled binary from stage 1
COPY --from=build /out/color-backend /color-backend
# documentation only: the app listens on 8080
EXPOSE 8080
# never run as root
USER nonroot:nonroot
# the program to start
ENTRYPOINT ["/color-backend"]
