# End-to-End Testing Guide

## Prerequisites

- Docker
- [Kind](https://kind.sigs.k8s.io/)
- Helm v3
- kubectl

## Setup

```bash
# 1. Create a Kind cluster
kind create cluster --name ing-validate

# 2. Build the patched validator image
docker build -t ingress-nginx-validator:v1.14.3-patched .

# 3. Load it into Kind
kind load docker-image ingress-nginx-validator:v1.14.3-patched --name ing-validate

# 4. Add the ingress-nginx Helm repo
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
```

## Single-class test (quickest path)

```bash
# Deploy stock production controller (no admission webhook)
helm install prod-default ingress-nginx/ingress-nginx \
  -n ingress-nginx-default --create-namespace \
  -f helm/values-prod-default.yaml

# Deploy patched validator (admission webhook enabled)
helm install validator-default ingress-nginx/ingress-nginx \
  -n validator-default --create-namespace \
  -f helm/values-validator-default.yaml

# Wait for pods
kubectl -n ingress-nginx-default wait --for=condition=ready pod \
  -l app.kubernetes.io/name=ingress-nginx --timeout=120s
kubectl -n validator-default wait --for=condition=ready pod \
  -l app.kubernetes.io/name=ingress-nginx --timeout=120s

# Deploy demo backend
kubectl apply -f test/backend.yaml

# Valid ingress — should succeed
kubectl apply -f test/ingress-valid.yaml

# Invalid ingress — should be DENIED with pcre_compile() error
kubectl apply -f test/ingress-invalid.yaml
# Expected: "admission webhook denied the request ... pcre_compile() failed: missing )"

# Check the validator logs for the full rendered nginx.conf
kubectl -n validator-default logs deploy/validator-default-ingress-nginx-controller \
  | grep -A 5 "ADMISSION VALIDATED"
```

## Multi-class test (3 IngressClasses)

```bash
# Deploy 3 production controllers
for class in default modsec internal; do
  helm install prod-${class} ingress-nginx/ingress-nginx \
    -n ingress-nginx-${class} --create-namespace \
    -f helm/values-prod-${class}.yaml
done

# Deploy 3 validators
for class in default modsec internal; do
  helm install validator-${class} ingress-nginx/ingress-nginx \
    -n validator-${class} --create-namespace \
    -f helm/values-validator-${class}.yaml
done

# Wait for all pods
for ns in ingress-nginx-default ingress-nginx-modsec ingress-nginx-internal \
          validator-default validator-modsec validator-internal; do
  kubectl -n $ns wait --for=condition=ready pod \
    -l app.kubernetes.io/name=ingress-nginx --timeout=120s
done

# Deploy demo backend
kubectl apply -f test/backend.yaml
```

### Verify IngressClasses have unique controller values

```bash
kubectl get ingressclass -o custom-columns=NAME:.metadata.name,CONTROLLER:.spec.controller
```

Expected:

```
nginx            k8s.io/ingress-nginx
nginx-modsec     k8s.io/ingress-nginx-modsec
nginx-internal   k8s.io/ingress-nginx-internal
```

### Create valid ingresses for each class

```bash
for class in nginx nginx-modsec nginx-internal; do
  cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-${class}
  namespace: demo
spec:
  ingressClassName: ${class}
  rules:
  - host: test-${class}.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: demo-backend
            port:
              number: 80
EOF
done
```

Expected: all 3 created successfully.

### Create invalid ingresses for each class

```bash
for class in nginx nginx-modsec nginx-internal; do
  echo "--- Testing invalid ingress for class: ${class} ---"
  cat <<EOF | kubectl apply -f - 2>&1
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: invalid-${class}
  namespace: demo
  annotations:
    nginx.ingress.kubernetes.io/use-regex: "true"
spec:
  ingressClassName: ${class}
  rules:
  - host: invalid-${class}.example.com
    http:
      paths:
      - path: "/test(/(.*)"
        pathType: ImplementationSpecific
        backend:
          service:
            name: demo-backend
            port:
              number: 80
EOF
done
```

Expected: all 3 denied with `pcre_compile() failed: missing )`.

### Verify class isolation

Each validator should only render `server_name` entries for its own IngressClass:

```bash
for ns in validator-default validator-modsec validator-internal; do
  echo "=== ${ns} ==="
  kubectl -n $ns logs deploy/${ns}-ingress-nginx-controller \
    | grep "server_name " | grep -v hash | sort -u
done
```

Expected: each validator lists only its own class's hostnames — no cross-class bleed.

## Cleanup

```bash
kind delete cluster --name ing-validate
```
