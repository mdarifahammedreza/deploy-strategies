# Lecture 2: Rolling Update
**Duration:** 60 min · **You will need:** `make up` done, dashboard open on the projector

## Learning goals
- Explain how `maxSurge` and `maxUnavailable` control a rolling update.
- Predict the order of box states (Pending, Starting, Ready, Terminating) during a rollout.
- Perform an upgrade, read the history and roll back with `make undo`.
- Explain why a version that passes readiness can still hurt users (v3).
- Explain how `maxUnavailable: 0` protects users from a version that never becomes ready (v4).

## 0. Before class (instructor checklist)
- [ ] `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] `make reset`
- [ ] Dashboard open at http://localhost:30080
- [ ] Terminal 2: `$KUBECTL get pods -w`
- [ ] Remember the numbers: 6 replicas, `maxSurge: 1`, `maxUnavailable: 0`, `minReadySeconds: 3`, `STARTUP_DELAY` 5 s, `preStop` 3 s. A full v1 to v2 rollout takes about 1 minute.

## 1. The idea (≈5 min)
> **Say:** "A rolling update replaces the pods a few at a time. First Kubernetes starts one new pod. When it is ready, Kubernetes removes one old pod. Then again, and again, until all pods are new."
>
> **Say:** "During the rollout, v1 and v2 answer requests at the same time. Your app must be able to live with that. In return, users do not see any downtime."

- Analogy: changing the light bulbs in a hall one by one. The hall is never dark, but for a while you have old and new bulbs together.
- Timeline (6 replicas, one step at a time; `+` = new pod starting, `-` = old pod stopping):

```
time ─────────────────────────────────────────────────────────────────►
start    v1 v1 v1 v1 v1 v1
step 1   v1 v1 v1 v1 v1 v1 [v2 starting]        7 pods (maxSurge 1)
step 2   v1 v1 v1 v1 v1 v1  v2 ready, wait 3 s  (minReadySeconds)
step 3   v1 v1 v1 v1 v1 (v1 terminating) v2     old pod goes away
step 4   v1 v1 v1 v1 v1 v2 [v2 starting]        ... repeat ...
end      v2 v2 v2 v2 v2 v2
traffic  100% v1 ─► 83% v1 / 17% v2 ─► 67/33 ─► ... ─► 100% v2   (no errors)
```

## 2. Read the YAML together, line by line (≈10 min)
This is the first Deployment with a real strategy, so here is the **full table** again for every line. Lines that are new compared with Recreate are marked **NEW**. The Service file is the same as in [Lecture 1](01-recreate.md) (`k8s/strategies/rolling/service.yaml`).

### File 1: `k8s/strategies/rolling/deployment.yaml`

```yaml
# =====================================================================
# ROLLING UPDATE: replace pods a few at a time. Old and new versions
# serve together for a while; there is never a moment with zero pods.
# Upgrade:  kubectl -n color-demo set image deploy/color-backend app=color-backend:v2
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages ReplicaSets, which manage pods
metadata:                               # identifying data of the Deployment
  name: color-backend                   # Deployment name (pods are named color-backend-<hash>-<id>)
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find and delete it
  annotations:                          # non-identifying notes attached to the object
    kubernetes.io/change-cause: "initial deploy: v1"  # shown by `kubectl rollout history`
