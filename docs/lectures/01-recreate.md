# Lecture 1: Recreate
**Duration:** 45 min · **You will need:** `make up` done, dashboard open on the projector

## Learning goals
- Explain how the Recreate strategy replaces pods.
- Predict the downtime window of a Recreate upgrade from the readiness settings.
- Read every line of a Deployment and a Service manifest.
- Perform a Recreate upgrade and see "unreachable" on the traffic bar.
- Decide when downtime is acceptable.

## 0. Before class (instructor checklist)
- [ ] `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] Clean the demo app: `make reset`
- [ ] Dashboard open at http://localhost:30080, empty page (no boxes)
- [ ] Second terminal ready: `$KUBECTL get pods -w`
- [ ] Optional: zoom the browser so the traffic bar and event log are readable from the back row

## 1. The idea (≈5 min)
> **Say:** "Recreate is the simplest strategy. Step one: stop every old pod. Step two: start every new pod. There is never a moment when two versions run together."
>
> **Say:** "The price is downtime. Between step one and step two nobody is there to answer. Today we measure that gap."

- Analogy: a shop closes, the staff change all the shelves, and then the shop opens again. Customers who come in between find a closed door.
- Timeline:

```
time ──────────────────────────────────────────────────────────────►

pods     v1 v1 v1 v1 v1 v1 │ (no pods) │ v2 v2 v2 v2 v2 v2
                           │           │ starting... ready
users    served by v1      │ ERRORS    │ served by v2
                           ▲           ▲
                  old pods deleted   new pods Ready (about 5 s later)
```

## 2. Read the YAML together, line by line (≈10 min)
This is the first lecture that shows a Deployment, so the table covers **every line**. Line numbers are for the file as shown below.

### File 1: `k8s/strategies/recreate/deployment.yaml`

```yaml
# =====================================================================
# RECREATE: kill ALL old pods first, THEN start the new ones.
# Simple, never runs two versions at once, but users see DOWNTIME.
# Upgrade:  kubectl -n color-demo set image deploy/color-backend app=color-backend:v2
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages ReplicaSets, which manage pods
metadata:                               # identifying data
  name: color-backend                   # same name as in the rolling example: only the strategy differs
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find and delete it
  annotations:                          # notes attached to the object
    kubernetes.io/change-cause: "initial deploy: v1"  # shown by `kubectl rollout history`
spec:                                   # desired state
  replicas: 6                           # keep 6 pods
  revisionHistoryLimit: 5               # remember 5 old versions for `rollout undo`
  strategy:                             # HOW pods are replaced
    type: Recreate                      # delete every old pod, wait until they are gone, then create new ones
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # pods must carry these labels
      app: color-backend                # the label from the template below
  template:                             # blueprint of every pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # matched by the selector and by the Service
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name (used by `kubectl set image`)
          image: color-backend:v1       # the version: change this tag to upgrade
          imagePullPolicy: IfNotPresent # use the image loaded into kind
          ports:                        # ports the container listens on
            - name: http                # name referenced by probes and the Service
              containerPort: 8080       # the Go server's port
          env:                          # environment variables for the app
            - name: STARTUP_DELAY       # "not ready" period after start
              value: "5s"               # makes the downtime gap clearly visible
          readinessProbe:               # "may this pod receive traffic?"
            httpGet:                    # check over HTTP...
              path: /readyz             # ...the readiness endpoint
              port: http                # ...on the named port
            periodSeconds: 1            # every second
            failureThreshold: 1         # one failure → not ready
          livenessProbe:                # "is the process alive?"
            httpGet:                    # check over HTTP...
              path: /healthz            # ...the always-OK endpoint
              port: http                # ...on the named port
            periodSeconds: 10           # every 10 seconds
          resources:                    # resource reservations and limits
            requests:                   # reserved by the scheduler
              cpu: 10m                  # 1% of a CPU core
              memory: 16Mi              # 16 MiB RAM
            limits:                     # hard ceiling
              memory: 64Mi              # killed above 64 MiB
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-5 | Comment header with the upgrade command | Shows the whole upgrade is one command: change the image. |
| 6 | `apiVersion: apps/v1` | Deployments belong to the `apps` API group. |
| 7 | `kind: Deployment` | A Deployment manages ReplicaSets, and ReplicaSets manage pods. |
| 9 | `name: color-backend` | The name that `make upgrade` uses (`deploy/color-backend`). Rolling uses the same name; only the strategy changes. |
| 10 | `namespace: color-demo` | Where the object lives. |
| 11-12 | `labels: app: color-backend` | `make reset` deletes everything with this label. |
| 13-14 | `annotations: kubernetes.io/change-cause` | The text shown by `make history`. |
| 16 | `replicas: 6` | Six pods = six boxes. |
| 17 | `revisionHistoryLimit: 5` | Keep 5 old ReplicaSets so `make undo` works. |
| 18-19 | `strategy: type: Recreate` | **The key line.** Delete all old pods, wait until they are gone, then create new ones. |
| 20-22 | `selector.matchLabels app: color-backend` | How the Deployment finds its pods. Must match the template labels. |
| 23-26 | `template.metadata.labels app: color-backend` | Every pod gets this label. The Service and the dashboard select on it. |
| 28 | `terminationGracePeriodSeconds: 30` | After SIGTERM the pod has 30 s to stop. The backend stops fast, so you will not wait 30 s. |
| 30 | `name: app` | Container name. `make upgrade` uses it: `set image ... app=color-backend:v2`. |
| 31 | `image: color-backend:v1` | The version. Changing this tag changes the pod template, and that starts a rollout. |
| 32 | `imagePullPolicy: IfNotPresent` | Use the image loaded by `make load`. Do not pull from the internet. |
| 33-35 | `ports: http 8080` | Named port that probes and the Service refer to. |
| 36-38 | `env STARTUP_DELAY=5s` | The app answers "not ready" for 5 s. This **is** the length of the downtime. |
| 39-44 | `readinessProbe /readyz, period 1, failureThreshold 1` | Check every second. A pod is only in the Service when this passes. |
| 45-49 | `livenessProbe /healthz, period 10` | Restart the container if the process hangs. Not important for this lesson. |
| 50-55 | `resources requests/limits` | Small reservations so six pods fit on a laptop. |

