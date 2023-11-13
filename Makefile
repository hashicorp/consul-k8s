VERSION = $(shell ./control-plane/build-support/scripts/version.sh control-plane/version/version.go)
CONSUL_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-version.sh charts/consul/values.yaml)
CONSUL_ENTERPRISE_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-enterprise-version.sh charts/consul/values.yaml)
CONSUL_DATAPLANE_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-dataplane-version.sh charts/consul/values.yaml)
KIND_VERSION= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kindVersion)
KIND_NODE_IMAGE= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kindNodeImage)
KUBECTL_VERSION= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kubectlVersion)

# ===========> Helm Targets

gen-helm-docs: ## Generate Helm reference docs from values.yaml and update Consul website. Usage: make gen-helm-docs consul=<path-to-consul-repo>.
	@cd hack/helm-reference-gen; go run ./... $(consul)

copy-crds-to-chart: ## Copy generated CRD YAML into charts/consul. Usage: make copy-crds-to-chart
	@cd hack/copy-crds-to-chart; go run ./...

camel-crds: ## Convert snake_case keys in yaml to camelCase. Usage: make camel-crds
	@cd hack/camel-crds; go run ./...

generate-external-crds: ## Generate CRDs for externally defined CRDs and copy them to charts/consul. Usage: make generate-external-crds
	@cd ./control-plane/config/crd/external; \
		kustomize build | yq --split-exp '.metadata.name + ".yaml"' --no-doc

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

# DANGER: this target is experimental and could be modified/removed at any time.
# Build consul-k8s-control-plane dev Docker image for use with skaffold or local development.
control-plane-dev-skaffold:
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh -o linux -a $(GOARCH)
	@docker build -t '$(DEV_IMAGE)' \
       --build-arg 'TARGETARCH=$(GOARCH)' \
       -f $(CURDIR)/control-plane/Dockerfile.dev $(CURDIR)/control-plane

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

control-plane-fips-dev-docker: ## Build consul-k8s-control-plane FIPS dev Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh -o linux -a $(GOARCH) --fips
	@docker build -t '$(DEV_IMAGE)' \
       --target=dev \
       --build-arg 'TARGETARCH=$(GOARCH)' \
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
	make ensure-controller-gen-version
	cd control-plane; $(CONTROLLER_GEN) object paths="./..."

# Perform a terraform fmt check but don't change anything
terraform-fmt-check:
	@$(CURDIR)/control-plane/build-support/scripts/terraformfmtcheck.sh $(TERRAFORM_DIR)
.PHONY: terraform-fmt-check

# Format all terraform files according to terraform fmt
terraform-fmt:
	@terraform fmt -recursive
.PHONY: terraform-fmt

# Check for hashicorppreview containers
check-preview-containers:
	@source $(CURDIR)/control-plane/build-support/scripts/check-hashicorppreview.sh


# ===========> CLI Targets
cli-dev:
	@echo "==> Installing consul-k8s CLI tool for ${GOOS}/${GOARCH}"
	@cd cli; go build -o ./bin/consul-k8s; cp ./bin/consul-k8s ${GOPATH}/bin/

cli-fips-dev:
	@echo "==> Installing consul-k8s CLI tool for ${GOOS}/${GOARCH}"
	@cd cli; CGO_ENABLED=1 GOEXPERIMENT=boringcrypto go build -o ./bin/consul-k8s -tags "fips"; cp ./bin/consul-k8s ${GOPATH}/bin/

cli-lint: ## Run linter in the control-plane directory.
	cd cli; golangci-lint run -c ../.golangci.yml


# ===========> Acceptance Tests Targets

acceptance-lint: ## Run linter in the control-plane directory.
	cd acceptance; golangci-lint run -c ../.golangci.yml

# For CNI acceptance tests, the calico CNI pluging needs to be installed on Kind. Our consul-cni plugin will not work 
# without another plugin installed first
kind-cni-calico:
	kubectl create namespace calico-system ||true
	kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/tigera-operator.yaml
	# Sleeps are needed as installs can happen too quickly for Kind to handle it
	@sleep 30
	kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/custom-resources.yaml
	@sleep 20

kind-delete:
	kind delete cluster --name dc1
	kind delete cluster --name dc2
	kind delete cluster --name dc3
	kind delete cluster --name dc4