spec:                                   # desired state
  replicas: 6                           # keep 6 pods (6 boxes on the dashboard)
  revisionHistoryLimit: 5               # keep 5 old ReplicaSets so `rollout undo` can go back
  minReadySeconds: 3                    # a new pod must stay Ready 3 s before it counts as available
  progressDeadlineSeconds: 60           # mark the rollout as failed if it makes no progress for 60 s
  strategy:                             # HOW pods are replaced when the template changes
    type: RollingUpdate                 # replace gradually (the default strategy)
    rollingUpdate:                      # knobs of the rolling update
      maxSurge: 1                       # at most 1 pod ABOVE 6 during the update (so 7 max)
      maxUnavailable: 0                 # never go BELOW 6 available pods → zero downtime
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # pods must carry all these labels
      app: color-backend                # the label set in the template below
  template:                             # blueprint of every pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # matched by the selector above AND by the Service
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name (used by `kubectl set image ... app=...`)
          image: color-backend:v1       # the version: change this tag to upgrade
          imagePullPolicy: IfNotPresent # use the image loaded into kind; don't pull from a registry
          ports:                        # ports the container listens on
            - name: http                # name referenced by probes and by the Service
              containerPort: 8080       # the Go server's port
          env:                          # environment variables for the app
            - name: STARTUP_DELAY       # how long the app stays "not ready" after start
              value: "5s"               # slow enough for students to watch each step
          readinessProbe:               # "may this pod receive traffic?" (controls Service endpoints)
            httpGet:                    # check over HTTP...
              path: /readyz             # ...this path (503 until STARTUP_DELAY has passed)
              port: http                # ...on the named port
            periodSeconds: 1            # check every second, so Ready shows up quickly
            failureThreshold: 1         # one failed check → not ready
          livenessProbe:                # "is the process still alive?" (failing → restart)
            httpGet:                    # check over HTTP...
              path: /healthz            # ...the always-OK endpoint
              port: http                # ...on the named port
            periodSeconds: 10           # check every 10 seconds
          lifecycle:                    # hooks around the container's life
            preStop:                    # runs BEFORE SIGTERM is sent
              sleep:                    # built-in sleep action (no shell needed in distroless)
                seconds: 3              # give kube-proxy 3 s to stop routing here → no failed requests
          resources:                    # resource reservations and limits
            requests:                   # reserved by the scheduler
              cpu: 10m                  # 1% of a CPU core
              memory: 16Mi              # 16 MiB RAM
            limits:                     # hard ceiling
              memory: 64Mi              # killed (OOM) above 64 MiB
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-5 | Comment header | One command upgrades: `set image`. |
| 6-7 | `apiVersion: apps/v1`, `kind: Deployment` | A Deployment owns one ReplicaSet per version. During a rollout, two ReplicaSets are active: the old one scales down and the new one scales up. |
| 9 | `name: color-backend` | Same name as in Recreate, so `make upgrade` works the same way. |
| 10-12 | `namespace`, `labels app: color-backend` | Used by `make reset`. |
| 13-14 | `annotations change-cause` | Shown in `make history`. `make upgrade` rewrites it each time. |
| 16 | `replicas: 6` | Six boxes in steady state. |
| 17 | `revisionHistoryLimit: 5` | Old ReplicaSets are kept (at 0 pods) so `make undo` is fast. |
| 18 **NEW** | `minReadySeconds: 3` | A new pod must stay Ready for 3 s before it counts as *available*. Only then is an old pod removed. This protects against pods that flap. |
| 19 **NEW** | `progressDeadlineSeconds: 60` | If the rollout makes no progress for 60 s, Kubernetes marks it failed. `make upgrade` then ends with an error. This is how a stuck v4 rollout is detected. |
| 20-21 | `strategy: type: RollingUpdate` | Replace pods gradually. (This is the default.) |
| 22-24 **NEW** | `rollingUpdate: maxSurge: 1, maxUnavailable: 0` | **The key lines.** `maxSurge: 1`: at most 1 extra pod (7 in total). `maxUnavailable: 0`: never fewer than 6 available pods. So Kubernetes must **add first, remove later**. |
| 25-27 | `selector.matchLabels app: color-backend` | How the Deployment finds its pods. |
| 28-31 | `template.metadata.labels app: color-backend` | The label the Service and the dashboard use. |
| 33 | `terminationGracePeriodSeconds: 30` | Maximum time for a pod to stop after SIGTERM. |
| 35 | `name: app` | Container name used by `set image app=...`. |
| 36 | `image: color-backend:v1` | Changing the tag starts the rollout. |
| 37 | `imagePullPolicy: IfNotPresent` | Use the image loaded into kind. |
| 38-40 | `ports: http 8080` | Named port for probes and Service. |
| 41-43 | `env STARTUP_DELAY=5s` | The pod is Starting (striped) for 5 s. Slow enough to watch. |
| 44-49 | `readinessProbe /readyz`, period 1 s, failureThreshold 1 | Controls when a pod is added to the Service. The rollout moves on only when this passes. |
| 50-54 | `livenessProbe /healthz`, period 10 s | Restarts a stuck process. |
| 55-58 **NEW** | `lifecycle.preStop.sleep 3 s` | Before SIGTERM, the pod waits 3 s. In this time kube-proxy removes it from the Service, so no request reaches a pod that is shutting down. This is why a rolling update has **zero** failed requests. |
| 59-64 | `resources requests/limits` | Small values so seven pods fit on a laptop. |

### File 2: `k8s/strategies/rolling/service.yaml`

