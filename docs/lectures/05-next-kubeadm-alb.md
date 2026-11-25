# Lecture 5: Next Steps, kubeadm and AWS ALB (Roadmap)
**Duration:** 30 min · **You will need:** `make up` done, dashboard open on the projector. This is a **roadmap lecture**: no new cluster is built today.

## Learning goals
- Explain the difference between a `kind` cluster and a `kubeadm` cluster.
- Describe what the AWS Load Balancer Controller does.
- Explain how ALB weighted target groups give a percentage-based canary that does not depend on replica count.
- Explain how an ALB listener rule can do a Blue/Green switch.
- Predict what the dashboard will show when `TARGET_URL` points at the ALB, and name what Argo Rollouts adds.

## 0. Before class (instructor checklist)
- [ ] `export KUBECTL="kubectl --context kind-deploy-demo -n color-demo"`
- [ ] A canary setup ready for a recap: `make canary-start`, `make canary-deploy TO=v2`
- [ ] Dashboard open at http://localhost:30080
- [ ] Whiteboard or slide with the two diagrams from section 1
- [ ] Read [docs/architecture.md](../architecture.md), section "How this maps to the ALB phase"

## 1. The idea (≈5 min)
> **Say:** "So far the cluster did the traffic splitting. Kubernetes only knows how to share traffic by **pod count**. In a real company, a load balancer in front of the cluster does the splitting, and it can do it by **percent**."
>
> **Say:** "Next phase, we move from a toy cluster on a laptop to a real cluster on AWS, with an Application Load Balancer, or ALB, at the front. The dashboard stays the same. We only change one setting: where it sends its requests."

- Analogy: today the "receptionist" is the Service and it hands out visitors evenly to whoever is free. With an ALB the receptionist has a rule book: "give 10 out of 100 visitors to the new team."
- Timeline of the course:

```
Phase 1 (now)     Browser ─► dashboard ─► Service color-backend ─► pods        (kind, laptop)
                                              share = pod ratio

Phase 2 (next)    Browser ─► dashboard ─► ALB ─┬─► target group "stable"  weight 90
                                               └─► target group "canary"  weight 10
                                              (kubeadm cluster on AWS)  share = weight, any replica count
```

### kind vs kubeadm
| | `kind` (today) | `kubeadm` (next) |
|---|---|---|
| What it is | Nodes are Docker containers on one laptop | Real machines (for example EC2 instances) joined into a cluster |
| Install time | About 1 minute | About 30 to 60 minutes of steps |
| Load balancer | None (we use a NodePort and port mapping) | You add one: AWS ALB or NLB |
| Good for | Learning, tests, CI | Learning how a real cluster is built, small production |
| You learn | Deployments and Services | Networking (CNI), certificates, upgrades of the control plane, cloud integration |

`kubeadm` is the official tool that sets up a cluster on machines you prepare. You install the container runtime, run `kubeadm init` on the control plane, and `kubeadm join` on the workers. Then you install a network plugin (CNI).

### AWS Load Balancer Controller
A controller is a program inside the cluster that watches objects and creates things outside. The **AWS Load Balancer Controller** watches `Ingress` (and `Service`) objects. When it sees an Ingress with the class `alb`, it creates a real **ALB** in your AWS account, creates **target groups**, and registers your pod IPs in them. With `target-type: ip` the ALB talks to pods directly, so the traffic does not need to go through kube-proxy.

### Percentage canary with weighted target groups
An ALB listener rule can **forward** to several target groups, each with a **weight**. If the weights are 90 and 10, about 10% of requests go to the second group. It does not matter if each group has 1 pod or 50 pods.

| | Replica-ratio canary (Lecture 4) | ALB weighted canary |
|---|---|---|
| Who splits | kube-proxy, evenly over pods | The ALB, by weight |
| 1% canary with 2 pods | Impossible | Weight 99 / 1 |
| Change the share | Start and stop pods (slow) | Change a number (seconds) |
| Canary pod count | Must grow with the share | Can stay small, but must handle the load |

### ALB listener rule switch for Blue/Green
Create two target groups: blue and green. The listener has one rule: "forward to blue." The switch is one change: "forward to green" (or weights 0 and 100). Rollback is the same change in the other direction. It is the same idea as our `slot` selector, but done at the load balancer.

