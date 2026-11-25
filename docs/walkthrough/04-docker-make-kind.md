# Walkthrough 4: Docker, kind and Make

This part explains everything that is **not** application code but makes the project run: two Dockerfiles that build the container images, the kind cluster configuration, the Makefile (the classroom remote control), and the helper functions of the end-to-end test script.

Words you need first:
- **Image**: a read-only package with a program and everything it needs. A **container** is a running image.
- **Layer**: each Dockerfile instruction creates one layer. Docker caches layers and re-uses them if nothing before them changed.
- **kind**: "Kubernetes IN Docker". Each Kubernetes node is a Docker container on your laptop.
- **Make**: a tool that runs named recipes (targets) from a file called `Makefile`.

---

## Part 1: `backend/Dockerfile`

One Dockerfile builds all four backend images (v1 to v4). Only the build arguments differ.

### Syntax line and stage 1 header

```dockerfile
# backend/Dockerfile lines 1-4
# syntax=docker/dockerfile:1
# ---------- Stage 1: compile the Go program ----------
# Official Go image (Alpine flavour = small download)
FROM golang:1.26-alpine AS build
```

The first line selects the current Dockerfile syntax. `FROM golang:1.26-alpine AS build` starts **stage 1**: an image that contains the Go compiler (Alpine Linux is a very small Linux, so the download is small). `AS build` gives the stage a name so later stages can take files from it.

This is a **multi-stage build**: the compiler is needed to build the program, but not to run it. We use a big image to build and throw it away.

### Working folder and layer caching

```dockerfile
# backend/Dockerfile lines 5-10
# all following commands run inside /src
WORKDIR /src
# copy only go.mod first so the dependency layer is cached (this module has no deps)
COPY go.mod ./
# copy the source code
COPY *.go ./
```

`WORKDIR` sets the folder for the next commands (and creates it). We copy `go.mod` **before** the source files. Docker caches each step: if `go.mod` did not change, the layer is re-used, and any dependency download step after it would not run again. Here the backend has no dependencies, but the pattern is the same as in the dashboard Dockerfile and is a good habit. Then `COPY *.go ./` brings in the code, which changes often, so it comes last.

### Compile

```dockerfile
# backend/Dockerfile lines 11-12
# CGO_ENABLED=0 → fully static binary that runs on a distroless image; -s -w strips debug info
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/color-backend .
```

`CGO_ENABLED=0` makes a **static** binary that does not depend on system libraries (such as libc). That is required for the tiny runtime image, which contains none of them. `-trimpath` removes your local file paths from the binary, and `-ldflags="-s -w"` strips debug information to make the file smaller. The output goes to `/out/color-backend`.

### Stage 2: the runtime image

```dockerfile
# backend/Dockerfile lines 14-16
# ---------- Stage 2: tiny runtime image ----------
# distroless = no shell, no package manager, runs as non-root user 65532
FROM gcr.io/distroless/static-debian12:nonroot
```

A second `FROM` starts a **new, clean image**. Nothing from stage 1 comes along unless we copy it. **Distroless** images contain only the minimum to run a program: no shell, no package manager, no `ls`, no `curl`. Why? It is smaller (a few MB), and more secure: an attacker who breaks into the container has no shell and no tools. The `:nonroot` tag makes the default user number 65532, not root. (Because there is no shell, we also use the built-in `sleep` action for `preStop` in the YAML, and `kubectl exec ... sh` will not work.)

### Build args turned into ENV

```dockerfile
# backend/Dockerfile lines 17-23
# build-time arguments: `docker build --build-arg VERSION=v2 ...` picks the version
ARG VERSION=v1
ARG COLOR=royalblue
ARG ERROR_RATE=0
ARG NEVER_READY=false
# turn the build args into environment variables the program reads at runtime
ENV VERSION=${VERSION} COLOR=${COLOR} ERROR_RATE=${ERROR_RATE} NEVER_READY=${NEVER_READY}
```

`ARG` defines a variable that exists **only at build time**; you set it with `docker build --build-arg VERSION=v2`. The program, however, reads **environment variables at runtime**. `ENV VERSION=${VERSION} ...` copies each build arg into an environment variable stored in the image. So `color-backend:v3` always carries `ERROR_RATE=0.3` inside, and it behaves the same on any cluster with no extra YAML. The `=v1` parts are defaults.

