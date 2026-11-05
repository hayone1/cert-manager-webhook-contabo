# Run `make help` to see every target.

BINARY        ?= webhook
IMAGE         ?= ghcr.io/hayone1/cert-manager-webhook-contabo
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PLATFORMS     ?= linux/amd64,linux/arm64
GOLANGCI_LINT ?= v2.15.0
LDFLAGS       := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
CHART         := charts/cert-manager-webhook-contabo

# Kubernetes version of the envtest binaries (etcd, kube-apiserver, kubectl)
# used by the conformance suite, and the setup-envtest version that fetches them.
ENVTEST_K8S_VERSION ?= 1.35.0
SETUP_ENVTEST       ?= sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.24
LOCALBIN            ?= $(CURDIR)/bin

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the binary into ./bin.
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/webhook

.PHONY: test
test: ## Run unit tests with the race detector (no network, no Contabo account).
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests and print the coverage.
	go test -count=1 -coverprofile=cover.out ./...
	go tool cover -func=cover.out | tail -1

.PHONY: envtest
envtest: ## Download the envtest binaries for the conformance suite into ./bin.
	@mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install $(SETUP_ENVTEST)
	$(LOCALBIN)/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path > $(LOCALBIN)/envtest-path

.PHONY: conformance
conformance: envtest ## Run cert-manager's DNS-01 conformance suite against the fake Contabo API.
	@assets=$$(cat $(LOCALBIN)/envtest-path); \
	TEST_ASSET_ETCD=$$assets/etcd \
	TEST_ASSET_KUBE_APISERVER=$$assets/kube-apiserver \
	TEST_ASSET_KUBECTL=$$assets/kubectl \
	go test -tags conformance -count=1 -v -timeout 20m ./test/conformance/

.PHONY: conformance-real
conformance-real: envtest ## Run the conformance suite against the REAL Contabo API (needs .env; local only).
	@set -a; [ -f .env ] && . ./.env; set +a; \
	: "$${CONFORMANCE_ZONE:=$${E2E_ZONE:?set CONFORMANCE_ZONE or E2E_ZONE in .env}}"; export CONFORMANCE_ZONE; \
	assets=$$(cat $(LOCALBIN)/envtest-path); \
	CONFORMANCE_MODE=real \
	TEST_ASSET_ETCD=$$assets/etcd \
	TEST_ASSET_KUBE_APISERVER=$$assets/kube-apiserver \
	TEST_ASSET_KUBECTL=$$assets/kubectl \
	go test -tags conformance -count=1 -v -timeout 40m -run TestConformanceReal ./test/conformance/

.PHONY: lint
lint: ## Run golangci-lint (built with this module's Go version).
	GOTOOLCHAIN=$$(go env GOVERSION) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT) run ./...
	GOTOOLCHAIN=$$(go env GOVERSION) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT) run --build-tags conformance ./test/conformance/

.PHONY: fmt
fmt: ## Format the code.
	gofmt -s -w .

.PHONY: vuln
vuln: ## Look for known vulnerabilities in dependencies.
	GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum.
	go mod tidy

.PHONY: chart-lint
chart-lint: ## Lint the Helm chart and check the rendered manifests with kubeconform.
	helm lint --strict $(CHART)
	helm lint --strict $(CHART) -f $(CHART)/ci/full-values.yaml
	helm template test $(CHART) -f $(CHART)/ci/full-values.yaml | \
	  go run github.com/yannh/kubeconform/cmd/kubeconform@v0.7.0 -strict -summary \
	    -schema-location default \
	    -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
	@# cert-manager refuses a commonName longer than 64 bytes; check with the longest name.
	@cn=$$(helm template a-very-long-release-name-that-hits-the-fullname-limit $(CHART) | sed -n 's/^ *commonName: *//p' | tr -d '"'); \
	  for c in $$cn; do [ $${#c} -le 64 ] || { echo "commonName too long ($${#c} bytes): $$c"; exit 1; }; done; \
	  echo "commonName length ok: $$cn"

.PHONY: docker-build
docker-build: ## Build the image for your machine's platform.
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE):$(VERSION) .

.PHONY: docker-buildx
docker-buildx: ## Build the multi-arch image (no push).
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE):$(VERSION) .

.PHONY: e2e-images
e2e-images: ## Build the webhook and fake Contabo API images for the end-to-end test.
	docker build --build-arg VERSION=e2e -t cert-manager-webhook-contabo:e2e .
	docker build -f test/e2e/fakecontabo/Dockerfile -t fake-contabo:e2e .

.PHONY: e2e
e2e: ## Run the end-to-end test (E2E_MODE=fake by default, or real). See docs/development.md.
	./test/e2e/run.sh

.PHONY: clean
clean: ## Remove build output.
	chmod -R u+w $(LOCALBIN) 2>/dev/null || true
	rm -rf $(LOCALBIN) dist cover.out