# Helper target for doing local cni acceptance testing
kind-cni: kind-delete
	kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc1 --image $(KIND_NODE_IMAGE)
	make kind-cni-calico
	kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc2 --image $(KIND_NODE_IMAGE)
	make kind-cni-calico
	kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc3 --image $(KIND_NODE_IMAGE)
	make kind-cni-calico
	kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc4 --image $(KIND_NODE_IMAGE)
	make kind-cni-calico

# Helper target for doing local acceptance testing
kind: kind-delete
	kind create cluster --name dc1 --image $(KIND_NODE_IMAGE)
	kind create cluster --name dc2 --image $(KIND_NODE_IMAGE)
	kind create cluster --name dc3 --image $(KIND_NODE_IMAGE)
	kind create cluster --name dc4 --image $(KIND_NODE_IMAGE)

# Helper target for loading local dev images (run with `DEV_IMAGE=...` to load non-k8s images)
kind-load:
	kind load docker-image --name dc1 $(DEV_IMAGE)
	kind load docker-image --name dc2 $(DEV_IMAGE)
	kind load docker-image --name dc3 $(DEV_IMAGE)
	kind load docker-image --name dc4 $(DEV_IMAGE)

# ===========> Shared Targets

help: ## Show targets and their descriptions.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-38s\033[0m %s\n", $$1, $$2}'

lint: cni-plugin-lint ## Run linter in the control-plane, cli, and acceptance directories.
	for p in control-plane cli acceptance;  do cd $$p; golangci-lint run --path-prefix $$p -c ../.golangci.yml; cd ..; done

ctrl-manifests: get-controller-gen ## Generate CRD manifests.
	make ensure-controller-gen-version
	cd control-plane; $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	make camel-crds
	make copy-crds-to-chart
	make generate-external-crds
	make add-copyright-header

get-controller-gen: ## Download controller-gen program needed for operator SDK.
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.12.1 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

ensure-controller-gen-version: ## Ensure controller-gen version is v0.12.1.
ifeq (, $(shell $(CONTROLLER_GEN) --version | grep v0.12.1))
	@echo "controller-gen version is not v0.12.1, uninstall the binary and install the correct version with 'make get-controller-gen'."
	@echo "Found version: $(shell $(CONTROLLER_GEN) --version)"
	@exit 1
else
	@echo "Found correct version: $(shell $(CONTROLLER_GEN) --version)"
endif

add-copyright-header: ## Add copyright header to all files in the project
ifeq (, $(shell which copywrite))
	@echo "Installing copywrite"
	@go install github.com/hashicorp/copywrite@latest
endif
	@copywrite headers --spdx "MPL-2.0" 

# ===========> CI Targets

ci.aws-acceptance-test-cleanup: ## Deletes AWS resources left behind after failed acceptance tests.
	@cd hack/aws-acceptance-test-cleanup; go run ./... -auto-approve

version:
	@echo $(VERSION)

consul-version:
	@echo $(CONSUL_IMAGE_VERSION)

consul-enterprise-version:
	@echo $(CONSUL_ENTERPRISE_IMAGE_VERSION)

consul-dataplane-version:
	@echo $(CONSUL_DATAPLANE_IMAGE_VERSION)

kind-version:
	@echo $(KIND_VERSION)

kind-node-image:
	@echo $(KIND_NODE_IMAGE)

kubectl-version:
	@echo $(KUBECTL_VERSION)

kind-test-packages:
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/kind_acceptance_test_packages.yaml"

gke-test-packages:
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/gke_acceptance_test_packages.yaml"

eks-test-packages:
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/eks_acceptance_test_packages.yaml"

aks-test-packages:
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/aks_acceptance_test_packages.yaml"

# ===========> Release Targets
check-env:
	@printenv | grep "CONSUL_K8S"

prepare-release-script: ## Sets the versions, updates changelog to prepare this repository to release
ifndef CONSUL_K8S_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_RELEASE_DATE
	$(error CONSUL_K8S_RELEASE_DATE is required, use format <Month> <Day>, <Year> (ex. October 4, 2022))
endif
ifndef CONSUL_K8S_LAST_RELEASE_GIT_TAG
	$(error CONSUL_K8S_LAST_RELEASE_GIT_TAG is required)
endif
ifndef CONSUL_K8S_CONSUL_VERSION
	$(error CONSUL_K8S_CONSUL_VERSION is required)