Notice what is **missing**: no `minReadySeconds`, no `progressDeadlineSeconds` and no `preStop` hook. Recreate does not need them. They appear in Lecture 2.

### File 2: `k8s/strategies/recreate/service.yaml`

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

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 6-7 | `apiVersion: v1`, `kind: Service` | A Service is a stable address in front of changing pods. |
| 9 | `name: color-backend` | The dashboard's traffic probe calls `http://color-backend/api/color`. The name must never change. |
| 10-12 | `namespace`, `labels app: color-backend` | `make reset` deletes the Service too. |
| 14 | `type: ClusterIP` | Internal only. The dashboard is inside the cluster. |
| 15-16 | `selector: app: color-backend` | Traffic goes to **Ready** pods with this label. When no pod is Ready, the Service has no endpoints and requests fail. That is the downtime. |
| 17-20 | `ports: 80 → targetPort http` | Clients use port 80; the Service forwards to container port 8080. |

## 3. Live demo (≈15 min)
> **Predict first (before `make recreate-start`):** "How many boxes will we see? What color? Will they all become solid at the same moment?"

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Start v1 | `make recreate-start`<br>raw: `$KUBECTL apply -f k8s/strategies/recreate/`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | Six royalblue boxes: dashed, then striped, then solid. Traffic bar fills to 100% v1. | **Say:** "Normal start. Look at the panel header: `v1 · 6/6 ready · 100% of traffic`." |
| 2. Check history | `make history`<br>raw: `$KUBECTL rollout history deploy/color-backend` | No change. | Show revision 1 with the text `initial deploy: v1`. |
| 3. Predict | (do not press Enter yet) | | **Ask:** "We will upgrade to v2. (a) Will we ever see blue and green boxes together? (b) Will the traffic bar stay full? (c) How many seconds of downtime do you expect?" Collect votes. |
| 4. Upgrade to v2 | `make upgrade TO=v2`<br>raw: `$KUBECTL set image deploy/color-backend app=color-backend:v2`<br>`$KUBECTL annotate deploy/color-backend kubernetes.io/change-cause="upgrade to v2" --overwrite`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | All six blue boxes fade (Terminating) and disappear. The screen is empty for a moment. Six green dashed boxes appear, then stripes. The traffic bar turns to the **unreachable** color for several seconds. Then the boxes become solid and the bar turns green. | **Say:** "Now! Look at the traffic bar. This is what your users see." **Ask:** "Did blue and green boxes ever stand together?" (No.) |
| 5. Read the log | (look at the event log) | Lines like `... (v1) removed`, `+ ... (v2) created`, `Starting → Ready`, `traffic: v2 now serves 100%`. | Count the seconds between the last `removed` and the first `Ready`. This is the gap (about 5 s plus start-up time). |
| 6. Compare the bar | (wait 5 s) | The bar finishes at 100% v2. | **Say:** "The bar looks at the last 100 requests, so the red part stays for about 5 more seconds after the pods are back." |
| 7. Undo | `make undo`<br>raw: `$KUBECTL rollout undo deploy/color-backend`<br>`$KUBECTL rollout status deploy/color-backend --timeout=180s` | The same downtime again, now back to blue. | **Say:** "Rollback with Recreate has downtime too. There is no free undo." |

