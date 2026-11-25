# Lecture 3: Blue/Green
**Duration:** 50 min · **You will need:** `make up` done, dashboard open on the projector

## Learning goals
- Explain how Blue/Green uses two full Deployments and one Service selector.
- Predict what the dashboard shows when the green slot is ready but not live.
- Perform the switch and the rollback with `make bg-switch` and `make bg-rollback`.
- Test the new version before users see it, using the preview Service.
- Explain the cost of Blue/Green (double capacity) and its risks (database, sessions).

## 0. Before class (instructor checklist)
- [ ] `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] `make reset`
- [ ] Dashboard open at http://localhost:30080
- [ ] Terminal 2: `$KUBECTL get pods -w`
- [ ] Make sure internet works for the `curlimages/curl` image used in the preview demo (or run `docker pull curlimages/curl` and `kind load docker-image --name deploy-demo curlimages/curl` before class)

## 1. The idea (≈5 min)
> **Say:** "Blue/Green keeps two complete copies of the app. Blue is live and serves the users. Green is the new version, fully started, but nobody uses it yet. We test green. When we are happy, we flip one switch and all users move to green at once."
>
> **Say:** "In Kubernetes the switch is one line: the `slot` selector of the Service. If green is bad, we flip the switch back."

- Analogy: a railway track switch. Two tracks are ready. Moving the lever sends the whole train to the other track in one moment.
- Timeline:

```
time ─────────────────────────────────────────────────────────────────►
blue  (v1)  ████████████████████████████████░░░░░░░░░░░░░░░░ (retire: scale to 0)
green (v2)  ░░░░░░░░ starting ░░ ready, idle ██████████████████████████
                                              ▲
Service selector:  slot=blue  ───────────────┴─► slot=green
                              (make bg-switch)
users       100% v1                              100% v2
rollback:   slot=green ──► slot=blue is the same one-line change
```

## 2. Read the YAML together, line by line (≈10 min)
Four files. Compared with Lecture 2, we have **no** `strategy` block: each Deployment is simple, and the **Service** does the work. The lines that differ from the previous lecture are listed.

### File 1: `k8s/strategies/blue-green/deployment-blue.yaml`

```yaml
# =====================================================================
# BLUE/GREEN, part 1: "blue" = the version users get TODAY (v1).
# Blue and green are two complete, separate Deployments. The Service
# decides which one is live by its `slot` label selector.
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages the blue pods
metadata:                               # identifying data
  name: color-backend-blue              # one Deployment per color
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find it
    slot: blue                          # which slot this Deployment fills
spec:                                   # desired state
  replicas: 4                           # full production capacity
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # must carry BOTH labels
      app: color-backend                # common app label
      slot: blue                        # only blue pods → blue and green never adopt each other's pods
  template:                             # blueprint of every blue pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # the dashboard watches this label
        slot: blue                      # the Service selects on this label
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name
          image: color-backend:v1       # blue runs the current version
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
| 1-5 | Comment header | Blue = the version users get today. |
| 9 | `name: color-backend-blue` | One Deployment per color. The name is **not** `color-backend`, so blue and green can live together. |
| 11-13 | `labels app, slot: blue` | `app` so `make reset` finds it; `slot` says which slot it fills. |
| 15 | `replicas: 4` | Full capacity. Green also gets 4. |
| 16-19 | `selector.matchLabels app + slot: blue` | The selector has **two** labels, so blue and green Deployments never adopt each other's pods. |
| 20-24 | `template.metadata.labels app + slot: blue` | The pod labels. The Service selects on `slot`. |
| 29 | `image: color-backend:v1` | Blue runs the current version. |
| rest | Same as Lecture 2: `STARTUP_DELAY 5s`, readiness `/readyz`, `preStop` 3 s, resources | No `strategy:` block, so the default RollingUpdate would apply if you changed the image. We never do that here. |

### File 2: `k8s/strategies/blue-green/deployment-green.yaml`