endif
	@source $(CURDIR)/control-plane/build-support/scripts/functions.sh; prepare_release $(CURDIR) $(CONSUL_K8S_RELEASE_VERSION) "$(CONSUL_K8S_RELEASE_DATE)" $(CONSUL_K8S_LAST_RELEASE_GIT_TAG) $(CONSUL_K8S_CONSUL_VERSION) $(CONSUL_K8S_CONSUL_DATAPLANE_VERSION) $(CONSUL_K8S_PRERELEASE_VERSION); \

prepare-release: prepare-release-script check-preview-containers

prepare-rc-script: ## Sets the versions, updates changelog to prepare this repository to release
ifndef CONSUL_K8S_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_RELEASE_DATE
	$(error CONSUL_K8S_RELEASE_DATE is required, use format <Month> <Day>, <Year> (ex. October 4, 2022))
endif
ifndef CONSUL_K8S_LAST_RELEASE_GIT_TAG
	$(error CONSUL_K8S_LAST_RELEASE_GIT_TAG is required)
endif
ifndef CONSUL_K8S_CONSUL_VERSION
	$(error CONSUL_K8S_CONSUL_VERSION is required)
endif
	@source $(CURDIR)/control-plane/build-support/scripts/functions.sh; prepare_rc_branch $(CURDIR) $(CONSUL_K8S_RELEASE_VERSION) "$(CONSUL_K8S_RELEASE_DATE)" $(CONSUL_K8S_LAST_RELEASE_GIT_TAG) $(CONSUL_K8S_CONSUL_VERSION) $(CONSUL_K8S_CONSUL_DATAPLANE_VERSION) $(CONSUL_K8S_PRERELEASE_VERSION); \

prepare-rc-branch: prepare-rc-script

prepare-main-dev:
ifndef CONSUL_K8S_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_RELEASE_DATE
	$(error CONSUL_K8S_RELEASE_DATE is required, use format <Month> <Day>, <Year> (ex. October 4, 2022))
endif
ifndef CONSUL_K8S_NEXT_RELEASE_VERSION
	$(error CONSUL_K8S_NEXT_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_NEXT_CONSUL_VERSION
	$(error CONSUL_K8S_NEXT_CONSUL_VERSION is required)
endif
ifndef CONSUL_K8S_NEXT_CONSUL_DATAPLANE_VERSION
	$(error CONSUL_K8S_NEXT_CONSUL_DATAPLANE_VERSION is required)
endif
	source $(CURDIR)/control-plane/build-support/scripts/functions.sh; prepare_dev $(CURDIR) $(CONSUL_K8S_RELEASE_VERSION) "$(CONSUL_K8S_RELEASE_DATE)" "" $(CONSUL_K8S_NEXT_RELEASE_VERSION) $(CONSUL_K8S_NEXT_CONSUL_VERSION) $(CONSUL_K8S_NEXT_CONSUL_DATAPLANE_VERSION)

prepare-release-dev:
ifndef CONSUL_K8S_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_RELEASE_DATE
	$(error CONSUL_K8S_RELEASE_DATE is required, use format <Month> <Day>, <Year> (ex. October 4, 2022))
endif
ifndef CONSUL_K8S_NEXT_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_CONSUL_VERSION
	$(error CONSUL_K8S_CONSUL_VERSION is required)
endif
ifndef CONSUL_K8S_CONSUL_DATAPLANE_VERSION
	$(error CONSUL_K8S_CONSUL_DATAPLANE_VERSION is required)
endif
	source $(CURDIR)/control-plane/build-support/scripts/functions.sh; prepare_dev $(CURDIR) $(CONSUL_K8S_RELEASE_VERSION) "$(CONSUL_K8S_RELEASE_DATE)" "" $(CONSUL_K8S_NEXT_RELEASE_VERSION) $(CONSUL_K8S_CONSUL_VERSION) $(CONSUL_K8S_CONSUL_DATAPLANE_VERSION)

# ===========> Makefile config
.DEFAULT_GOAL := help
.PHONY: gen-helm-docs copy-crds-to-chart generate-external-crds bats-tests help ci.aws-acceptance-test-cleanup version cli-dev prepare-dev prepare-release
SHELL = bash
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
DEV_IMAGE?=consul-k8s-control-plane-dev
DOCKER_HUB_USER=$(shell cat $(HOME)/.dockerhub)
GIT_COMMIT?=$(shell git rev-parse --short HEAD)
GIT_DIRTY?=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
GIT_DESCRIBE?=$(shell git describe --tags --always)
CRD_OPTIONS ?= "crd:ignoreUnexportedFields=true,allowDangerousTypes=true"
