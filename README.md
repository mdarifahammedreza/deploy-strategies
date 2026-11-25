# Deployment Strategies Live

Every colored box on a live web page is **one real backend pod** in a Kubernetes cluster. Watch Recreate, Rolling Update, Blue/Green and Canary happen in front of your eyes, while a traffic bar shows which version really answers the users.

![Dashboard in the middle of a rolling update: v1 and v2 pods serve traffic together](docs/images/rolling-mid.png)

## Prerequisites
| Tool | Version | Check |
|---|---|---|
| Docker | running daemon | `docker ps` |
| Go | 1.26 | `go version` |
| Node.js | 24 | `node --version` |
| kubectl | any recent | `kubectl version --client` |
| make | any | `make --version` |
| jq | any (used by `make e2e`) | `jq --version` |
| kind | v0.33.0 | `kind version` |

If `kind` is missing, install it with Go:

```bash
make kind-install      # runs: go install sigs.k8s.io/kind@v0.33.0
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

## Quick start
```bash
make up                # kind cluster + all images + dashboard (first run: a few minutes)
make open              # prints http://localhost:30080 (and tries to open a browser)
make rolling-start     # deploy v1 x6 with a rolling-update Deployment
make upgrade TO=v2     # roll out v2 and watch the boxes change
```

Open http://localhost:30080 before you run the last two commands. You will see six royalblue boxes turn into six mediumseagreen boxes, one by one, with no errors.

## The four images
All four images are built from one Dockerfile with different build arguments.

| Image | Color | Behaviour | Lesson |
|---|---|---|---|
| `color-backend:v1` | royalblue | healthy | baseline |
| `color-backend:v2` | mediumseagreen | healthy | normal upgrade |
| `color-backend:v3` | darkorange | 30% HTTP 500, but passes readiness | readiness is not correctness; a bad canary |
| `color-backend:v4` | mediumpurple | never ready | stuck rollout; `maxUnavailable: 0` protects users |

## Command cheat-sheet
Run `make help` to list every target. In the tables, `$KUBECTL` means:

```bash
export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"
```

### Cluster, images and dashboard
| make target | What it does | Raw command |
|---|---|---|
| `make up` | cluster + images + dashboard | `make cluster-up load dashboard` |
| `make cluster-up` | create the kind cluster (skipped if it exists) | `kind create cluster --config kind/cluster.yaml` |
| `make cluster-down` | delete the cluster | `kind delete cluster --name deploy-demo` |
| `make images` | build all images | `docker build -t color-backend:v1 ...` (see `make image-backend`, `make image-dashboard`) |
| `make load` | copy images into the kind nodes | `kind load docker-image --name deploy-demo color-backend:v1 color-backend:v2 color-backend:v3 color-backend:v4 color-dashboard:dev` |
| `make dashboard` | deploy or redeploy the dashboard | `$KUBECTL apply -f k8s/base/`<br>`$KUBECTL rollout restart deploy/color-dashboard`<br>`$KUBECTL rollout status deploy/color-dashboard --timeout=120s` |
| `make status` | show deployments, replicasets, pods, services | `$KUBECTL get deploy,rs,pods,svc -o wide` |
| `make reset` | remove the demo app, keep the dashboard | `$KUBECTL delete deploy,svc -l app=color-backend --ignore-not-found` |

### Recreate
| make target | Raw command |
|---|---|
| `make recreate-start` | `make reset`, then `$KUBECTL apply -f k8s/strategies/recreate/` and `$KUBECTL rollout status deploy/color-backend --timeout=180s` |
| `make upgrade TO=v2` | `$KUBECTL set image deploy/color-backend app=color-backend:v2`<br>`$KUBECTL annotate deploy/color-backend kubernetes.io/change-cause="upgrade to v2" --overwrite`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` |
| `make history` | `$KUBECTL rollout history deploy/color-backend` |
| `make undo` | `$KUBECTL rollout undo deploy/color-backend`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` |

### Rolling Update
`upgrade`, `history` and `undo` are the same targets as for Recreate.

| make target | Raw command |
|---|---|
| `make rolling-start` | `make reset`, then `$KUBECTL apply -f k8s/strategies/rolling/` and `$KUBECTL rollout status deploy/color-backend --timeout=180s` |
| `make upgrade TO=v2` (or `v3`, `v4`) | `$KUBECTL set image deploy/color-backend app=color-backend:v2` + `annotate` + `rollout status` (see above) |
| `make history` | `$KUBECTL rollout history deploy/color-backend` |
| `make undo` | `$KUBECTL rollout undo deploy/color-backend` + `rollout status` |

### Blue/Green
| make target | Raw command |
|---|---|
| `make bg-start` | `make reset`, then `$KUBECTL apply -f k8s/strategies/blue-green/deployment-blue.yaml -f k8s/strategies/blue-green/service.yaml` and `$KUBECTL rollout status deploy/color-backend-blue --timeout=180s` |
| `make bg-deploy-green` | `$KUBECTL apply -f k8s/strategies/blue-green/deployment-green.yaml -f k8s/strategies/blue-green/service-preview.yaml` and `$KUBECTL rollout status deploy/color-backend-green --timeout=180s` |
| `make bg-switch` | `$KUBECTL patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"green"}}}'` |
| `make bg-rollback` | `$KUBECTL patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"blue"}}}'` |
| `make bg-retire-blue` | `$KUBECTL scale deploy/color-backend-blue --replicas=0` |

