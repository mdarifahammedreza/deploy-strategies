# Lecture 4: Canary
**Duration:** 60 min · **You will need:** `make up` done, dashboard open on the projector

## Learning goals
- Explain how a replica-ratio canary sends a small share of users to the new version.
- Predict the traffic share from the number of stable and canary pods.
- Perform the steps 10% → 30% → 50% → promote with the `make canary-*` targets.
- Recognize a bad canary (v3) on the dashboard and abort in the correct order.
- Explain two limits of this approach: the share depends on pod count, and there is no automatic analysis.

## 0. Before class (instructor checklist)
- [ ] `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] `make reset`
- [ ] Dashboard open at http://localhost:30080
- [ ] Terminal 2: `$KUBECTL get pods -w`
- [ ] Remember: stable has 9 replicas, canary adds 1, so 1 of 10 pods is about 10%. The exact share jumps a little (it is random), so say "about".

## 1. The idea (≈5 min)
> **Say:** "A canary release sends the new version to a small group of users first. If they are fine, we send more. If something is wrong, only a few users are hurt, and we go back."
>
> **Say:** "The name comes from coal mines. Miners took a canary bird into the mine. If the air was bad, the small bird showed it first. Our canary pod is the early warning."

- Analogy: a restaurant gives a free taste of a new dish to one table out of ten before it changes the whole menu.
- Timeline:

```
time ──────────────────────────────────────────────────────────────────────►
stable v1   ■■■■■■■■■  ■■■■■■■  ■■■■■   ■■■■■■■■■ (v2 after promote)
canary v2   ■ (10%)    ■■■ (30%) ■■■■■ (50%)  ──► removed
                ▲          ▲         ▲              ▲
          canary-deploy  canary-30  canary-50   canary-promote
Bad canary: canary-deploy TO=v3 ─► errors on about 10% of pods only ─► canary-abort
```

## 2. Read the YAML together, line by line (≈10 min)
Three files. The new idea compared with Lecture 3: the Service selects **only `app`**, not `track`. Stable and canary pods both match it. Lines that are the same as in Lecture 2 or 3 are only summarised.

### File 1: `k8s/strategies/canary/deployment-stable.yaml`

```yaml
# =====================================================================
# CANARY, part 1: the STABLE track (v1) serves most users.
# Canary traffic share ≈ canary pods / all pods, because the Service
# load-balances evenly over every ready pod it selects.
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages the stable pods
metadata:                               # identifying data
  name: color-backend-stable            # stable track Deployment
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find it
    track: stable                       # which track this is
spec:                                   # desired state
  replicas: 9                           # 9 stable + 1 canary = 10 pods → canary gets about 10%
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # must carry both labels
      app: color-backend                # common app label
      track: stable                     # only stable pods
  template:                             # blueprint of every stable pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # selected by the Service AND watched by the dashboard
        track: stable                   # tells stable and canary pods apart
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name
          image: color-backend:v1       # the current, trusted version
          imagePullPolicy: IfNotPresent # use the image loaded into kind
          ports:                        # ports the container listens on
            - name: http                # named port
              containerPort: 8080       # the Go server's port
          env:                          # environment variables
            - name: STARTUP_DELAY       # "not ready" period after start
              value: "5s"               # visible Starting phase
          readinessProbe:               # "may this pod receive traffic?"
            httpGet:                    # over HTTP...
              path: /readyz             # ...the readiness endpoint
              port: http                # ...on the named port
            periodSeconds: 1            # every second
            failureThreshold: 1         # one failure → not ready
          lifecycle:                    # container hooks
            preStop:                    # before SIGTERM...
              sleep:                    # ...just wait...
                seconds: 3              # ...3 s so routing updates first
          resources:                    # reservations and limits
            requests:                   # reserved by the scheduler
              cpu: 10m                  # 1% of a CPU core
              memory: 16Mi              # 16 MiB RAM
            limits:                     # hard ceiling
              memory: 64Mi              # killed above 64 MiB
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-5 | Comment header | The key rule: canary share is about canary pods divided by all pods. |
| 9 | `name: color-backend-stable` | The track that serves most users. |
| 11-13 | `labels app, track: stable` | `app` for `make reset`; `track` to tell stable and canary apart. |
| 15 | `replicas: 9` | 9 stable + 1 canary = 10 pods. The canary gets 1 of 10 requests (about 10%). |
| 16-19 | `selector.matchLabels app + track: stable` | Each Deployment owns only its own pods. |
| 20-24 | `template.metadata.labels app + track: stable` | `app` is what the Service and dashboard select. `track` is for us. |
| 26 | `terminationGracePeriodSeconds: 30` | Time to stop after SIGTERM. |
| 28-29 | `name: app`, `image: color-backend:v1` | The trusted version. `make canary-promote` changes this tag. |
| 34-36 | `env STARTUP_DELAY=5s` | Visible Starting phase. |
| 37-42 | `readinessProbe /readyz` | The Service only uses pods that pass this. |
| 43-46 | `lifecycle.preStop sleep 3` | Protects requests when a pod is removed (scale down). |
| 47-52 | `resources` | Small reservations. |