## 2. Read the YAML together, line by line (≈10 min)
There is no YAML in this repository for the ALB phase yet. Below is an **illustration only**. It is not part of the repo, it is not tested, and the names are examples. It shows how a weighted canary is described in an Ingress for the AWS Load Balancer Controller.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: color-backend
  namespace: color-demo
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/actions.color-split: >
      {"type":"forward","forwardConfig":{"targetGroups":[
      {"serviceName":"color-backend-stable","servicePort":80,"weight":90},
      {"serviceName":"color-backend-canary","servicePort":80,"weight":10}]}}
spec:
  ingressClassName: alb
  rules:
    - http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: color-split
                port:
                  name: use-annotation
```

| Line | What it says | Why it matters for THIS strategy |
|---|---|---|
| 1-2 | `apiVersion: networking.k8s.io/v1`, `kind: Ingress` | An Ingress describes HTTP routing into the cluster. |
| 3-5 | `name`, `namespace` | Same namespace as the demo. |
| 6 | `annotations:` | The AWS controller reads its settings from annotations. |
| 7 | `scheme: internet-facing` | The ALB gets a public DNS name. This is the name you will give to the dashboard. |
| 8 | `target-type: ip` | The ALB sends traffic straight to pod IPs. |
| 9-12 | `actions.color-split` with two `targetGroups` and `weight` 90 / 10 | **The canary.** Two Services, two weights. Change 90 / 10 to 70 / 30 to move to 30%. Pod counts do not matter. |
| 13-14 | `spec.ingressClassName: alb` | Tells the AWS controller to handle this Ingress. |
| 15-24 | `rules ... name: color-split, port: use-annotation` | Send everything to the action defined in the annotation. |

For Blue/Green the same Ingress could have `color-backend-blue` with weight 100 and `color-backend-green` with weight 0, and the switch is changing the two numbers.

## 3. Live demo (≈15 min)
This is a whiteboard walk-through with **today's cluster** to refresh the starting point. Nothing in the table needs AWS.

> **Predict first:** "In the future, the dashboard will call the ALB. With weights 90 / 10 and only **1** stable pod and **1** canary pod, what will the traffic bar show?" (About 90% and 10%, not 50% and 50%.)

| Step | Run (make target AND raw kubectl equivalent) | What students see on the dashboard | Say / ask |
|---|---|---|---|
| 1. Show the limit | `make canary-start`<br>raw: `$KUBECTL apply -f k8s/strategies/canary/deployment-stable.yaml -f k8s/strategies/canary/service.yaml`<br>`$KUBECTL rollout status deploy/color-backend-stable --timeout=180s` | Nine blue boxes. | **Say:** "Remember: stable has 9 pods." |
| 2. Canary at 10% | `make canary-deploy TO=v2`<br>raw: `$KUBECTL apply -f k8s/strategies/canary/deployment-canary.yaml`<br>`$KUBECTL set image deploy/color-backend-canary app=color-backend:v2`<br>`$KUBECTL scale deploy/color-backend-stable --replicas=9`<br>`$KUBECTL scale deploy/color-backend-canary --replicas=1`<br>`$KUBECTL rollout status deploy/color-backend-canary --timeout=180s` | One green box, about 10% on the bar. | **Ask:** "How do we get 1%?" (We cannot, with 10 pods. We would need 100.) |
| 3. Where would the ALB sit? | (draw on the board) | | **Say:** "In the ALB phase the dashboard calls the ALB DNS name. The bar shows what the ALB really does." |
| 4. Find the setting | `$KUBECTL get deploy color-dashboard -o jsonpath='{.spec.template.spec.containers[0].env}'` | (terminal) A JSON list with `TARGET_URL` = `http://color-backend/api/color`. | **Say:** "This is the only line we change. In the future it will be `http://<alb-dns-name>/api/color`. Pod watch and pod probes stay the same." |
| 5. Clean up | `make reset` | Page is empty. | |

## 4. Break it on purpose (≈10 min)
These are thought experiments for the next phase. Discuss what the dashboard would show.

| Scenario | What would happen | Dashboard shows |
|---|---|---|
| ALB weights 100 / 0 for stable / canary, canary pods Ready | Canary pods are Ready but get no user traffic | Green boxes dimmed with the badge "no traffic", exactly like the idle green slot in Lecture 3 |
| Canary weight 10 with a v3 canary | About 3% of requests fail, regardless of pod count | Red ring on the v3 box and a small red error count in the legend |
| ALB cannot reach any target (security group wrong) | All requests fail | Traffic bar shows **unreachable**, while the pod boxes still look healthy. Lesson: the traffic probe tells the truth about the user's path |
| `TARGET_URL` is wrong | Probe cannot connect | **unreachable** 100%, even if all pods are fine |