```yaml
# =====================================================================
# BLUE/GREEN, part 2: "green" = the NEW version (v2), started NEXT TO
# blue. It gets no user traffic until the Service selector is switched.
# =====================================================================
apiVersion: apps/v1                     # Deployments live in the "apps" API group
kind: Deployment                        # manages the green pods
metadata:                               # identifying data
  name: color-backend-green             # the second, independent Deployment
  namespace: color-demo                 # where it lives
  labels:                               # labels on the Deployment object
    app: color-backend                  # lets `make reset` find it
    slot: green                         # which slot this Deployment fills
spec:                                   # desired state
  replicas: 4                           # SAME capacity as blue → instant switch, no scaling needed
  selector:                             # which pods belong to this Deployment
    matchLabels:                        # must carry both labels
      app: color-backend                # common app label
      slot: green                       # only green pods
  template:                             # blueprint of every green pod
    metadata:                           # pod metadata
      labels:                           # pod labels
        app: color-backend              # the dashboard watches this label
        slot: green                     # the Service selects on this label after the switch
    spec:                               # pod specification
      terminationGracePeriodSeconds: 30 # after SIGTERM, wait up to 30 s before SIGKILL
      containers:                       # containers in the pod
        - name: app                     # container name
          image: color-backend:v2       # green runs the new version
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
| 1-4 | Comment header | Green = the new version, started next to blue. |
| 8 | `name: color-backend-green` | A second, independent Deployment. |
| 12 | `slot: green` | Label on the object. |
| 14 | `replicas: 4` | **Same** capacity as blue, so the switch needs no scaling and is instant. |
| 18 | `slot: green` in the selector | Only green pods. |
| 23 | `slot: green` in the pod labels | After the switch the Service selects this label. |
| 28 | `image: color-backend:v2` | The new version. |

### File 3: `k8s/strategies/blue-green/service.yaml` (the LIVE Service)

```yaml
# =====================================================================
# The LIVE Service. Its selector points at ONE slot. Switching from blue
# to green is a single change of `slot`, done atomically:
#   kubectl -n color-demo patch service color-backend \
#     -p '{"spec":{"selector":{"app":"color-backend","slot":"green"}}}'
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
    app: color-backend         # backend pods...
    slot: blue                 # ...of the BLUE slot only. This one line IS the switch.
  ports:                       # port mapping list
    - name: http               # port name
      port: 80                 # Service port
      targetPort: http         # container port named "http" (8080)
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-6 | Comment header with the `patch` command | The switch is one `kubectl patch`. |
| 10 | `name: color-backend` | The name the dashboard's traffic probe calls. It never changes. |
| 16-18 | `selector: app: color-backend` and `slot: blue` | **This one line IS the switch.** Today it points to blue only. Green pods have `slot: green`, so they get no traffic. |
| 19-22 | `ports 80 → http` | Same as before. |

### File 4: `k8s/strategies/blue-green/service-preview.yaml`

```yaml
# =====================================================================
# PREVIEW Service: always points at GREEN, so the team can test the new
# version inside the cluster BEFORE switching real users to it:
#   kubectl -n color-demo run tmp --rm -it --image=curlimages/curl --restart=Never \
#     -- curl -s http://color-backend-preview/api/color
# =====================================================================
apiVersion: v1                 # Services are core v1 objects
kind: Service                  # stable address in front of pods
metadata:                      # identifying data
  name: color-backend-preview  # a second, test-only name
  namespace: color-demo        # where it lives
  labels:                      # labels on the Service object
    app: color-backend         # lets `make reset` delete it
spec:                          # desired state
  type: ClusterIP              # internal only
  selector:                    # which pods get the traffic
    app: color-backend         # backend pods...
    slot: green                # ...of the GREEN slot (the candidate)
  ports:                       # port mapping list
    - name: http               # port name
      port: 80                 # Service port
      targetPort: http         # container port named "http" (8080)
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-5 | Comment header with a `curl` pod command | The preview is for testing inside the cluster. |
| 10 | `name: color-backend-preview` | A second name. The dashboard does not call it, so the traffic bar does not count test requests. |
| 16-18 | `selector: app + slot: green` | Always green. Your team can test green while users are still on blue. |

## 3. Live demo (≈15 min)
> **Predict first (before `make bg-deploy-green`):** "We start green with 4 pods. Will users see v2? Will the traffic bar change?" (No. Green is not selected by the live Service.)

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Start blue | `make bg-start`<br>raw: `$KUBECTL apply -f k8s/strategies/blue-green/deployment-blue.yaml -f k8s/strategies/blue-green/service.yaml`<br>`$KUBECTL rollout status deploy/color-backend-blue --timeout=180s` | Four royalblue boxes (dashed, striped, solid). Bar 100% v1. | **Say:** "Blue is live. Four pods." |
| 2. Start green | `make bg-deploy-green`<br>raw: `$KUBECTL apply -f k8s/strategies/blue-green/deployment-green.yaml -f k8s/strategies/blue-green/service-preview.yaml`<br>`$KUBECTL rollout status deploy/color-backend-green --timeout=180s` | A new green panel appears: four mediumseagreen boxes become Ready. After a few seconds they are **dimmed with the badge "no traffic"**. The traffic bar is still 100% blue. The panel header says `v2 · 4/4 ready · 0% of traffic`. | **Say:** "Green is fully ready but idle. We pay for 8 pods now." **Ask:** "Why are the green boxes dimmed?" (They are Ready, but no request reached them in the last window.) |
| 3. Test the preview | `$KUBECTL run tmp --rm -it --image=curlimages/curl --restart=Never -- curl -s http://color-backend-preview/api/color` | (terminal) JSON with `"version":"v2"` and `"color":"mediumseagreen"`. The dashboard does **not** change. | **Say:** "We tested v2 with a real request, and no user was involved." |
| 4. Predict | (do not press Enter yet) | | **Ask:** "When we switch, how long until the bar is 100% green? Will we see blue and green mixed on the bar?" (The switch is instant. The bar needs about 5 s to fill because it counts the last 100 requests, so it shows a short mix.) |
| 5. Switch | `make bg-switch`<br>raw: `$KUBECTL patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"green"}}}'` | The green boxes lose the "no traffic" dimming and become solid. The blue boxes become dimmed "no traffic". The traffic bar moves from blue to 100% green within about 5 seconds. No red, no unreachable. | **Say:** "One line changed. No pod started or stopped. That is why it is instant." |
| 6. Rollback | `make bg-rollback`<br>raw: `$KUBECTL patch service color-backend -p '{"spec":{"selector":{"app":"color-backend","slot":"blue"}}}'` | The reverse picture: blue takes all traffic again. | **Say:** "Rollback takes seconds, not minutes. Blue was never deleted." |
| 7. Switch again | `make bg-switch` | Green serves everything. | **Say:** "We are happy with green now." |
| 8. Retire blue | `make bg-retire-blue`<br>raw: `$KUBECTL scale deploy/color-backend-blue --replicas=0` | Blue boxes fade and disappear. Only four green boxes remain. | **Say:** "We pay for 4 pods again. But now rollback is slower: we must scale blue up first. So wait before you retire." |