### File 2: `k8s/strategies/canary/deployment-canary.yaml`

```yaml
# =====================================================================
# CANARY, part 2: a FEW pods of the NEW version (v2) mixed into the
# same Service. Watch its errors; then grow it (promote) or delete it
# (abort). Like the canary bird in a coal mine: it warns you early.
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages the canary pods
metadata:                               # identifying data
  name: color-backend-canary            # canary track Deployment
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find it
    track: canary                       # which track this is
spec:                                   # desired state
  replicas: 1                           # start small: 1 of 10 pods ≈ 10% of requests
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # must carry both labels
      app: color-backend                # common app label
      track: canary                     # only canary pods
  template:                             # blueprint of every canary pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # SAME app label → the same Service sends it traffic
        track: canary                   # tells canary pods apart
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name (used by `kubectl set image ... app=`)
          image: color-backend:v2       # the candidate version (v3 = a bad candidate)
          imagePullPolicy: IfNotPresent # use the image loaded into kind
          ports:                        # ports the container listens on
            - name: http                # named port
              containerPort: 8080       # the Go server's port
          env:                          # environment variables
            - name: STARTUP_DELAY       # "not ready" period after start
              value: "5s"               # visible Starting phase
          readinessProbe:               # "may this pod receive traffic?"
            httpGet:                    # over HTTP...
              path: /readyz             # ...the readiness endpoint
              port: http                # ...on the named port
            periodSeconds: 1            # every second
            failureThreshold: 1         # one failure → not ready
          lifecycle:                    # container hooks
            preStop:                    # before SIGTERM...
              sleep:                    # ...just wait...
                seconds: 3              # ...3 s so routing updates first
          resources:                    # reservations and limits
            requests:                   # reserved by the scheduler
              cpu: 10m                  # 1% of a CPU core
              memory: 16Mi              # 16 MiB RAM
            limits:                     # hard ceiling
              memory: 64Mi              # killed above 64 MiB
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-5 | Comment header | The canary bird warns early. |
| 9 | `name: color-backend-canary` | A separate Deployment, so we can scale and delete it alone. |
| 13 | `track: canary` | Label on the object. |
| 15 | `replicas: 1` | Start small: 1 of 10 pods. |
| 19 | `track: canary` in the selector | Only canary pods. |
| 23 | `app: color-backend` | The **same** `app` label as stable, so the **same Service** sends it traffic. |
| 24 | `track: canary` in the pod labels | Lets you list canary pods: `$KUBECTL get pods -l track=canary`. |
| 29 | `image: color-backend:v2` | The candidate. `make canary-deploy TO=v3` changes it with `set image` right after `apply`. |

### File 3: `k8s/strategies/canary/service.yaml`

```yaml
# =====================================================================
# Canary Service: selects ONLY `app`, NOT `track`, so stable AND canary
# pods both receive traffic. More canary pods = bigger canary share.
# =====================================================================
apiVersion: v1                 # Services are core v1 objects
kind: Service                  # stable address in front of pods
metadata:                      # identifying data
  name: color-backend          # the name users (and the dashboard prober) call
  namespace: color-demo        # where it lives
  labels:                      # labels on the Service object
    app: color-backend         # lets `make reset` delete it