```yaml
# =====================================================================
# Service "color-backend": one stable virtual IP + DNS name in front of
# whatever backend pods are READY right now. The dashboard's traffic
# prober calls http://color-backend/api/color, which lands here.
# =====================================================================
apiVersion: v1                 # Services are core v1 objects
kind: Service                  # load-balances to a changing set of pods
metadata:                      # identifying data
  name: color-backend          # DNS name inside the namespace: "color-backend"
  namespace: color-demo        # where it lives
  labels:                      # labels on the Service object itself
    app: color-backend         # lets `make reset` delete it together with the Deployments
spec:                          # desired state
  type: ClusterIP              # reachable only inside the cluster (the dashboard is inside)
  selector:                    # traffic goes to READY pods carrying these labels
    app: color-backend         # every backend pod of this strategy
  ports:                       # port mapping list
    - name: http               # port name
      port: 80                 # clients call port 80 on the Service...
      targetPort: http         # ...which forwards to the container port named "http" (8080)
```

Identical to the Recreate Service. The Service only selects `app: color-backend` and sends traffic to Ready pods, so old and new pods are in the same Service during the rollout. **That is the whole trick of a rolling update.**

## 3. Live demo (≈15 min)
> **Predict first (before `make rolling-start`):** "The Deployment has `replicas: 6`. How many boxes at most will we ever see during the upgrade?" (7, because of `maxSurge: 1`.)

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Start v1 | `make rolling-start`<br>raw: `$KUBECTL apply -f k8s/strategies/rolling/`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | Six royalblue boxes; bar 100% v1. | **Say:** "Same start as before." |
| 2. Predict | (do not press Enter) | | **Ask:** "Will the traffic bar ever show 'unreachable'? How many v1 pods are Ready at every moment?" (Never unreachable. Always at least 6 Ready pods, counting old and new together.) |
| 3. Upgrade to v2 | `make upgrade TO=v2`<br>raw: `$KUBECTL set image deploy/color-backend app=color-backend:v2`<br>`$KUBECTL annotate deploy/color-backend kubernetes.io/change-cause="upgrade to v2" --overwrite`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | A seventh box appears: a **dashed** mediumseagreen box (Pending), then **striped** (Starting), then **solid**. About 3 s later one blue box fades (Terminating) and disappears. A new green panel `v2 · 1/1 ready` grows while the blue panel shrinks. The traffic bar shows blue and green together, with green growing step by step. The legend shows 0 errors. | **Say:** "Watch the count: never more than 7 boxes, never less than 6 Ready." **Ask:** "At the moment when 3 green and 3 blue are Ready, what share do you expect for each color?" (About 50%.) |
| 4. Mid-rollout screenshot | (pause on the screen when both colors are present) | Both versions serve at once. | **Say:** "This is the picture in the README. Two versions, one Service, no errors." |
| 5. Finish | (wait for the command to return, about 1 minute) | Only green boxes, panel `v2 · 6/6 ready · 100% of traffic`. | **Ask:** "Where did the blue pods go?" (Their ReplicaSet has 0 pods now.) |
| 6. History | `make history`<br>raw: `$KUBECTL rollout history deploy/color-backend` | No change. | Show revisions 1 and 2 with their change-cause text. |
| 7. See the ReplicaSets | `make status`<br>raw: `$KUBECTL get deploy,rs,pods,svc -o wide` | No change. | **Say:** "Two ReplicaSets: the old one at 0, the new one at 6. The old one is kept for rollback." |
| 8. Undo | `make undo`<br>raw: `$KUBECTL rollout undo deploy/color-backend`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | The same rolling movement, now green to blue. Still no errors. | **Say:** "Rollback is just another rolling update, in the other direction. It takes the same time." |

## 4. Break it on purpose (≈10 min)
### 4.1 v3: readiness is not correctness
Start from v2: `make upgrade TO=v2` (if you are on v1).

> **Predict first:** "v3 is slow to start like the others and passes the readiness check, but it returns HTTP 500 for 30% of requests. Will Kubernetes stop the rollout?"

| Step | Run | What the dashboard shows | Say |
|---|---|---|---|
| 1 | `make upgrade TO=v3`<br>raw: `$KUBECTL set image deploy/color-backend app=color-backend:v3` + the `annotate` and `rollout status` commands from the table above | Darkorange boxes become Ready one by one and take traffic. The rollout **finishes successfully**. Orange boxes get **red rings** and badges such as `7 errors`. The legend shows `v3 ... (N req, M errors)` in red. About 30% of the requests fail. | **Say:** "Kubernetes asked 'is the pod ready?' and the pod said yes. Kubernetes does not know that 30% of the answers are wrong. Readiness is not correctness." |
| 2 | `make undo` | Rolling movement back to the previous version, errors disappear. | **Say:** "The fix is rollback. A rolling update has no automatic check of the quality of answers. You need monitoring, or a canary (Lecture 4)." |

### 4.2 v4: a version that never becomes ready
> **Predict first:** "v4 never passes readiness. We have `maxUnavailable: 0`. What happens to the users? What happens to the rollout?"

