VERSION = $(shell ./control-plane/build-support/scripts/version.sh control-plane/version/version.go)

# ===========> Helm Targets

gen-helm-docs: ## Generate Helm reference docs from values.yaml and update Consul website. Usage: make gen-helm-docs consul=<path-to-consul-repo>.
	@cd hack/helm-reference-gen; go run ./... $(consul)

copy-crds-to-chart: ## Copy generated CRD YAML into charts/consul. Usage: make copy-crds-to-chart
	@cd hack/copy-crds-to-chart; go run ./...

bats-tests: ## Run Helm chart bats tests.
	 bats --jobs 4 charts/consul/test/unit


# ===========> Control Plane Targets

control-plane-dev: ## Build consul-k8s-control-plane binary.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh -o linux -a amd64

control-plane-dev-docker: ## Build consul-k8s-control-plane dev Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh -o linux -a $(GOARCH)
	@docker build -t '$(DEV_IMAGE)' \
       --target=dev \
       --build-arg 'TARGETARCH=$(GOARCH)' \
       --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' \
       --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' \
       --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' \
       -f $(CURDIR)/control-plane/Dockerfile $(CURDIR)/control-plane

check-remote-dev-image-env:
ifndef REMOTE_DEV_IMAGE
	$(error REMOTE_DEV_IMAGE is undefined: set this image to <your_docker_repo>/<your_docker_image>:<image_tag>, e.g. hashicorp/consul-k8s-dev:latest)
endif

control-plane-dev-docker-multi-arch: check-remote-dev-image-env ## Build consul-k8s-control-plane dev multi-arch Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh -o linux -a "arm64 amd64"
	@docker buildx create --use && docker buildx build -t '$(REMOTE_DEV_IMAGE)' \
       --platform linux/amd64,linux/arm64 \
       --target=dev \
       --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' \
       --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' \
       --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' \
       --push \
       -f $(CURDIR)/control-plane/Dockerfile $(CURDIR)/control-plane

control-plane-test: ## Run go test for the control plane.
	cd control-plane; go test ./...

control-plane-ent-test: ## Run go test with Consul enterprise tests. The consul binary in your PATH must be Consul Enterprise.
	cd control-plane; go test ./... -tags=enterprise

control-plane-cov: ## Run go test with code coverage.
	cd control-plane; go test ./... -coverprofile=coverage.out; go tool cover -html=coverage.out

control-plane-clean: ## Delete bin and pkg dirs.
	@rm -rf \
		$(CURDIR)/control-plane/bin \
		$(CURDIR)/control-plane/pkg

control-plane-lint: cni-plugin-lint ## Run linter in the control-plane directory.
	cd control-plane; golangci-lint run -c ../.golangci.yml

cni-plugin-lint:
	cd control-plane/cni; golangci-lint run -c ../../.golangci.yml

ctrl-generate: get-controller-gen ## Run CRD code generation.
	cd control-plane; $(CONTROLLER_GEN) object:headerFile="build-support/controller/boilerplate.go.txt" paths="./..."




# ===========> CLI Targets

cli-lint: ## Run linter in the control-plane directory.
	cd cli; golangci-lint run -c ../.golangci.yml




# ===========> Acceptance Tests Targets

acceptance-lint: ## Run linter in the control-plane directory.
	cd acceptance; golangci-lint run -c ../.golangci.yml


# ===========> Shared Targets

help: ## Show targets and their descriptions.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-38s\033[0m %s\n", $$1, $$2}'

lint: cni-plugin-lint ## Run linter in the control-plane, cli, and acceptance directories.
	for p in control-plane cli acceptance;  do cd $$p; golangci-lint run --path-prefix $$p -c ../.golangci.yml; cd ..; done

ctrl-manifests: get-controller-gen ## Generate CRD manifests.
	cd control-plane; $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	make copy-crds-to-chart

get-controller-gen: ## Download controller-gen program needed for operator SDK.
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# ===========> CI Targets

ci.aws-acceptance-test-cleanup: ## Deletes AWS resources left behind after failed acceptance tests.
	@cd hack/aws-acceptance-test-cleanup; go run ./... -auto-approve

version:
	@echo $(VERSION)

# ===========> Release Targets

prepare-release: ## Sets the versions, updates changelog to prepare this repository to release
ifndef RELEASE_VERSION
	$(error RELEASE_VERSION is required)
endif
	source $(CURDIR)/control-plane/build-support/scripts/functions.sh; set_release_mode $(CURDIR) $(RELEASE_VERSION) "$(shell date +"%B %d, %Y")" $(PRERELEASE_VERSION)


# ===========> Makefile config

.DEFAULT_GOAL := help
.PHONY: gen-helm-docs copy-crds-to-chart bats-tests help ci.aws-acceptance-test-cleanup version
SHELL = bash
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
DEV_IMAGE?=consul-k8s-control-plane-dev
DOCKER_HUB_USER=$(shell cat $(HOME)/.dockerhub)
GIT_COMMIT?=$(shell git rev-parse --short HEAD)
GIT_DIRTY?=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
GIT_DESCRIBE?=$(shell git describe --tags --always)
CRD_OPTIONS ?= "crd:allowDangerousTypes=true"
