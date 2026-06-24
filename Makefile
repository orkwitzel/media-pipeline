# ─────────────────────────────────────────────────────────────────────────────
# media-pipeline — build images + deploy to the CURRENT kubectl context.
#
# This deploys to whatever `kubectl config current-context` points at — it does
# NOT create a cluster. Every target prints the active context first so you don't
# apply to the wrong one.
#
#   make build       # build the 5 app images
#   make deploy      # kubectl apply all manifests to the current context
#   make up          # build + deploy + wait for rollout
#   make prereqs     # install ingress-nginx + cert-manager (once per cluster)
#   make status      # pods / services / ingress in the namespace
#   make logs APP=worker
#   make undeploy    # delete the app (keeps namespace + PVCs)
#   make nuke        # delete the whole namespace (drops PVCs/data too)
# ─────────────────────────────────────────────────────────────────────────────

KUBECTL       ?= kubectl
NAMESPACE     ?= media-pipeline
K8S_DIR       ?= deploy/k8s
IMAGE_PREFIX  ?= media-pipeline
IMAGE_TAG     ?= latest

# The five services that have a Dockerfile (backing services use public images).
SERVICES      := gateway migrator notifier web worker

# Extra args forwarded to `docker build`, e.g. behind a TLS-intercepting proxy:
#   make build DOCKER_BUILD_ARGS='--build-arg UV_INSECURE_HOST=pypi.org files.pythonhosted.org'
DOCKER_BUILD_ARGS ?=

# Cluster prerequisites (pinned; override if you want different versions).
INGRESS_NGINX_URL ?= https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/cloud/deploy.yaml
CERT_MANAGER_URL  ?= https://github.com/cert-manager/cert-manager/releases/download/v1.16.0/cert-manager.yaml

# Default log target for `make logs`.
APP ?= gateway

.DEFAULT_GOAL := help
.PHONY: help context build deploy up prereqs wait status logs restart undeploy nuke

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n",$$1,$$2}'

context: ## Print the kubectl context this will deploy to
	@echo "context:   $$($(KUBECTL) config current-context)"
	@echo "namespace: $(NAMESPACE)"

build: ## Build all app images (media-pipeline/<svc>:latest)
	@for s in $(SERVICES); do \
		echo "==> build $(IMAGE_PREFIX)/$$s:$(IMAGE_TAG)"; \
		docker build $(DOCKER_BUILD_ARGS) -t $(IMAGE_PREFIX)/$$s:$(IMAGE_TAG) services/$$s || exit 1; \
	done

deploy: context ## Apply all manifests to the current context (in order)
	$(KUBECTL) apply -f $(K8S_DIR)/00-namespace.yaml
	$(KUBECTL) apply -f $(K8S_DIR)/configmap.yaml -f $(K8S_DIR)/secret.yaml
	@# The migrator Job is immutable; delete any prior run so re-deploys succeed.
	-$(KUBECTL) delete job migrator -n $(NAMESPACE) --ignore-not-found
	$(KUBECTL) apply -f $(K8S_DIR)/services/

up: build deploy wait ## Build, deploy, then wait for everything to be ready

prereqs: ## Install ingress-nginx + cert-manager (run once per cluster, before deploy)
	@echo "==> context: $$($(KUBECTL) config current-context)"
	$(KUBECTL) apply -f $(CERT_MANAGER_URL)
	$(KUBECTL) apply -f $(INGRESS_NGINX_URL)
	$(KUBECTL) -n cert-manager rollout status deploy/cert-manager --timeout=180s
	$(KUBECTL) -n ingress-nginx rollout status deploy/ingress-nginx-controller --timeout=180s

wait: ## Wait for the migration Job and all app rollouts to be ready
	-$(KUBECTL) -n $(NAMESPACE) wait --for=condition=complete job/migrator --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status statefulset/postgres --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status statefulset/minio --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status statefulset/rabbitmq --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status deploy/redis --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status deploy/gateway --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status deploy/worker --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status deploy/notifier --timeout=180s
	$(KUBECTL) -n $(NAMESPACE) rollout status deploy/web --timeout=180s

status: ## Show pods, services, ingress, and the TLS certificate
	$(KUBECTL) get pods,svc,ingress -n $(NAMESPACE)
	-$(KUBECTL) get certificate -n $(NAMESPACE)

logs: ## Tail logs for one service:  make logs APP=worker
	$(KUBECTL) logs -n $(NAMESPACE) -l app=$(APP) --tail=100 -f

restart: ## Rolling-restart the stateless app deployments (e.g. after rebuilding images)
	$(KUBECTL) -n $(NAMESPACE) rollout restart deploy/gateway deploy/worker deploy/notifier deploy/web

undeploy: ## Delete the app workloads (KEEPS the namespace and PVCs/data)
	-$(KUBECTL) delete -f $(K8S_DIR)/services/ --ignore-not-found
	-$(KUBECTL) delete -f $(K8S_DIR)/configmap.yaml -f $(K8S_DIR)/secret.yaml --ignore-not-found

nuke: ## Delete the entire namespace — removes PVCs and all data
	-$(KUBECTL) delete namespace $(NAMESPACE)