> **Say:** "The pod watch says what exists. The traffic probe says what users get. When they disagree, believe the traffic probe."

## 5. Discussion questions (with answers in a collapsed <details> block)
1. Why can an ALB do a 1% canary with only 2 pods, but a Service cannot?
2. With weights, do you still need a canary Deployment? Why?
3. What is the risk of an ALB Blue/Green switch compared with our `slot` selector?
4. What does Argo Rollouts add to what we did by hand in Lecture 4?
5. What changes for the dashboard when `TARGET_URL` points to the ALB?

<details>
<summary>Answers</summary>

1. The Service picks a pod per connection with equal chance, so the share is the pod ratio. The ALB picks a **target group** by weight first, and only then a pod inside the group.
2. Yes. The canary version must run somewhere. Weights only decide how much traffic it gets. The canary pods must be able to handle that share of the load.
3. Both are one-line changes. With an ALB you also depend on AWS: target registration, health-check timing and idle connections can delay or surprise you. Always check the target group health before the switch.
4. Argo Rollouts replaces the manual `make canary-*` steps. It has a `Rollout` object with steps (10%, 30%, 50%), it can pause for approval, it talks to the ALB (or a service mesh) to set weights, and it can read metrics (for example the error rate) and **abort or promote automatically**.
5. Nothing in the code. The pod watch and pod probes stay the same. Only the traffic bar changes meaning: it shows the ALB weights instead of replica ratios. Note that the dashboard must be able to reach the ALB DNS name.
</details>

## 6. Recap: pros / cons / when to use
Comparison of **ALB weighted routing** with what we built.

| ✅ Pros | ❌ Cons | Use when |
|---|---|---|
| Exact percentages (1%, 5%, 25%), independent of replicas | Needs AWS, a real cluster and an ALB (cost, setup time) | You run on AWS and need fine control |
| Changing weights takes seconds and starts no pods | The AWS Load Balancer Controller must be installed and configured (IAM, subnets, tags) | You release often and want a safe, small first step |
| Works for Blue/Green with one listener-rule change | More moving parts to debug | You want one tool for canary and Blue/Green |
| Pairs with Argo Rollouts for automatic analysis | Cloud lock-in for the exact annotations | You want release decisions based on metrics, not on someone watching a screen |

## 7. Lab / homework
**Before the next phase, prepare these (checklist for students):**
- [ ] An AWS account (a free-tier or sandbox account is enough), with billing alerts on
- [ ] AWS CLI installed and configured (`aws sts get-caller-identity` works)
- [ ] Three small Linux machines or VMs for `kubeadm` (1 control plane, 2 workers), or a plan to create them
- [ ] Read the official pages: "Creating a cluster with kubeadm" and "AWS Load Balancer Controller" installation
- [ ] `helm` installed (the controller is normally installed with Helm)
- [ ] This repository running locally: `make up` and `make e2e` pass

Tasks:
1. **Easy.** Write down in your own words (5 sentences) how `kind` and `kubeadm` differ. Include where the load balancer would come from.
   *Expected:* mentions Docker containers vs real machines, CNI, and that a cloud load balancer must be added.
2. **Medium.** For this table of replica counts, write the best canary share you can reach with the Service, and then with ALB weights: (a) 4 pods in total, (b) 20 pods in total, (c) 2 pods in total.
   *Expected:* Service: (a) 25% steps, (b) 5% steps, (c) 50% steps. ALB: any percent, for example 1%, in all three cases.
3. **Harder.** Draw the data flow of the dashboard in the ALB phase (Browser, dashboard, ALB, two target groups, pods, Kubernetes API). Mark which of the three data sources (pod watch, identity probe, traffic probe) change and which stay the same. Explain why the traffic bar then shows weights.
   *Expected:* pod watch and identity probe unchanged; only the traffic probe target changes (`TARGET_URL`); the bar reflects the ALB's weighted choice because each probe request goes through the ALB.

Back to the start: [Lecture 0](00-setup-and-tour.md) · [README](../../README.md)
