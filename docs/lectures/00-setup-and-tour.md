# Lecture 0: Setup and Tour
**Duration:** 45 min · **You will need:** `make up` done, dashboard open on the projector (http://localhost:30080)

## Learning goals
- Explain what a deployment strategy is and why we need one.
- Describe the parts of this project: backend pods, Service, dashboard, browser.
- Perform `make up` and explain what each of its three steps does.
- Read the dashboard: box states, traffic bar and event log.
- Explain why the traffic probe opens a new connection for every request.

## 0. Before class (instructor checklist)
- [ ] Docker is running: `docker ps`
- [ ] Tools are installed: `kubectl version --client`, `kind version`, `go version`, `node --version`, `jq --version`
- [ ] If `kind` is missing: `make kind-install` (then make sure `$(go env GOPATH)/bin` is in your `PATH`)
- [ ] Start everything: `make up` (first run takes a few minutes)
- [ ] Define the shortcut used in all lectures: `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] Open http://localhost:30080 on the projector. The top-right label should say it is connected.
- [ ] Clean start: `make reset` (the dashboard stays, the page shows no boxes)
- [ ] Open a second terminal on the projector for `$KUBECTL get pods -w`

## 1. The idea (≈5 min)
> **Say:** "A deployment strategy answers one question: how do we replace the old version of our app with a new version? We must decide how many users may see an error, how much extra hardware we pay for, and how fast we can go back."
>
> **Say:** "Today we do not learn a strategy yet. Today we learn the tool. On the screen, every colored box is one real pod in a real Kubernetes cluster. Blue means version 1. Green means version 2. When we change the cluster, the boxes change."

- Analogy: changing the tires of a bus. You can stop the bus and change all tires (Recreate). You can change one tire at a time while the bus drives slowly (Rolling). You can bring a second bus and move the passengers (Blue/Green). You can send one passenger on the new bus first (Canary).
- Timeline of this project (the four strategies we will see):

```
Strategy       time ──────────────────────────────────────────────►
Recreate       v1 v1 v1 v1 | (nothing) |  v2 v2 v2 v2
Rolling        v1 v1 v1 v1 | v1 v1 v1 v2 | v1 v2 v2 v2 | v2 v2 v2 v2
Blue/Green     blue v1 (live)  + green v2 (idle) ── switch ──► green v2 (live)
Canary         v1 x9 + v2 x1 ──► v1 x7 + v2 x3 ──► v1 x5 + v2 x5 ──► v2 x9
```

### The architecture in one picture
```
Browser ──► dashboard pod ──► Kubernetes API   (which pods exist?)
                 │
                 ├──► each pod IP, 1 per second   (which version is each pod?)
                 └──► Service color-backend, 20 per second  (what do users get?)
```
Read the full explanation in [docs/architecture.md](../architecture.md).

### Why does the project use `kind`?
`kind` means "Kubernetes IN Docker". Every Kubernetes node is a Docker container. We get a real cluster on a laptop in about one minute, and we can delete it with one command.

## 2. Read the YAML together, line by line (≈10 min)
This lecture has no strategy YAML yet. The only file is the cluster definition. Strategy Deployments start in [Lecture 1](01-recreate.md).

File: `kind/cluster.yaml`

```yaml
# =====================================================================
# kind cluster for the deployment-strategies class.
# kind = "Kubernetes IN Docker": every Kubernetes node is a Docker container.
# Create it with:  kind create cluster --config kind/cluster.yaml
# =====================================================================
kind: Cluster                          # object type understood by the kind CLI (not by Kubernetes)
apiVersion: kind.x-k8s.io/v1alpha4     # version of kind's config file format
name: deploy-demo                      # cluster name → kubectl context becomes "kind-deploy-demo"
nodes:                                 # list of nodes (each one is a Docker container)
  - role: control-plane                # node 1: runs the API server, scheduler, etcd, controllers
    extraPortMappings:                 # publish ports of this node container on your laptop
      - containerPort: 30080           # NodePort of the dashboard Service inside the node
        hostPort: 30080                # same port on localhost → http://localhost:30080
        protocol: TCP                  # plain TCP (HTTP and WebSocket both ride on it)
  - role: worker                       # node 2: runs application pods
  - role: worker                       # node 3: second worker so pods spread over two nodes
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 6 | `kind: Cluster` | This is a kind file, not a Kubernetes object. The kind CLI reads it. |
| 7 | `apiVersion: kind.x-k8s.io/v1alpha4` | The version of kind's own file format. |
| 8 | `name: deploy-demo` | The cluster name. The kubectl context becomes `kind-deploy-demo`, which is why every kubectl command in the Makefile uses `--context kind-deploy-demo`. |
| 9 | `nodes:` | The list of nodes. Each one is a Docker container. |
| 10 | `role: control-plane` | Node 1 runs the API server, scheduler and controllers. |
| 11-14 | `extraPortMappings` ... `hostPort: 30080` | Opens port 30080 of your laptop and sends it into the node. The dashboard Service uses NodePort 30080, so http://localhost:30080 reaches the dashboard. |
| 15-16 | two `role: worker` nodes | Application pods run on the two workers. With two workers, pods spread over two nodes, which is more like real life. |

Other base files you can show quickly (no need to read every line): `k8s/base/00-namespace.yaml` (the namespace `color-demo`), `k8s/base/10-dashboard-rbac.yaml` (the dashboard may only **read** pods), `k8s/base/20-dashboard.yaml` (the dashboard Deployment and the NodePort Service).

## 3. Live demo (≈15 min)
`make up` is three targets in one line: `cluster-up`, `load`, `dashboard`. Explain them (they already ran earlier) (or run `make up` again; it is safe, it skips an existing cluster).

> **Predict first:** "Which of these three steps needs the internet? Which one needs Docker only?" (Answer: building images needs the internet the first time to download base images. `kind load` only copies local images into the node containers.)

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Create the cluster | `make cluster-up`<br>raw: `kind get clusters` then `kind create cluster --config kind/cluster.yaml` | Nothing yet (browser not reachable if the cluster is new). | **Say:** "kind creates three Docker containers: one control plane and two workers." Ask: "Run `docker ps`. How many kind containers do you see?" (3) |
| 2. Load the images | `make load`<br>raw: `kind load docker-image --name deploy-demo color-backend:v1 color-backend:v2 color-backend:v3 color-backend:v4 color-dashboard:dev` | Nothing yet. | **Say:** "kind nodes cannot see the images in your Docker. We must copy them in. Without this you get `ErrImagePull`." |
| 3. Deploy the dashboard | `make dashboard`<br>raw: `$KUBECTL apply -f k8s/base/`<br>`$KUBECTL rollout restart deploy/color-dashboard`<br>`$KUBECTL rollout status deploy/color-dashboard --timeout=120s` | The page loads. Connected label is green. No boxes, empty traffic bar. | **Say:** "The dashboard is itself a pod. It is not drawn, because it does not have the label `app=color-backend`." |
| 4. Show what exists | `make status`<br>raw: `$KUBECTL get deploy,rs,pods,svc -o wide` | Still no boxes. | Ask: "Where are the backend pods?" (We have not deployed any yet.) |
| 5. Start the watch | In terminal 2: `$KUBECTL get pods -w` | Nothing changes. | **Say:** "Keep this terminal next to the browser. The terminal shows text, the dashboard shows the same facts as pictures." |
| 6. First app | `make rolling-start`<br>raw: `$KUBECTL apply -f k8s/strategies/rolling/`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | Six royalblue boxes appear in a panel "v1". First dashed (Pending), then striped (Starting), then solid (Ready). The traffic bar grows to 100% v1. | **Say:** "Look at the event log: `Pending → Starting → Ready`. Now look at the terminal: the same events." Ask: "Why do the boxes become solid one by one, not all together?" (Each pod waits 5 s, `STARTUP_DELAY`, then passes readiness at a slightly different time.) |
| 7. Keep-alive demo | see the section below | The `hits` counter in each box counts requests from the last window. | See below. |

### Dashboard tour (box states)
| What you see | Meaning |
|---|---|
| Dashed outline, no fill | **Pending**: the pod object exists, but it is not running yet (waiting for a node or for the image). |
| Animated stripes | **Starting**: the container is running but **not ready**. It receives no traffic. |
| Solid color | **Ready and serving**: it is in the Service and answers requests. Badge says `serving`. |
| Dimmed, badge "no traffic" | **Ready but receives no traffic**: it passed readiness, but in the last window no request reached it. This is what the idle green slot looks like in Blue/Green. |
| Faded and a bit smaller | **Terminating**: Kubernetes is shutting this pod down. |
| Red ring, badge "N errors" | The pod returned HTTP errors to the traffic probe. |

The **traffic bar** at the top shows who answered the **last 100 requests** sent at 20 requests per second through the Service named `color-backend`. That is a window of about 5 seconds, so the bar is always a little late. A grey/red segment named **unreachable** means no pod answered at all.

The **event log** on the right lists changes: `+ pod created`, `Starting → Ready`, `- pod removed`, `traffic: v2 now serves 60%`.

The panel header `v2 · 3/4 ready · 40% of traffic` means: 3 of the 4 pods of version v2 are Ready, and v2 answered 40% of the last 100 requests.

### Why the probe opens a new connection every time (Step 7)
A Kubernetes Service is not a real process. `kube-proxy` writes rules on each node, and these rules choose a pod **when a new TCP connection starts**. If a client keeps one connection open (keep-alive), all its requests go to the same pod. The dashboard disables keep-alive so every request makes a new choice.

Demo with a temporary pod (needs internet to pull `curlimages/curl`):

```bash
$KUBECTL run curl --rm -it --image=curlimages/curl --restart=Never -- sh
```

Inside the pod, first a **new connection per request** (six separate `curl` processes):

```sh
for i in 1 2 3 4 5 6; do curl -s http://color-backend/api/color; echo; done
```

> **Predict first:** "Will the `pod` field be the same in all six lines?" (No: different pod names appear.)

Now **one connection reused** (curl reuses the connection when you give several URLs in one command):

```sh
curl -s -w '\n' http://color-backend/api/color http://color-backend/api/color http://color-backend/api/color http://color-backend/api/color http://color-backend/api/color http://color-backend/api/color
```

> **Say:** "Same Service, same six requests, but now the `pod` name is the same every time. If our dashboard did this, the traffic bar would show only one pod, and the lessons would be wrong." Type `exit` to leave and delete the temporary pod.

## 4. Break it on purpose (≈10 min)
| Scenario | Commands | What the dashboard shows |
|---|---|---|
| Delete a pod by hand | `make status`, then `$KUBECTL delete pod <one pod name>` | The box fades (Terminating). A new dashed box appears at once and becomes solid after about 5 s. The Deployment keeps 6 pods. |
| Wrong image tag | `$KUBECTL set image deploy/color-backend app=color-backend:nope` | A new dashed box that never starts (`ErrImagePull` in `$KUBECTL get pods`). The 6 old pods stay Ready: the rollout waits. Fix: `make undo`. |
| Dashboard offline | `$KUBECTL scale deploy/color-dashboard --replicas=0` | The page shows it lost the connection. The app itself is unaffected. Restore with `$KUBECTL scale deploy/color-dashboard --replicas=1`. |

> **Say:** "The dashboard only watches. If the dashboard dies, your application does not notice."

## 5. Discussion questions (with answers in a collapsed <details> block)
1. Why do we use a Service name (`color-backend`) and not pod IPs?
2. The dashboard has two kinds of probes: one to each pod IP and one to the Service. Why both?
3. A pod is Running in `kubectl get pods` but the box is striped. How is that possible?

<details>
<summary>Answers</summary>

1. Pod IPs change every time a pod is replaced. The Service has one stable name and IP, and it only sends traffic to Ready pods. This is exactly the thing that deployment strategies use.
2. The pod-IP probe tells us **what each pod is** (its version and color). The Service probe tells us **what a real user gets**. They can disagree: a pod can be healthy but receive no traffic.
3. Running only means the container started. Ready means the readiness probe (`/readyz`) passes. The backend waits `STARTUP_DELAY` (5 s) before it says ready. The striped box is Running but not Ready.
</details>

## 6. Recap: pros / cons / when to use
This lecture is about the tool, so the table is about the **dashboard as a teaching tool**.

| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| Strategies become visible; students predict and check | Local `kind` has only 3 nodes and no real load balancer | Teaching and demos |
| One command (`make up`) builds everything | Traffic bar lags about 5 s (100-request window) | You want to compare strategies side by side |
| Every `make` target prints the real `kubectl` command | Percentages are replica ratios, not exact weights (see Lecture 4) | You explain how Kubernetes works under the hood |

## 7. Lab / homework
1. **Easy.** Run `make help`. For five targets, write the raw `kubectl` command from the Makefile next to the name.
   *Expected:* five correct pairs, e.g. `make history` = `$KUBECTL rollout history deploy/color-backend`.
2. **Medium.** With `make rolling-start` running, run `$KUBECTL get pods -o wide` and match each pod name to a box on the dashboard (the box shows the pod name and node). Which node runs which pods?
   *Expected:* a table of 6 pods with their nodes (two worker nodes).
3. **Harder.** Change `PROBE_RPS` or `WINDOW_SIZE` of the dashboard with `$KUBECTL set env deploy/color-dashboard WINDOW_SIZE=20`, then run `make rolling-start` and `make upgrade TO=v2`. What changes on the traffic bar? Restore with `$KUBECTL set env deploy/color-dashboard WINDOW_SIZE=100`.
   *Expected:* with a window of 20 the bar reacts in about 1 second but jumps a lot, because it only remembers 20 requests.

Next: [Lecture 1: Recreate](01-recreate.md)