### Canary
| make target | Raw command |
|---|---|
| `make canary-start` | `make reset`, then `$KUBECTL apply -f k8s/strategies/canary/deployment-stable.yaml -f k8s/strategies/canary/service.yaml` and `$KUBECTL rollout status deploy/color-backend-stable --timeout=180s` |
| `make canary-deploy TO=v2` (or `TO=v3`) | `$KUBECTL apply -f k8s/strategies/canary/deployment-canary.yaml`<br>`$KUBECTL set image deploy/color-backend-canary app=color-backend:v2`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=1`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s` |
| `make canary-30` | `$KUBECTL scale deploy/color-backend-canary --replicas=3`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=7` |
| `make canary-50` | `$KUBECTL scale deploy/color-backend-canary --replicas=5`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=5` |
| `make canary-promote TO=v2` | `$KUBECTL set image deploy/color-backend-stable app=color-backend:v2`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=300s`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=0` |
| `make canary-abort` | `$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=0` |

Abort order matters: restore stable capacity **first**, then remove the canary.

## Reading the dashboard
**Pod boxes.** Each box is one backend pod. The color is the version.

| What you see | Meaning |
|---|---|
| Dashed outline | **Pending**: the pod exists but is not running yet |
| Animated stripes | **Starting**: running but not ready (receives no traffic) |
| Solid color | **Ready** and serving traffic |
| Dimmed, with a "no traffic" badge | Ready, but receives no traffic (for example the idle green slot) |
| Faded and a bit smaller | **Terminating** |
| Red ring | The pod is returning errors (HTTP 500) |

**Panel header.** `v2 · 3/4 ready · 40% of traffic` means 3 of 4 pods of version v2 are Ready, and v2 answered 40% of recent requests.

**Traffic bar.** It shows who answered the **last 100 requests**. The dashboard sends 20 requests per second through the Service named `color-backend`, so the bar covers about 5 seconds and always lags a little. A segment called **unreachable** means no pod answered.

**Event log.** A list of changes, built in the browser by comparing snapshots: `+ pod created`, `Starting → Ready`, `- pod removed`, `traffic: v2 now serves 60%`.

Details: [docs/architecture.md](docs/architecture.md).

## Project layout
```
deploy-strategies/
├── README.md
├── Makefile                           # every classroom command
├── kind/cluster.yaml                  # kind cluster: 1 control plane + 2 workers
├── backend/                           # color API (Go, standard library only)
│   ├── config.go  app.go  main.go  (+ tests)
│   └── Dockerfile
├── dashboard/                         # in-cluster watcher + WebSocket server (Go)
│   ├── main.go  config.go
│   ├── internal/traffic/   window.go prober.go
│   ├── internal/pods/      status.go identity.go
│   ├── internal/state/     snapshot.go
│   ├── internal/hub/       hub.go
│   ├── internal/server/    server.go
│   └── Dockerfile                     # builds frontend + dashboard into one image
├── frontend/                          # Vite + TypeScript page
│   └── src/ types.ts format.ts group.ts events.ts render.ts socket.ts main.ts style.css
├── k8s/
│   ├── base/ 00-namespace.yaml 10-dashboard-rbac.yaml 20-dashboard.yaml
│   └── strategies/
│       ├── recreate/   deployment.yaml service.yaml
│       ├── rolling/    deployment.yaml service.yaml
│       ├── blue-green/ deployment-blue.yaml deployment-green.yaml service.yaml service-preview.yaml
│       └── canary/     deployment-stable.yaml deployment-canary.yaml service.yaml
├── scripts/e2e.sh                     # end-to-end test of every strategy
└── docs/
    ├── architecture.md
    ├── images/rolling-mid.png
    ├── walkthrough/                   # line-by-line code explanations
    └── lectures/                      # instructor scripts
```

## Testing
```bash
make test     # all unit tests: Go backend, Go dashboard (with -race), Vitest frontend
make e2e      # drives every strategy on the live cluster and checks /api/state
```

`make e2e` needs `make up` first, plus `curl` and `jq`. It ends with `ALL STRATEGIES PASSED`.

## Teaching material
Class lectures (instructor scripts with demos, exercises and labs):

| Lecture | Topic |
|---|---|
| [00 Setup and tour](docs/lectures/00-setup-and-tour.md) | What a strategy is, `make up`, the dashboard tour |
| [01 Recreate](docs/lectures/01-recreate.md) | Simple, but with downtime |
| [02 Rolling Update](docs/lectures/02-rolling-update.md) | `maxSurge`, `maxUnavailable`, bad versions v3 and v4 |
| [03 Blue/Green](docs/lectures/03-blue-green.md) | Two Deployments and one Service switch |
| [04 Canary](docs/lectures/04-canary.md) | 10% → 30% → 50% → promote, bad canary, abort |
| [05 Next: kubeadm and ALB](docs/lectures/05-next-kubeadm-alb.md) | Roadmap: ALB weighted target groups, Argo Rollouts |

Code walkthroughs (line by line):

| Walkthrough | Topic |
|---|---|
| [01 Backend](docs/walkthrough/01-backend.md) | The color API |
| [02 Dashboard](docs/walkthrough/02-dashboard.md) | Pod watch, probes, snapshot, WebSocket |
| [03 Frontend](docs/walkthrough/03-frontend.md) | The page that draws the boxes |
| [04 Docker, Make, kind](docs/walkthrough/04-docker-make-kind.md) | Dockerfiles, Makefile and the kind config |

Architecture: [docs/architecture.md](docs/architecture.md).

## Clean up
```bash
make reset          # remove the demo app, keep the dashboard (between strategies)
make cluster-down   # delete the whole kind cluster
```
