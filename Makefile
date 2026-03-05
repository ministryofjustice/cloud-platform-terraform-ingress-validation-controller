# nginx-config-validator — ingress-nginx admission validation PoC
#
# Targets assume:
#   - Docker is running
#   - Kind cluster 'ing-validate' exists (or will be created)
#   - Helm v3 is installed

CONTROLLER_TAG  ?= v1.14.3
CHART_VERSION   ?= 4.14.3
IMAGE_NAME      ?= ingress-nginx-validator
IMAGE_TAG       ?= $(CONTROLLER_TAG)-patched
KIND_CLUSTER    ?= ing-validate
NAMESPACE_PROD  ?= ingress-nginx
NAMESPACE_VAL   ?= ingress-nginx-validator

.PHONY: help cluster build load deploy-prod deploy-validator deploy test-backend test-valid test-invalid test clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*##"}{printf "  %-20s %s\n", $$1, $$2}'

# ── Cluster ───────────────────────────────────────────────────
cluster: ## Create Kind cluster
	kind create cluster --name $(KIND_CLUSTER)

# ── Build & Load ──────────────────────────────────────────────
build: ## Build the patched validator image
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) \
		--build-arg INGRESS_NGINX_VERSION=controller-$(CONTROLLER_TAG) .

load: ## Load the image into the Kind cluster
	kind load docker-image $(IMAGE_NAME):$(IMAGE_TAG) --name $(KIND_CLUSTER)

# ── Deploy ────────────────────────────────────────────────────
deploy-prod: ## Deploy the stock (production) ingress-nginx controller
	helm upgrade --install ingress-nginx ingress-nginx \
		--repo https://kubernetes.github.io/ingress-nginx \
		--version $(CHART_VERSION) \
		--namespace $(NAMESPACE_PROD) --create-namespace \
		-f helm/values-production.yaml

deploy-validator: ## Deploy the patched validator controller
	helm upgrade --install ingress-nginx-validator ingress-nginx \
		--repo https://kubernetes.github.io/ingress-nginx \
		--version $(CHART_VERSION) \
		--namespace $(NAMESPACE_VAL) --create-namespace \
		-f helm/values-validator.yaml \
		--set-string controller.extraArgs.update-status=false

deploy: deploy-prod deploy-validator ## Deploy both controllers

# ── Test ──────────────────────────────────────────────────────
test-backend: ## Deploy the demo backend (namespace + deployment + service)
	kubectl apply -f test/backend.yaml

test-valid: ## Apply a valid ingress (should be ALLOWED)
	kubectl apply -f test/ingress-valid.yaml

test-invalid: ## Apply an invalid ingress (should be DENIED by nginx -t)
	kubectl apply -f test/ingress-invalid.yaml

test: test-backend test-valid ## Run the passing tests (backend + valid ingress)

# ── Cleanup ───────────────────────────────────────────────────
clean: ## Delete the Kind cluster
	kind delete cluster --name $(KIND_CLUSTER)