spec:                          # desired state
  type: ClusterIP              # internal virtual IP
  selector:                    # which pods get the traffic
    app: color-backend         # stable + canary pods alike (no `track` here on purpose!)
  ports:                       # port mapping list
    - name: http               # port name
      port: 80                 # Service port
      targetPort: http         # container port named "http" (8080)
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-4 | Comment header | Explains why `track` is missing. |
| 8 | `name: color-backend` | The name the dashboard calls. |
| 10-11 | `labels app: color-backend` | For `make reset`. |
| 14-15 | `selector: app: color-backend` | **No `track` here on purpose.** Both Deployments match. Kubernetes spreads connections evenly over all Ready pods, so more canary pods mean a bigger canary share. |
| 16-19 | `ports 80 → http` | Same as before. |

## 3. Live demo (≈15 min)
> **Predict first (before `make canary-start`):** "Stable has 9 replicas. How many boxes will we see?" (9 royalblue boxes.)

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Start stable | `make canary-start`<br>raw: `$KUBECTL apply -f k8s/strategies/canary/deployment-stable.yaml -f k8s/strategies/canary/service.yaml`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=180s` | Nine royalblue boxes; bar 100% v1. | **Say:** "Stable only. No canary yet." |
| 2. Add the canary (10%) | `make canary-deploy TO=v2`<br>raw: `$KUBECTL apply -f k8s/strategies/canary/deployment-canary.yaml`<br>`$KUBECTL set image deploy/color-backend-canary app=color-backend:v2`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=1`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s` | A green panel with **one** box: dashed, striped, then solid. The bar shows a small green piece, around 10% (it can move between 5% and 15%). Panel header: `v2 · 1/1 ready · ~10% of traffic`. | **Predict first:** "What share will green get?" (About 1 of 10.) **Say:** "Only one in ten users tries v2." |
| 3. Increase to 30% | `make canary-30`<br>raw: `$KUBECTL scale deploy/color-backend-canary --replicas=3`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=7` | Two more green boxes start. When all three are Ready, two blue boxes fade away. Bar: about 30% green. | **Say:** "Note the order: add canary pods first, wait until they are Ready, then remove stable pods. Total capacity never drops." |
| 4. Increase to 50% | `make canary-50`<br>raw: `$KUBECTL scale deploy/color-backend-canary --replicas=5`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=5` | Five green, five blue. Bar: about half and half. | **Ask:** "We have 10 pods and 5 are green. What is the share?" (About 50%.) |
| 5. Promote | `make canary-promote TO=v2`<br>raw: `$KUBECTL set image deploy/color-backend-stable app=color-backend:v2`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=300s`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=0` | The stable Deployment is updated with a rolling update. New green stable boxes replace blue ones. At the end the canary boxes fade and disappear. Only green boxes remain, bar 100% v2. | **Say:** "Promote means: the stable track now runs v2. Then the canary is not needed, so it goes to zero." |
| 6. Clean up | `make reset` | Page is empty. | Prepare the next demo. |

## 4. Break it on purpose (≈10 min)
### 4.1 A bad canary (v3)
> **Predict first:** "v3 returns HTTP 500 for 30% of its requests, and it passes readiness. If the canary has 10% of the traffic, how many percent of **all** requests fail?" (About 3%: 30% of 10%.)

| Step | Run | What the dashboard shows | Say |
|---|---|---|---|
| 1 | `make canary-start` | Nine blue boxes. | |
| 2 | `make canary-deploy TO=v3` | One darkorange box becomes Ready. The bar has an orange piece of about 10%. The canary box gets a **red ring** and a badge such as `3 errors`. The legend shows errors in red. The blue boxes are clean. | **Say:** "Only about 3 in 100 requests fail, and only users who hit the canary pod. With Recreate or Rolling it would be 30% of 100%." |
| 3 | `make canary-abort`<br>raw: `$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=180s`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=0` | The orange box fades and disappears. Nine blue boxes remain. Errors stop. | **Say:** "Look at the order of the abort. We restore stable capacity **first**, then remove the canary. If we removed the canary first, we might run on less capacity than we need." |