### Binary, port, user, start command

```dockerfile
# backend/Dockerfile lines 24-31
# copy just the compiled binary from stage 1
COPY --from=build /out/color-backend /color-backend
# documentation only: the app listens on 8080
EXPOSE 8080
# never run as root
USER nonroot:nonroot
# the program to start
ENTRYPOINT ["/color-backend"]
```

`COPY --from=build` takes only the compiled binary from stage 1. `EXPOSE 8080` is documentation: it does not open the port (Kubernetes' `containerPort` does the declaring). `USER nonroot:nonroot` runs the program without root rights. `ENTRYPOINT ["/color-backend"]` uses the **exec form** (a JSON list): the program becomes process 1 and receives SIGTERM directly. In shell form a shell would sit in between and the signal would never reach our Go code, so graceful shutdown would break.

---

## Part 2: `dashboard/Dockerfile`

The dashboard image has **three stages**: build the web page, build the Go server, and combine both.

### Header

```dockerfile
# dashboard/Dockerfile lines 1-2
# syntax=docker/dockerfile:1
# Build context = the REPO ROOT:  docker build -f dashboard/Dockerfile -t color-dashboard:dev .
```

The build context is the **repository root**, not the `dashboard` folder. We need the `frontend/` folder too, and Docker can only copy files from inside the context. That is why the command has `-f dashboard/Dockerfile` and ends with `.`. The `.dockerignore` file in the root keeps `node_modules`, `docs`, `k8s` and similar folders out of the context, so the build starts faster.

### Stage 1: the frontend

```dockerfile
# dashboard/Dockerfile lines 3-11
# ---------- Stage 1: build the Vite frontend into static files ----------
FROM node:24-alpine AS web
WORKDIR /web
# install exact dependency versions from the lock file (cached unless package*.json change)
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
# copy the source and produce /web/dist
COPY frontend/ ./
RUN npm run build
```

`node:24-alpine` has Node.js to run Vite. We copy only `package.json` and `package-lock.json` first and run `npm ci`. `npm ci` installs the **exact** versions from the lock file (more repeatable than `npm install`). As these two files rarely change, that slow step is cached. Only after that do we copy the source and run `npm run build`, which type-checks and bundles the page into `/web/dist`.

### Stage 2: the Go server

```dockerfile
# dashboard/Dockerfile lines 13-20
# ---------- Stage 2: compile the Go dashboard ----------
FROM golang:1.26-alpine AS build
WORKDIR /src
# download modules first so this layer is cached between code changes
COPY dashboard/go.mod dashboard/go.sum ./
RUN go mod download
COPY dashboard/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dashboard .
```

Same pattern: copy `go.mod` and `go.sum`, run `go mod download`, then copy the code. The dashboard has real dependencies (client-go and the websocket package), and downloading them takes a long time, so the layer cache saves minutes on every code change. The final `go build` is the same static build as in the backend.

### Stage 3: put it together

```dockerfile
# dashboard/Dockerfile lines 22-30
# ---------- Stage 3: minimal runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashboard /dashboard
# the frontend files the Go server serves
COPY --from=web /web/dist /web
ENV STATIC_DIR=/web LISTEN=:8081
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/dashboard"]
```

The final image starts from distroless again and receives two things from earlier stages: the Go binary from `build` and the finished web files from `web`. Node, npm, the Go compiler and all source code stay behind, so the image is small. `ENV STATIC_DIR=/web LISTEN=:8081` tells the Go program where the frontend files are and which port to listen on (these match `dashboard/config.go`). Then the same safe defaults: non-root user and exec-form `ENTRYPOINT`.

---

## Part 3: `kind/cluster.yaml`

### Header and identity

```yaml
# kind/cluster.yaml lines 1-9
# =====================================================================
# kind cluster for the deployment-strategies class.
# kind = "Kubernetes IN Docker": every Kubernetes node is a Docker container.
# Create it with:  kind create cluster --config kind/cluster.yaml
# =====================================================================
kind: Cluster                          # object type understood by the kind CLI (not by Kubernetes)
apiVersion: kind.x-k8s.io/v1alpha4     # version of kind's config file format
name: deploy-demo                      # cluster name → kubectl context becomes "kind-deploy-demo"
nodes:                                 # list of nodes (each one is a Docker container)
```

The first lines are comments with the command to create the cluster. `kind: Cluster` and `apiVersion` describe the kind configuration format (this is kind's own file, not a Kubernetes object). `name: deploy-demo` makes the kubectl context `kind-deploy-demo`, which the Makefile uses. `nodes` lists the machines.

### Control plane with a port mapping

```yaml
# kind/cluster.yaml lines 10-14
  - role: control-plane                # node 1: runs the API server, scheduler, etcd, controllers
    extraPortMappings:                 # publish ports of this node container on your laptop
      - containerPort: 30080           # NodePort of the dashboard Service inside the node
        hostPort: 30080                # same port on localhost → http://localhost:30080
        protocol: TCP                  # plain TCP (HTTP and WebSocket both ride on it)
```

The **control-plane** node runs the Kubernetes brain (API server, scheduler, etcd, controllers). `extraPortMappings` publishes a port of the node's Docker container on your laptop. The dashboard Service is a NodePort 30080; this mapping forwards `localhost:30080` to it, so you can open the dashboard in a normal browser. Without this the cluster would only be reachable from inside Docker's network. Both HTTP and WebSocket use TCP, so one mapping is enough.

### Two workers

```yaml
# kind/cluster.yaml lines 15-16
  - role: worker                       # node 2: runs application pods
  - role: worker                       # node 3: second worker so pods spread over two nodes
```

Two worker nodes run the application pods. Two (not one) so pods spread across nodes and students see different node names in the boxes.

---

## Part 4: the `Makefile`

A Makefile has **variables** and **targets**. A target looks like `name: dependencies` followed by recipe lines indented with a **tab**. Running `make name` first runs the dependencies and then the recipe. The text after `##` is the help text that `make help` prints.

### Header and variables

```makefile
# Makefile lines 6-19
# Name of the kind cluster (must match `name:` in kind/cluster.yaml).
CLUSTER ?= deploy-demo
# Namespace that holds the whole demo.
NS ?= color-demo
# kind version installed by `make kind-install` (the version this repo was tested with).
KIND_VERSION ?= v0.33.0
# kubectl always talks to OUR cluster and namespace, whatever your current context is.
KUBECTL ?= kubectl --context kind-$(CLUSTER) -n $(NS)
# Where the dashboard is reachable from your laptop.
DASH_URL ?= http://localhost:30080
# Target version for upgrades: `make upgrade TO=v3`.
TO ?= v2
# Folder with the strategy manifests.
STRAT := k8s/strategies
```

`?=` means "set this only if not already set", so you can override a variable on the command line (`make upgrade TO=v3`) or from the environment. `KUBECTL` includes `--context` and `-n`, so every command talks to our cluster and namespace, even if your current kubectl context points to some production cluster. `:=` (for `STRAT`) is a normal assignment, evaluated once.

### Default goal and `.PHONY`

```makefile
# Makefile lines 21-26
.DEFAULT_GOAL := help
.PHONY: help test test-backend test-dashboard test-frontend images image-backend image-dashboard \
        kind-install cluster-up cluster-down load dashboard up reset status open dev-frontend e2e \
        recreate-start rolling-start upgrade undo history \
        bg-start bg-deploy-green bg-switch bg-rollback bg-retire-blue \
        canary-start canary-deploy canary-30 canary-50 canary-promote canary-abort
```

`.DEFAULT_GOAL := help` makes plain `make` show the help. `.PHONY` lists targets that are **commands, not files**. Without it, if a file named `test` existed, `make test` would say "up to date" and do nothing. The backslash at the end of a line continues the list on the next line.

### `help`

```makefile
# Makefile lines 28-30
help: ## list all targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*## "} {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
```

`grep` finds every line with `target: ... ## text` and `awk` prints it in two columns, with the target name in cyan (`\033[36m` is a color code). The `@` in front hides the command itself, so only the table prints. `$$` is needed because make would otherwise treat a single `$` as one of its own variables.

### Test targets

```makefile
# Makefile lines 32-45
# ---------------------------------------------------------------- tests
test: test-backend test-dashboard test-frontend ## run every unit test

test-backend: ## Go tests of the color API
	cd backend && go test ./...

test-dashboard: ## Go tests of the dashboard
	cd dashboard && go test -race ./...

test-frontend: ## Vitest tests of the frontend
	cd frontend && ([ -d node_modules ] || npm ci) && npm test

e2e: ## run the end-to-end test of every strategy against the live cluster
	bash scripts/e2e.sh
```

`test` has no recipe; it only depends on the three others, so it runs all of them. The backend and dashboard tests run `go test`; the dashboard uses `-race`, which finds data races (two goroutines touching the same data) at runtime. For the frontend, `[ -d node_modules ] || npm ci` installs packages only if the folder is missing. `cd ... && ...` is on one line, because every recipe line runs in a **new shell**, so a `cd` on its own line would be forgotten. `e2e` runs the script described in Part 5.

### Image targets

```makefile
# Makefile lines 47-57
# ---------------------------------------------------------------- images
images: image-backend image-dashboard ## build every container image

image-backend: ## build color-backend:v1..v4 (one Dockerfile, different build args)
	docker build -t color-backend:v1 --build-arg VERSION=v1 --build-arg COLOR=royalblue backend
	docker build -t color-backend:v2 --build-arg VERSION=v2 --build-arg COLOR=mediumseagreen backend
	docker build -t color-backend:v3 --build-arg VERSION=v3 --build-arg COLOR=darkorange --build-arg ERROR_RATE=0.3 backend
	docker build -t color-backend:v4 --build-arg VERSION=v4 --build-arg COLOR=mediumpurple --build-arg NEVER_READY=true backend

image-dashboard: ## build color-dashboard:dev (frontend + Go server)
	docker build -t color-dashboard:dev -f dashboard/Dockerfile .
```

`images` depends on the two image targets. `image-backend` runs `docker build` four times from the same Dockerfile and context (`backend`) with different `--build-arg` values: v3 gets `ERROR_RATE=0.3` and v4 gets `NEVER_READY=true`. `image-dashboard` uses `-f dashboard/Dockerfile` and `.` (the repo root) because of the frontend, as explained above. The dashboard tag `dev` is only a label here.

### Cluster targets

```makefile
# Makefile lines 59-67
# ---------------------------------------------------------------- cluster
kind-install: ## install the kind CLI with Go
	go install sigs.k8s.io/kind@$(KIND_VERSION)

cluster-up: ## create the kind cluster (skipped if it already exists)
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --config kind/cluster.yaml

cluster-down: ## delete the kind cluster
	kind delete cluster --name $(CLUSTER)
```

`kind-install` installs the pinned kind version with `go install`. `cluster-up` first lists clusters; `grep -qx` is a quiet, whole-line match. If the cluster exists, grep succeeds and `||` skips the create step. So running `make up` twice is safe (**idempotent**). `cluster-down` deletes the cluster and everything in it.

### `load`: `kind load`

```makefile
# Makefile lines 69-71
load: images ## copy local images into the kind nodes (kind cannot see your Docker images otherwise)
	kind load docker-image --name $(CLUSTER) \
	  color-backend:v1 color-backend:v2 color-backend:v3 color-backend:v4 color-dashboard:dev
```

The kind nodes are Docker containers with **their own** image store, separate from your laptop's Docker. An image that you built locally is invisible to them. Normally Kubernetes would pull from a registry; our images are in no registry. `kind load docker-image` copies the images into every node. That is also why the YAML uses `imagePullPolicy: IfNotPresent`: "use the image the node already has, do not try to pull". With the default policy for a `:latest`-style tag, Kubernetes would try to pull and fail with `ErrImagePull`. The target depends on `images`, so it builds first.

### `dashboard`, `up`

```makefile
# Makefile lines 73-79
dashboard: ## deploy (or redeploy) the dashboard
	$(KUBECTL) apply -f k8s/base/
	$(KUBECTL) rollout restart deploy/color-dashboard
	$(KUBECTL) rollout status deploy/color-dashboard --timeout=120s

up: cluster-up load dashboard ## everything: cluster + images + dashboard
	@echo "Dashboard ready → $(DASH_URL)"
```

`dashboard` applies all YAML in `k8s/base/` (namespace, RBAC, Deployment, Service). `rollout restart` forces new pods, so a freshly loaded image is actually used (the tag `dev` never changes, so Kubernetes would not notice an update on its own). `rollout status` waits until the new pods are ready or 120 seconds pass. `up` chains the three steps: cluster, load, dashboard.

### `open`, `status`, `reset`, `dev-frontend`

```makefile
# Makefile lines 81-92
open: ## print the dashboard URL (and try to open a browser)
	@echo $(DASH_URL); (xdg-open $(DASH_URL) || open $(DASH_URL)) >/dev/null 2>&1 || true

status: ## show deployments, replicasets, pods and services of the demo
	$(KUBECTL) get deploy,rs,pods,svc -o wide

reset: ## remove the demo app (all objects labelled app=color-backend), keep the dashboard
	$(KUBECTL) delete deploy,svc -l app=color-backend --ignore-not-found
	-$(KUBECTL) wait --for=delete pod -l app=color-backend --timeout=90s 2>/dev/null

dev-frontend: ## hot-reload the frontend locally against the dashboard in the cluster
	cd frontend && npm run dev
```

`open` prints the URL and tries `xdg-open` (Linux), then `open` (macOS), hiding all output; `|| true` means a missing browser is not an error. `status` shows Deployments, ReplicaSets, pods and Services with `-o wide`, handy for showing how objects relate. `reset` deletes all Deployments and Services with the label `app=color-backend` (`--ignore-not-found` avoids errors on a clean start), and the dashboard survives because it has a different label. The `-` before the second command means "ignore failure", and `wait --for=delete` waits until the old pods are really gone, so the next strategy starts from a clean screen. `dev-frontend` starts the Vite dev server with hot reload.

### Recreate, rolling, upgrade, undo, history

```makefile
# Makefile lines 94-115
# ---------------------------------------------------------------- recreate & rolling
# Both use ONE Deployment named color-backend; they differ only in spec.strategy.

recreate-start: reset ## RECREATE: deploy v1 x6 with strategy Recreate
	$(KUBECTL) apply -f $(STRAT)/recreate/
	$(KUBECTL) rollout status deploy/color-backend --timeout=180s

rolling-start: reset ## ROLLING: deploy v1 x6 with strategy RollingUpdate
	$(KUBECTL) apply -f $(STRAT)/rolling/
	$(KUBECTL) rollout status deploy/color-backend --timeout=180s

upgrade: ## change the image of deploy/color-backend: make upgrade TO=v2|v3|v4
	$(KUBECTL) set image deploy/color-backend app=color-backend:$(TO)
	$(KUBECTL) annotate deploy/color-backend kubernetes.io/change-cause="upgrade to $(TO)" --overwrite
	$(KUBECTL) rollout status deploy/color-backend --timeout=180s

undo: ## roll deploy/color-backend back to the previous revision
	$(KUBECTL) rollout undo deploy/color-backend
	$(KUBECTL) rollout status deploy/color-backend --timeout=180s

history: ## show the revision history of deploy/color-backend
	$(KUBECTL) rollout history deploy/color-backend
```

The two start targets depend on `reset`, so each begins clean, then `apply` the folder of YAML and wait until the rollout finishes. Recreate and rolling share one Deployment name and differ only in `spec.strategy`. `upgrade` is the heart of these lessons: `set image` changes the container image, which is exactly what makes Kubernetes start a rollout. `annotate ... change-cause` writes a note shown in `rollout history`. `TO` comes from the variable at the top. `undo` goes back one revision, and `history` lists revisions.

### Blue/green

```makefile
# Makefile lines 117-135
# ---------------------------------------------------------------- blue / green
# Two full Deployments (blue = v1, green = v2). The Service's `slot` selector decides who is live.

bg-start: reset ## BLUE/GREEN: blue (v1 x4) live behind the Service
	$(KUBECTL) apply -f $(STRAT)/blue-green/deployment-blue.yaml -f $(STRAT)/blue-green/service.yaml
	$(KUBECTL) rollout status deploy/color-backend-blue --timeout=180s

bg-deploy-green: ## start green (v2 x4) next to blue + preview Service; users still on blue
	$(KUBECTL) apply -f $(STRAT)/blue-green/deployment-green.yaml -f $(STRAT)/blue-green/service-preview.yaml
	$(KUBECTL) rollout status deploy/color-backend-green --timeout=180s

bg-switch: ## point the live Service at green (instant cut-over)
	$(KUBECTL) patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"green"}}}'

bg-rollback: ## point the live Service back at blue
	$(KUBECTL) patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"blue"}}}'

bg-retire-blue: ## after a successful switch: scale blue to 0 to free resources
	$(KUBECTL) scale deploy/color-backend-blue --replicas=0
```

`bg-start` applies two files: the blue Deployment and the Service that selects `slot: blue`. `bg-deploy-green` adds the green Deployment next to it and a *preview* Service, so you can test green while users stay on blue. The switch is a one-line `patch` of the Service selector to `slot: green`: nothing is restarted, only the list of endpoints changes, so it is instant. `bg-rollback` patches it back. `bg-retire-blue` scales blue to zero to free resources once you are sure.

### Canary

```makefile
# Makefile lines 137-149
# ---------------------------------------------------------------- canary
# stable (v1) + canary (TO) behind one Service; traffic share ≈ replica ratio.

canary-start: reset ## CANARY: stable v1 x9, no canary yet
	$(KUBECTL) apply -f $(STRAT)/canary/deployment-stable.yaml -f $(STRAT)/canary/service.yaml
	$(KUBECTL) rollout status deploy/color-backend-stable --timeout=180s

canary-deploy: ## add 1 canary pod (≈10%): make canary-deploy TO=v2 (or TO=v3 for a bad one)
	$(KUBECTL) apply -f $(STRAT)/canary/deployment-canary.yaml
	$(KUBECTL) set image deploy/color-backend-canary app=color-backend:$(TO)
	$(KUBECTL) scale deploy/color-backend-stable --replicas=9
	$(KUBECTL) scale deploy/color-backend-canary --replicas=1
	$(KUBECTL) rollout status deploy/color-backend-canary --timeout=180s
```

`canary-start` runs 9 stable pods. `canary-deploy` creates the canary Deployment, sets its image to `$(TO)`, and sets the replica counts to 9 stable and 1 canary. The Service selects only `app=color-backend`, so it spreads traffic over all 10 pods: roughly 10% to the canary.

```makefile
# Makefile lines 151-159
canary-30: ## shift to ≈30% canary (stable 7, canary 3)
	$(KUBECTL) scale deploy/color-backend-canary --replicas=3
	$(KUBECTL) rollout status deploy/color-backend-canary --timeout=180s
	$(KUBECTL) scale deploy/color-backend-stable --replicas=7

canary-50: ## shift to ≈50% canary (stable 5, canary 5)
	$(KUBECTL) scale deploy/color-backend-canary --replicas=5
	$(KUBECTL) rollout status deploy/color-backend-canary --timeout=180s
	$(KUBECTL) scale deploy/color-backend-stable --replicas=5
```

`canary-30` and `canary-50` move the ratio by changing replica counts (3 of 10 and 5 of 10). Notice the order: first scale the **canary up** and wait until it is ready, then scale stable down. That way total ready capacity never drops.

```makefile
# Makefile lines 161-170
canary-promote: ## canary looks good: stable becomes TO, canary removed
	$(KUBECTL) set image deploy/color-backend-stable app=color-backend:$(TO)
	$(KUBECTL) scale deploy/color-backend-stable --replicas=9
	$(KUBECTL) rollout status deploy/color-backend-stable --timeout=300s
	$(KUBECTL) scale deploy/color-backend-canary --replicas=0

canary-abort: ## canary looks bad: restore stable capacity first, then remove the canary
	$(KUBECTL) scale deploy/color-backend-stable --replicas=9
	$(KUBECTL) rollout status deploy/color-backend-stable --timeout=180s
	$(KUBECTL) scale deploy/color-backend-canary --replicas=0
```

`canary-promote` makes the stable Deployment run the new image (a normal rolling update inside it), waits for it, and only then removes the canary. `canary-abort` does the reverse: restore full stable capacity first, wait until ready, then remove the canary. In both cases capacity is added before it is removed.

---

## Part 5: helpers in `scripts/e2e.sh`

The script drives each strategy through `make`, and it reads `/api/state` from the dashboard to check what really happens. The helper functions are the interesting part.

### Safe mode and settings

```bash
# scripts/e2e.sh lines 1-13
#!/usr/bin/env bash
# =====================================================================
# End-to-end test. It drives every strategy through the Makefile and
# checks, using the dashboard's /api/state JSON, that the traffic
# behaves the way the lectures say it does.
# Needs: a running cluster with the dashboard (`make up`), curl, jq.
# =====================================================================
set -euo pipefail

STATE_URL="${STATE_URL:-http://localhost:30080/api/state}"
TIMEOUT="${TIMEOUT:-240}"
SAMPLES="$(mktemp)"
trap 'rm -f "$SAMPLES"' EXIT
```

`set -euo pipefail` makes bash strict: `-e` stops at the first failing command, `-u` fails on an unset variable, and `pipefail` makes a pipeline fail if any part fails. `${STATE_URL:-default}` means "use the environment variable, or this default". `mktemp` creates a temp file for samples, and `trap ... EXIT` deletes it however the script ends.

### jq helper functions

```bash
# scripts/e2e.sh lines 15-25
# jq helpers available in every expression:
#   pct("v2")    → % of the recent requests answered by v2
#   errs("v3")   → failed requests answered by v3
#   ready("v2")  → number of Ready pods running v2
#   pods("v2")   → number of pods (any state) running v2
JQ_DEFS='
def pct($v):   ([.traffic.byVersion[] | select(.version == $v) | .percent] | add // 0);
def errs($v):  ([.traffic.byVersion[] | select(.version == $v) | .errors]  | add // 0);
def ready($v): ([.pods[] | select(.version == $v and .status == "Ready")] | length);
def pods($v):  ([.pods[] | select(.version == $v)] | length);
'
```

`jq` is a command-line JSON tool. These are four small functions in the jq language, put in front of every expression. `pct("v2")` returns the percent of traffic for v2: select the entry with that version, collect `.percent` in a list and `add`; `// 0` gives 0 if the list is empty (no such version). The others count errors, Ready pods and pods per version. With these, a test line like `pct("v1") >= 99` reads almost like English.

### Print helpers

```bash
# scripts/e2e.sh lines 27-31
log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
pass() { printf '    \033[32m✔ %s\033[0m\n' "$*"; }
fail() { printf '    \033[31m✘ %s\033[0m\n' "$*"; dump; exit 1; }
dump() { curl -s "$STATE_URL" | jq '{traffic: .traffic, pods: [.pods[] | {name, version, status, hits}]}' || true; }
state() { curl -sf "$STATE_URL"; }
```

`log` prints a blue heading, `pass` a green tick, `fail` a red cross, then shows the current state with `dump` and exits with code 1. `state` fetches the JSON (`-f` makes curl fail on HTTP errors such as the 503 before the first snapshot).

### `wait_for`

```bash
# scripts/e2e.sh lines 33-41
# wait_for "<description>" '<jq boolean>' [timeout]: poll until the expression is true.
wait_for() {
  local desc="$1" expr="$2" deadline=$((SECONDS + ${3:-$TIMEOUT}))
  until state | jq -e "$JQ_DEFS $expr" >/dev/null 2>&1; do
    (( SECONDS > deadline )) && fail "timeout: $desc"
    sleep 1
  done
  pass "$desc"
}
```

Polls until an expression is true. `local` keeps variables inside the function. `SECONDS` is a built-in bash counter of seconds since start, so `deadline` is "now plus timeout". `jq -e` sets the exit code from the result (0 for true), and output is thrown away with `>/dev/null`. The `until` loop checks, and if time is up the `fail` is called; otherwise it sleeps one second. The optional third argument overrides the timeout (used for "within 30s" checks).

### `observe`, `seen`, `never`

```bash
# scripts/e2e.sh lines 43-57
# observe '<jq boolean>': record one snapshot per second into $SAMPLES until the expression is true.
observe() {
  local expr="$1" deadline=$((SECONDS + TIMEOUT)) snap
  : > "$SAMPLES"
  while true; do
    snap="$(state)" || { sleep 1; continue; }
    echo "$snap" | jq -c . >> "$SAMPLES"
    echo "$snap" | jq -e "$JQ_DEFS $expr" >/dev/null 2>&1 && break
    (( SECONDS > deadline )) && fail "timeout while observing: $expr"
    sleep 1
  done
}
# seen / never: assertions over the recorded samples.
seen()  { if jq -s -e "$JQ_DEFS any(.[]; $2)" "$SAMPLES" >/dev/null; then pass "$1"; else fail "never observed: $1"; fi; }
never() { if jq -s -e "$JQ_DEFS all(.[]; ($2) | not)" "$SAMPLES" >/dev/null; then pass "$1"; else fail "unexpectedly observed: $1"; fi; }
```

Some facts last only a moment (for example "requests failed while no pod was ready"), so polling at the end would miss them. `observe` saves **one snapshot per second** into the sample file until a finish condition is true. Afterwards, `seen` checks that the condition was true in **at least one** sample (`any`), and `never` checks that it was true in **none** (`all` ... `not`). `jq -s` reads the file as one array. Together they let the script say "v1 and v2 were never Ready at the same time" (Recreate) or "no failed requests during the whole rollout" (Rolling).

### Putting it together: two scenarios

```bash
# scripts/e2e.sh lines 59-69
state >/dev/null || { echo "dashboard not reachable at $STATE_URL, run 'make up' first"; exit 1; }

# ------------------------------------------------------------------ recreate
log "Recreate: v1 → v2 should cause downtime"
make -s recreate-start >/dev/null
wait_for "100% of traffic on v1" 'pct("v1") >= 99'
make -s upgrade TO=v2 >/dev/null & UPGRADE=$!
observe 'pct("v2") >= 99'
wait "$UPGRADE"
seen  "requests failed while no pod was ready" '.traffic.errors > 0'
never "v1 and v2 pods were never ready at the same time" 'ready("v1") > 0 and ready("v2") > 0'
```

First check the dashboard is reachable. Then the Recreate scenario: start, wait until all traffic is v1, run the upgrade **in the background** (`&`, saving its process id in `UPGRADE`), observe until v2 serves everything, and `wait` for the upgrade. Finally two assertions: errors happened, and the two versions were never ready at once. This is the lecture's claim, turned into a test.

```bash
# scripts/e2e.sh lines 71-79
# ------------------------------------------------------------------ rolling
log "Rolling update: v1 → v2 without errors, both versions serve during the rollout"
make -s rolling-start >/dev/null
wait_for "100% of traffic on v1" 'pct("v1") >= 99'
make -s upgrade TO=v2 >/dev/null & UPGRADE=$!
observe 'pct("v2") >= 99 and pods("v1") == 0'
wait "$UPGRADE"
seen  "v1 and v2 served at the same time" 'pct("v1") > 0 and pct("v2") > 0'
never "no failed requests"                '.traffic.errors > 0'
```

The Rolling scenario has the same shape but asserts the opposite: both versions served at the same time, and there were no failed requests at all.

---

## Try it yourself

1. **List the targets.** Run `make` with no arguments. Expected: a colored table of every target with its description (that is `help`).
2. **Dry-run a target.** `make -n upgrade TO=v3`. Expected: make prints the three `kubectl` commands with `app=color-backend:v3` filled in, without running anything.
3. **Look inside an image.** `docker run --rm --entrypoint sh color-backend:v1 -c "echo hi"`. Expected: an error that `sh` is not found: distroless has no shell. Then `docker image ls color-backend:v1`: the size is only a few MB.
4. **Check the baked-in settings.** `docker inspect color-backend:v3 --format '{{.Config.Env}}'`. Expected: the list contains `ERROR_RATE=0.3` and `VERSION=v3`.
5. **See `kind load` matter.** `docker exec deploy-demo-worker crictl images | grep color-backend`. Expected: all four backend tags are listed, because `make load` copied them into the node.
6. **Use the jq helpers by hand.** `curl -s localhost:30080/api/state | jq 'def pct($v): ([.traffic.byVersion[] | select(.version == $v) | .percent] | add // 0); pct("v1")'`. Expected: a number such as `100` if v1 serves everything, or `0` if there is no v1 traffic.