## 4. Break it on purpose (≈10 min)
| Scenario | Commands | What the dashboard shows |
|---|---|---|
| **Switch to a bad green** | Edit green to a bad image: `$KUBECTL set image deploy/color-backend-green app=color-backend:v3`, wait for the pods to roll (they become Ready), then `make bg-switch` | Orange (v3) boxes take all traffic and show red rings; about 30% of requests fail. This shows that preview tests must really check the answers. Fix: `make bg-rollback` (errors stop within seconds). |
| **Switch to an empty slot** | Run `make bg-switch` before `make bg-deploy-green` (or run `$KUBECTL scale deploy/color-backend-green --replicas=0` and then `make bg-switch`) | The Service selects `slot: green`, but no green pod is Ready. The Service has **no endpoints**, so the traffic bar goes **unreachable**. Fix: `make bg-rollback`. **Say:** "The switch does not check anything. Always confirm that green is Ready and tested before the switch." |
| **Switch back after retiring blue** | After `make bg-retire-blue`, run `make bg-rollback` | Unreachable, because blue has 0 pods. Fix: `$KUBECTL scale deploy/color-backend-blue --replicas=4`, wait for Ready, then `make bg-rollback`. |

Reset green between scenarios with `$KUBECTL set image deploy/color-backend-green app=color-backend:v2` and `$KUBECTL scale deploy/color-backend-green --replicas=4`.

## 5. Discussion questions (with answers in a collapsed <details> block)
1. What is the cost of Blue/Green? How is it different from a rolling update?
2. Both slots use the same database. What can go wrong at the switch?
3. A user is in the middle of a shopping cart when we switch. What happens to their session?
4. Why is the switch instant but the traffic bar slow?
5. Why does the preview Service use another name?

<details>
<summary>Answers</summary>

1. You run **double the pods** during the release (8 instead of 4). A rolling update needs only one extra pod. In return, you get an instant switch and an instant rollback.
2. The database must work with **both** versions, because rollback means v1 talks to the data that v2 has written. Use backward-compatible migrations: add columns first, remove old ones in a later release.
3. If the session lives in the pod's memory, it is lost and the user must log in again. Put sessions in a shared store (Redis, a database) or use signed tokens.
4. The switch changes the Service endpoints at once. The bar looks at the last 100 requests (about 5 s), so it needs that time to show the new picture.
5. So that test requests do not mix with the user traffic that the dashboard measures, and so that the preview always points to green, even after the live Service moves.
</details>

## 6. Recap: pros / cons / when to use
| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| Instant switch, no mixed versions for users | Double capacity (cost) while both run | You need a fast, clean cut-over |
| Instant rollback while blue still exists | Database and session compatibility must be planned | The new version must be tested in production-like conditions first |
| New version is tested in production environment before users arrive | All users move at once: a hidden bug hits 100% | Releases with a clear "go / no-go" decision |
| No mixed-version traffic | More objects to manage (2 Deployments, 2 Services) | You can afford the extra pods |

## 7. Lab / homework
1. **Easy.** Run `make bg-start`, `make bg-deploy-green`, `make bg-switch`, `make bg-rollback`. After each step, copy the panel headers (`v1 · x/y ready · z% of traffic`) from the dashboard.
   *Expected:* v1 100% → v1 100% with idle v2 (0%) → v2 100% → v1 100%.
2. **Medium.** Run `$KUBECTL get endpoints color-backend -o wide` before and after `make bg-switch`. Compare the pod IPs with the IPs on the dashboard boxes.
   *Expected:* the endpoint list changes from the four blue pod IPs to the four green pod IPs, at the moment of the switch.
3. **Harder.** Do a full release with a bad candidate and show the safe way: set green to v3, use the preview Service to call `/api/color` 20 times (`for i in $(seq 1 20); do curl -s -o /dev/null -w '%{http_code}\n' http://color-backend-preview/api/color; done` inside a temporary curl pod), count the HTTP 500 codes, and decide **not** to switch.
   *Expected:* about 6 of 20 responses are 500 (30% error rate). You do not switch, so the dashboard still shows 100% blue and no errors. Then fix green: `$KUBECTL set image deploy/color-backend-green app=color-backend:v2`.

Next: [Lecture 4: Canary](04-canary.md)
