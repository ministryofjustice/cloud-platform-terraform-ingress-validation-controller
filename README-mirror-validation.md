# nginx-config-validator

Restore full-depth `nginx -t` admission validation for ingress-nginx, mitigating
the gap left when CVE-2025-1974 disabled template testing in v1.12.1+.

## Architecture

```
                          ┌──────────────────────────┐
  kubectl apply ──►       │  ValidatingWebhookConfig │
                          │  (routes to validator)   │
                          └────────────┬─────────────┘
                                       │ AdmissionReview
                                       ▼
         ┌─────────────────────────────────────────────────────┐
         │  Patched ingress-nginx (validator)                  │
         │  • nginx -t re-enabled via one-line patch           │
         │  • Admission webhook on :8443                       │
         │  • Does NOT serve traffic (service.enabled=false)   │
         └─────────────────────────────────────────────────────┘
                    ╱                                ╲
          ALLOW (valid config)              DENY (nginx -t fails)
                    ╲                                ╱
                     ▼
         ┌─────────────────────────────────────────────────────┐
         │  Stock ingress-nginx (production)                   │
         │  • Unmodified upstream image                        │
         │  • Handles actual traffic routing                   │
         │  • Admission webhooks disabled                      │
         └─────────────────────────────────────────────────────┘
```

Two ingress-nginx deployments share the same IngressClass (`nginx`):

| Role       | Image                      | Admission | Traffic | Election ID                          |
|------------|----------------------------|-----------|---------|--------------------------------------|
| Production | Stock upstream              | OFF       | YES     | `ingress-controller-leader`          |
| Validator  | Patched (nginx -t enabled) | ON        | NO      | `ingress-controller-leader-validator`|

## The Patch

A single change to `internal/ingress/controller/controller.go` in the
ingress-nginx source — uncomment the `testTemplate(content)` call that was
disabled for CVE-2025-1974:

```diff
-	/* Deactivated to mitigate CVE-2025-1974
-	// TODO: Implement sandboxing so this test can be done safely
+	// Re-enabled for admission validation PoC (CVE-2025-1974 mitigated by network isolation)
 	err = n.testTemplate(content)
 	if err != nil {
 		n.metricCollector.IncCheckErrorCount(ing.ObjectMeta.Namespace, ing.Name)
 		return err
 	}
-	*/
```

The security concern (arbitrary config rendering during admission) is mitigated
by network-isolating the validator pod — it is not exposed to tenant traffic.

## Quick Start (Kind)

```bash
# 1. Create a Kind cluster
make cluster

# 2. Build the patched image
make build

# 3. Load it into Kind
make load

# 4. Deploy both controllers
make deploy

# 5. Wait for pods to be ready
kubectl -n ingress-nginx wait --for=condition=ready pod -l app.kubernetes.io/name=ingress-nginx --timeout=120s
kubectl -n ingress-nginx-validator wait --for=condition=ready pod -l app.kubernetes.io/name=ingress-nginx --timeout=120s

# 6. Deploy demo backend
make test-backend

# 7. Test valid ingress (should succeed)
make test-valid

# 8. Test invalid ingress (should be DENIED with nginx -t error)
make test-invalid
```

## Repository Layout

```
├── Dockerfile                          # Multi-stage build: clone → patch → compile → overlay
├── Makefile                            # Build, deploy, and test targets
├── helm/
│   ├── values-production.yaml          # Stock controller (no admission, serves traffic)
│   └── values-validator.yaml           # Patched controller (admission on, no traffic)
├── patch/
│   └── re-enable-nginx-test.patch      # The one-line patch for controller.go
└── test/
    ├── backend.yaml                    # Demo namespace + deployment + service
    ├── ingress-valid.yaml              # Valid ingress (allowed)
    └── ingress-invalid.yaml            # Invalid ingress — broken regex (denied)
```

## Configuration

| Variable         | Default           | Description                            |
|------------------|-------------------|----------------------------------------|
| `CONTROLLER_TAG` | `v1.14.3`         | ingress-nginx controller version       |
| `CHART_VERSION`  | `4.14.3`          | Helm chart version                     |
| `IMAGE_NAME`     | `ingress-nginx-validator` | Docker image name             |
| `KIND_CLUSTER`   | `ing-validate`    | Kind cluster name                      |

Override via `make build CONTROLLER_TAG=v1.15.0 CHART_VERSION=4.15.0`.

## CVE-2025-1974 Context

In ingress-nginx v1.12.1, the `testTemplate()` call in `CheckIngress()` was
commented out to mitigate CVE-2025-1974 — a vulnerability where the admission
webhook's config-rendering path could be exploited by attackers with pod-level
network access. The upstream fix disabled the deepest validation check (`nginx -t`
against the full merged config), leaving only annotation-level validation.

This project restores `nginx -t` testing via a dedicated validator deployment.
When paired with a NetworkPolicy that restricts the validator pod's egress to only the Kubernetes API server, tenant workloads cannot reach it; deploying without such a policy leaves the validator vulnerable to the original CVE-2025-1974 network attack vector.