## 4. Break it on purpose (≈10 min)
| Scenario | Commands | What the dashboard shows |
|---|---|---|
| **Recreate to a version that never becomes ready** (v4) | `make recreate-start`, then `make upgrade TO=v4` (it will time out after 3 minutes; press Ctrl+C when students understand) | All six blue pods go away. Six purple striped boxes appear and **never** become solid. The traffic bar stays at **unreachable**: a complete outage that does not end. |
| Fix it | `make undo` | Purple boxes disappear, blue boxes start and become solid after about 5 s. Service is back. |
| **Recreate to a half-broken version** (v3) | `make upgrade TO=v3` | Orange boxes become Ready (they pass readiness) but about 30% of requests return HTTP 500. Boxes get red rings and badges like `12 errors`. Recover with `make undo`. |
| Delete one pod only | `$KUBECTL delete pod <name>` | Not downtime: the other five pods keep serving. Recreate only stops **all** pods when the template changes. |

> **Say:** "With Recreate, a bad version means a full outage, and we cannot see it coming. The old pods are already gone."

## 5. Discussion questions (with answers in a collapsed <details> block)
1. Which line in the YAML decides the length of the downtime? Can you make the gap shorter?
2. Give two real cases where a Recreate downtime is acceptable.
3. Why can Recreate be the **only** correct choice for some apps?
4. Why did the unreachable part of the traffic bar last longer than the empty screen?

<details>
<summary>Answers</summary>

1. The time is: shutdown of old pods + start-up time of new pods. In our demo `STARTUP_DELAY: "5s"` plus the readiness check (every 1 s) is the biggest part. Real apps: make the app start faster, or use another strategy.
2. A nightly batch job or an internal admin tool used only in office hours. A system with a maintenance window, for example a bank that is closed at night.
3. When two versions must never run at the same time: a database schema change that breaks the old version, a singleton that holds a lock (one writer only), or a license that allows only one instance.
4. The bar counts the last 100 requests (about 5 s). Failed requests stay in that window for a while, even after the pods are back.
</details>

## 6. Recap: pros / cons / when to use
| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| Very simple to understand and to write | **Downtime** while no pod is ready | The app can be offline for a short time |
| Only one version runs at a time: no compatibility problems | Bad version = full outage until you roll back (rollback also has downtime) | The app is a singleton or uses an incompatible database schema |
| No extra capacity needed | Users notice the outage | Dev and test environments |
| Frees resources before starting new pods | No chance to test the new version with real traffic first | Batch jobs and internal tools |

## 7. Lab / homework
1. **Easy.** Run `make recreate-start` and `make upgrade TO=v2`. Write down how many seconds the traffic bar showed "unreachable". Repeat once. Are the two numbers the same?
   *Expected:* about 5 to 10 seconds, with small differences because of scheduling and the 5 s window.
2. **Medium.** Change `STARTUP_DELAY` from `"5s"` to `"15s"` in a copy of `deployment.yaml` (do not edit the original; use `$KUBECTL set env deploy/color-backend STARTUP_DELAY=15s` after `make recreate-start`, then `make upgrade TO=v2`). Predict the downtime first.
   *Expected:* the downtime grows by about 10 seconds. (Note: `set env` itself changes the pod template and triggers a Recreate, so wait for it to finish before upgrading.)
3. **Harder.** Run `make recreate-start`, then `make upgrade TO=v4`. While the outage is running, fix it without `make undo`, using only `$KUBECTL rollout history deploy/color-backend` and `$KUBECTL rollout undo deploy/color-backend --to-revision=<number>`. Which revision number did you pick and why?
   *Expected:* the revision that has the `v1` image (find it with `--revision=N` on `rollout history`). The dashboard returns to blue boxes.

Next: [Lecture 2: Rolling Update](02-rolling-update.md)
