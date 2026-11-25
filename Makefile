# =====================================================================
#  Deployment Strategies Live: classroom command shortcuts
#  `make` (or `make help`) lists every target with its description.
# =====================================================================

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

.DEFAULT_GOAL := help
.PHONY: help test test-backend test-dashboard test-frontend images image-backend image-dashboard \
        kind-install cluster-up cluster-down load dashboard up reset status open dev-frontend e2e \
        recreate-start rolling-start upgrade undo history \
        bg-start bg-deploy-green bg-switch bg-rollback bg-retire-blue \
        canary-start canary-deploy canary-30 canary-50 canary-promote canary-abort

help: ## list all targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*## "} {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

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

# ---------------------------------------------------------------- images
images: image-backend image-dashboard ## build every container image

image-backend: ## build color-backend:v1..v4 (one Dockerfile, different build args)
	docker build -t color-backend:v1 --build-arg VERSION=v1 --build-arg COLOR=royalblue backend
	docker build -t color-backend:v2 --build-arg VERSION=v2 --build-arg COLOR=mediumseagreen backend
	docker build -t color-backend:v3 --build-arg VERSION=v3 --build-arg COLOR=darkorange --build-arg ERROR_RATE=0.3 backend
	docker build -t color-backend:v4 --build-arg VERSION=v4 --build-arg COLOR=mediumpurple --build-arg NEVER_READY=true backend

image-dashboard: ## build color-dashboard:dev (frontend + Go server)
	docker build -t color-dashboard:dev -f dashboard/Dockerfile .

# ---------------------------------------------------------------- cluster
kind-install: ## install the kind CLI with Go
	go install sigs.k8s.io/kind@$(KIND_VERSION)

cluster-up: ## create the kind cluster (skipped if it already exists)
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --config kind/cluster.yaml

cluster-down: ## delete the kind cluster
	kind delete cluster --name $(CLUSTER)

load: images ## copy local images into the kind nodes (kind cannot see your Docker images otherwise)
	kind load docker-image --name $(CLUSTER) \
	  color-backend:v1 color-backend:v2 color-backend:v3 color-backend:v4 color-dashboard:dev

dashboard: ## deploy (or redeploy) the dashboard
	$(KUBECTL) apply -f k8s/base/
	$(KUBECTL) rollout restart deploy/color-dashboard
	$(KUBECTL) rollout status deploy/color-dashboard --timeout=120s

up: cluster-up load dashboard ## everything: cluster + images + dashboard
	@echo "Dashboard ready → $(DASH_URL)"

open: ## print the dashboard URL (and try to open a browser)
	@echo $(DASH_URL); (xdg-open $(DASH_URL) || open $(DASH_URL)) >/dev/null 2>&1 || true

status: ## show deployments, replicasets, pods and services of the demo
	$(KUBECTL) get deploy,rs,pods,svc -o wide

reset: ## remove the demo app (all objects labelled app=color-backend), keep the dashboard
	$(KUBECTL) delete deploy,svc -l app=color-backend --ignore-not-found
	-$(KUBECTL) wait --for=delete pod -l app=color-backend --timeout=90s 2>/dev/null

dev-frontend: ## hot-reload the frontend locally against the dashboard in the cluster
	cd frontend && npm run dev

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

canary-30: ## shift to ≈30% canary (stable 7, canary 3)
	$(KUBECTL) scale deploy/color-backend-canary --replicas=3
	$(KUBECTL) rollout status deploy/color-backend-canary --timeout=180s
	$(KUBECTL) scale deploy/color-backend-stable --replicas=7

canary-50: ## shift to ≈50% canary (stable 5, canary 5)
	$(KUBECTL) scale deploy/color-backend-canary --replicas=5
	$(KUBECTL) rollout status deploy/color-backend-canary --timeout=180s
	$(KUBECTL) scale deploy/color-backend-stable --replicas=5

canary-promote: ## canary looks good: stable becomes TO, canary removed
	$(KUBECTL) set image deploy/color-backend-stable app=color-backend:$(TO)
	$(KUBECTL) scale deploy/color-backend-stable --replicas=9
	$(KUBECTL) rollout status deploy/color-backend-stable --timeout=300s
	$(KUBECTL) scale deploy/color-backend-canary --replicas=0

canary-abort: ## canary looks bad: restore stable capacity first, then remove the canary
	$(KUBECTL) scale deploy/color-backend-stable --replicas=9
	$(KUBECTL) rollout status deploy/color-backend-stable --timeout=180s
	$(KUBECTL) scale deploy/color-backend-canary --replicas=0