| Step | Run | What the dashboard shows | Say |
|---|---|---|---|
| 1 | `make upgrade TO=v4` (from v2) | **One** purple box appears and stays **striped** forever (Starting). The six green boxes stay solid. The traffic bar stays at 100% green with no errors. | **Say:** "Because `maxUnavailable` is 0, Kubernetes may not remove an old pod until a new pod is available. The new pod never is, so the old pods stay. Users are safe." |
| 2 | wait about 60 s | `make upgrade` prints that the deployment `exceeded its progress deadline` and ends with an error. The purple box is still there. | **Say:** "`progressDeadlineSeconds: 60` makes the failure visible. Kubernetes does **not** roll back for you. It only reports." |
| 3 | `$KUBECTL rollout status deploy/color-backend --timeout=10s` | (terminal) Shows the same error. | Show how a CI pipeline can detect the failure from the exit code. |
| 4 | `make undo` | The purple box disappears. All pods are green again. | **Say:** "Stuck rollout, zero user impact. That is the safety of `maxUnavailable: 0`." |

### 4.3 Wrong settings (optional)
`maxUnavailable: 1` and `maxSurge: 0` mean the update first **removes** a pod, so capacity drops to 5. Show the exercise in the lab.

## 5. Discussion questions (with answers in a collapsed <details> block)
1. What is the maximum and the minimum number of pods during our rollout? Which settings give these numbers?
2. Why do we need the `preStop` sleep of 3 seconds?
3. v3 passed readiness but returned 500. Name two things that would catch this in real life.
4. During a rolling update, v1 and v2 serve users at the same time. What can go wrong with a database or an API?
5. Why is a rolling update slow if `minReadySeconds` is big?

<details>
<summary>Answers</summary>

1. Maximum = 6 + `maxSurge` = 7. Minimum available = 6 − `maxUnavailable` = 6.
2. When a pod is deleted, two things happen at the same time: the pod gets SIGTERM, and the endpoint is removed from the Service. The removal is not instant on every node. The 3 s sleep gives kube-proxy time to stop sending requests before the app begins to shut down.
3. A better readiness probe that tests a real request, monitoring of the error rate (alerts), automated analysis (Argo Rollouts, Lecture 5), and a canary with a small share of traffic first.
4. Both versions must work with the same database schema and understand the same API calls and messages. Use backward-compatible changes (add a column first, remove old things later).
5. Each new pod must stay Ready for that time before the next old pod is removed, so every step takes longer. It is a trade: safer, but slower.
</details>

## 6. Recap: pros / cons / when to use
| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| No downtime (with correct probes and `maxUnavailable: 0`) | Two versions serve at the same time | Most stateless web services: it is the Kubernetes default |
| Built in: no extra tools, one `kubectl set image` | A bad but "ready" version (v3) rolls out to **everyone** | You can make old and new versions compatible |
| Only one extra pod needed (`maxSurge: 1`) | Rollback is another full rollout (about 1 minute) | You do not want to pay for double capacity |
| A stuck rollout (v4) leaves old pods untouched | You cannot choose the share of users on the new version | A quick, simple release path is more important than fine control |

## 7. Lab / homework
1. **Easy.** Run `make rolling-start`, `make upgrade TO=v2`, `make history`, `make undo`, `make history`. Write down the revision numbers before and after the undo.
   *Expected:* after `undo`, a new revision number appears (the old revision is re-used as a new one), and the pods return to v1.
2. **Medium.** Edit a copy of the file or use `$KUBECTL patch deploy color-backend -p '{"spec":{"strategy":{"rollingUpdate":{"maxSurge":3,"maxUnavailable":2}}}}'`. Before you run `make upgrade TO=v2`, predict: the max number of boxes, the min number of Ready boxes, and whether the traffic bar will show errors.
   *Expected:* up to 9 boxes (6 + 3) and at least 4 Ready (6 − 2). The rollout is faster: 3 new pods start together. The bar should still show no errors, because there are always Ready pods. (Pods may look "faded" in groups of two or three.)
3. **Harder.** Set `maxSurge: 0` and `maxUnavailable: 1` (same patch idea). Run `make upgrade TO=v2`. Describe what is different about the first step compared with the lecture, and what could go wrong with only 5 Ready pods during a busy time.
   *Expected:* the first step is a **removal** (one blue box fades) before any green box appears, so the Ready count drops to 5. The rollout is correct but capacity is lower. If the cluster is full (no room for a surge pod) this setting is the way to roll out, at the price of less capacity.

Next: [Lecture 3: Blue/Green](03-blue-green.md)