### 4.2 Wrong abort order (what NOT to do)
Start from 50% (`make canary-start`, `make canary-deploy TO=v3`, `make canary-50`). Stable now has 5 pods.
- **Wrong:** `$KUBECTL scale deploy/color-backend-canary --replicas=0` first. Result: the bad pods are gone, but only **5 pods** serve all users, and for a moment capacity is cut in half. Then you scale stable up and wait for 5 s start-up.
- **Right:** `make canary-abort`. First stable goes back to 9 (new blue boxes start and become Ready, while orange boxes still serve), then the canary goes to 0.

### 4.3 Limits of the replica-ratio canary
| Try this | What you see | Lesson |
|---|---|---|
| Ask for 1% | With 10 pods, the smallest step is 1 of 10 = 10%. For 1% you would need 100 pods. | The share depends on the **pod count**. |
| Watch the bar for one minute at 10% | The green share moves between about 5% and 15%. | The balancing is random, not exact. |
| Look for an "automatic stop" | There is none. You, the human, must watch the dashboard and press the abort. | There is no automatic analysis. |

## 5. Discussion questions (with answers in a collapsed <details> block)
1. What is the canary share with 9 stable and 3 canary pods?
2. Why did `canary-30` wait for the canary to be Ready before it scaled stable down?
3. What are two disadvantages of controlling the share with replica counts?
4. A 3% error rate: would you notice it on a dashboard in real life? What would you need?
5. Which strategy would you use for a payment service? Why?

<details>
<summary>Answers</summary>

1. 3 of 12 pods = 25%.
2. So that the number of Ready pods never drops. If stable was scaled down first, there would be less capacity during the 5 s start-up of the new pods.
3. (a) The smallest step is 1 pod: 1% needs 100 pods. (b) Changing the share means starting or stopping pods, which is slow (5 s per start-up). (c) The share is only approximate. (d) Nobody watches for you.
4. You would need metrics per version (error rate, latency) and alerts. In this class the dashboard does this for us. In production use Prometheus and similar tools.
5. A canary (or Blue/Green with a good test), because a bad release must hurt as few users as possible and must be easy to stop. Recreate is wrong because of downtime.
</details>

## 6. Recap: pros / cons / when to use
| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| Small blast radius: only a few users see a bad version | Share is limited by pod count (no 1% with 10 pods) | You are not sure the new version is good |
| Real production traffic tests the new version | No automatic analysis: a human must watch and decide | You have metrics (error rate, latency) per version |
| Easy to abort (scale down) | Two versions run together (like Rolling) | The change is risky or touches many users |
| No special tools: only Deployments and one Service | Slow steps (new pods need 5 s to start) | You want a step-by-step release with checks |

## 7. Lab / homework
1. **Easy.** Run `make canary-start`, `make canary-deploy TO=v2`, `make canary-30`, `make canary-50`. At each step write down the number of v1 and v2 Ready pods and the share shown on the traffic bar.
   *Expected:* 9+1 → about 10%; 7+3 → about 30%; 5+5 → about 50%. The bar share is close to the pod ratio but not exact.
2. **Medium.** Do a **good** canary all the way: `make canary-start`, `make canary-deploy TO=v2`, `make canary-30`, `make canary-50`, `make canary-promote TO=v2`. Then run `$KUBECTL get deploy`. Which Deployment has pods? What is the image of the stable Deployment?
   *Expected:* `color-backend-stable` has 9 pods and now runs `color-backend:v2`. `color-backend-canary` has 0 replicas (it still exists).
3. **Harder.** Run a bad canary with v3 up to 30% (`make canary-start`, `make canary-deploy TO=v3`, `make canary-30`). Predict the overall error rate, compare it with the dashboard legend, then abort correctly. Then explain in two sentences how you would automate the decision "abort when the canary error rate is above 5%".
   *Expected:* about 9% of all requests fail (30% error rate × 30% share); the legend shows v3 with an error count in red. After `make canary-abort`, only blue boxes remain. Automation idea: a tool reads the error rate per version from metrics and scales the canary to 0 when the limit is crossed. This is what Argo Rollouts does (Lecture 5).

Next: [Lecture 5: Next steps with kubeadm and ALB](05-next-kubeadm-alb.md)
