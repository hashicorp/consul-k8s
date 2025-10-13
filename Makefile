VERSION = $(shell ./control-plane/build-support/scripts/version.sh version/version.go)
GOLANG_VERSION?=$(shell head -n 1 .go-version)
CONSUL_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-version.sh charts/consul/values.yaml)
CONSUL_ENTERPRISE_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-enterprise-version.sh charts/consul/values.yaml)
CONSUL_DATAPLANE_IMAGE_VERSION = $(shell ./control-plane/build-support/scripts/consul-dataplane-version.sh charts/consul/values.yaml)
KIND_VERSION= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kindVersion)
KIND_NODE_IMAGE= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kindNodeImage)
KUBECTL_VERSION= $(shell ./control-plane/build-support/scripts/read-yaml-config.sh acceptance/ci-inputs/kind-inputs.yaml .kubectlVersion)

GO_MODULES := $(shell find . -name go.mod -exec dirname {} \; | sort)

GOTESTSUM_PATH?=$(shell command -v gotestsum)

##@ Helm Targets

.PHONY: gen-helm-docs
gen-helm-docs: ## Generate Helm reference docs from values.yaml and update Consul website. Usage: make gen-helm-docs consul=<path-to-consul-repo>.
	@cd hack/helm-reference-gen; go run ./... $(consul)

.PHONY: copy-crds-to-chart
copy-crds-to-chart: ## Copy generated CRD YAML into charts/consul. Usage: make copy-crds-to-chart
	@cd hack/copy-crds-to-chart; go run ./...

.PHONY: camel-crds
camel-crds: ## Convert snake_case keys in yaml to camelCase. Usage: make camel-crds
	@cd hack/camel-crds; go run ./...

.PHONY: generate-external-crds
generate-external-crds: ## Generate CRDs for externally defined CRDs and copy them to charts/consul. Usage: make generate-external-crds
	@cd ./control-plane/config/crd/external; \
		kustomize build | yq --split-exp '.metadata.name + ".yaml"' --no-doc

.PHONY: bats-tests
bats-tests: ## Run Helm chart bats tests.
	docker run -it -v $(CURDIR):/consul-k8s hashicorpdev/consul-helm-test:latest bats --jobs 4 /consul-k8s/charts/consul/test/unit -f "$(TEST_NAME)"

##@ Control Plane Targets

.PHONY: control-plane-dev
control-plane-dev: ## Build consul-k8s-control-plane binary.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh --os linux --arch amd64

.PHONY: dev-docker
dev-docker: control-plane-dev-docker ## build dev local dev docker image
	docker tag '$(DEV_IMAGE)' 'consul-k8s-control-plane:local'

.PHONY: control-plane-dev-docker
control-plane-dev-docker: ## Build consul-k8s-control-plane dev Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh --os linux --arch $(GOARCH)
	@docker buildx build --debug --platform $(GOOS)/$(GOARCH) -t '$(DEV_IMAGE)' \
	   --no-cache \
       --target=dev \
       --build-arg 'GOLANG_VERSION=$(GOLANG_VERSION)' \
       --build-arg 'TARGETARCH=$(GOARCH)' \
       --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' \
       --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' \
       --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' \
       -f $(CURDIR)/control-plane/Dockerfile $(CURDIR)/control-plane

.PHONY: control-plane-dev-skaffold
# DANGER: this target is experimental and could be modified/removed at any time.
control-plane-dev-skaffold: ## Build consul-k8s-control-plane dev Docker image for use with skaffold or local development.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh --os linux --arch $(GOARCH)
	@docker build -t '$(DEV_IMAGE)' \
       --build-arg 'GOLANG_VERSION=$(GOLANG_VERSION)' \
       --build-arg 'TARGETARCH=$(GOARCH)' \
       -f $(CURDIR)/control-plane/Dockerfile.dev $(CURDIR)/control-plane

.PHONY: check-remote-dev-image-env
check-remote-dev-image-env:
ifndef REMOTE_DEV_IMAGE
	$(error REMOTE_DEV_IMAGE is undefined: set this image to <your_docker_repo>/<your_docker_image>:<image_tag>, e.g. hashicorp/consul-k8s-dev:latest)
endif

.PHONY: control-plane-dev-docker-multi-arch
control-plane-dev-docker-multi-arch: check-remote-dev-image-env ## Build consul-k8s-control-plane dev multi-arch Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh --os linux --arch "arm64 amd64"
	@docker buildx create --use && docker buildx build -t '$(REMOTE_DEV_IMAGE)' \
       --platform linux/amd64,linux/arm64 \
       --target=dev \
       --build-arg 'GOLANG_VERSION=$(GOLANG_VERSION)' \
       --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' \
       --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' \
       --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' \
       --push \
       -f $(CURDIR)/control-plane/Dockerfile $(CURDIR)/control-plane

.PHONY: control-plane-fips-dev-docker
control-plane-fips-dev-docker: ## Build consul-k8s-control-plane FIPS dev Docker image.
	@$(SHELL) $(CURDIR)/control-plane/build-support/scripts/build-local.sh --os linux --arch $(GOARCH) --fips
	@docker build -t '$(DEV_IMAGE)' \
       --target=dev \
       --build-arg 'GOLANG_VERSION=$(GOLANG_VERSION)' \
       --build-arg 'TARGETARCH=$(GOARCH)' \
       --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' \
       --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' \
       --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' \
       --push \
       -f $(CURDIR)/control-plane/Dockerfile $(CURDIR)/control-plane

.PHONY: control-plane-test
control-plane-test: ## Run go test for the control plane.
ifeq ("$(GOTESTSUM_PATH)","")
	cd control-plane && go test ./...
else
	cd control-plane && \
	gotestsum \
		--format=pkgname \
		--debug \
		--rerun-fails=3 \
		--packages="./..."
endif


.PHONY: control-plane-ent-test
control-plane-ent-test: ## Run go test with Consul enterprise tests. The consul binary in your PATH must be Consul Enterprise.
ifeq ("$(GOTESTSUM_PATH)","")
	cd control-plane && go test ./... -tags=enterprise
else
	cd control-plane && \
	gotestsum \
		--format=pkgname \
		--debug \
		--rerun-fails=3 \
		--packages="./..." \
		-- \
		--tags enterprise
endif

.PHONY: control-plane-cov
control-plane-cov: ## Run go test with code coverage.
	cd control-plane; go test ./... -coverprofile=coverage.out; go tool cover -html=coverage.out

.PHONY: control-plane-clean
control-plane-clean: ## Delete bin and pkg dirs.
	@rm -rf \
		$(CURDIR)/control-plane/bin \
		$(CURDIR)/control-plane/pkg
	@rm -rf \
		$(CURDIR)/control-plane/cni/bin \
		$(CURDIR)/control-plane/cni/pkg

.PHONY: control-plane-lint
control-plane-lint: cni-plugin-lint ## Run linter in the control-plane directory.
	cd control-plane; golangci-lint run -c ../.golangci.yml

.PHONY: cni-plugin-lint
cni-plugin-lint:
	cd control-plane/cni; golangci-lint run -c ../../.golangci.yml

.PHONY: ctrl-generate
ctrl-generate: get-controller-gen ## Run CRD code generation.
	make ensure-controller-gen-version
	cd control-plane; $(CONTROLLER_GEN) object paths="./..."

.PHONY: terraform-fmt-check
terraform-fmt-check: ## Perform a terraform fmt check but don't change anything
	@$(CURDIR)/control-plane/build-support/scripts/terraformfmtcheck.sh $(TERRAFORM_DIR)

.PHONY: terraform-fmt
terraform-fmt: ## Format all terraform files according to terraform fmt
	@terraform fmt -recursive

.PHONY: check-preview-containers
check-preview-containers: ## Check for hashicorppreview containers
	@source $(CURDIR)/control-plane/build-support/scripts/check-hashicorppreview.sh

##@ CLI Targets

.PHONY: cli-dev
cli-dev: ## run cli dev
	@echo "==> Installing consul-k8s CLI tool for ${GOOS}/${GOARCH}"
	@cd cli; go build -o ./bin/consul-k8s; cp ./bin/consul-k8s ${GOPATH}/bin/

.PHONY: cli-fips-dev
cli-fips-dev: ## run cli fips dev
	@echo "==> Installing consul-k8s CLI tool for ${GOOS}/${GOARCH}"
	@cd cli; CGO_ENABLED=1 GOEXPERIMENT=boringcrypto go build -o ./bin/consul-k8s -tags "fips"; cp ./bin/consul-k8s ${GOPATH}/bin/

.PHONY: cli-lint
cli-lint: ## Run linter in the control-plane directory.
	cd cli; golangci-lint run -c ../.golangci.yml

##@ Acceptance Tests Targets

.PHONY: acceptance-lint
acceptance-lint: ## Run linter in the control-plane directory.
	cd acceptance; golangci-lint run -c ../.golangci.yml

.PHONY: kind-cni-calico
# For CNI acceptance tests, the calico CNI plugin needs to be installed on Kind. Our consul-cni plugin will not work
# without another plugin installed first
kind-cni-calico: ## install cni plugin on kind
	kubectl create namespace calico-system || true
	kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/tigera-operator.yaml
	# Sleeps are needed as installs can happen too quickly for Kind to handle it
	@sleep 30
	kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/custom-resources.yaml
	@if [ "$(DUAL_STACK)" = "true" ]; then \
		echo "Adding IPv6 config..."; \
		kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/custom-resources-ipv6.yaml; \
	else \
		echo "Adding IPv4 config..."; \
		kubectl create -f $(CURDIR)/acceptance/framework/environment/cni-kind/custom-resources.yaml; \
	fi
	@sleep 20

.PHONY: kind-delete
kind-delete:
	kind delete cluster --name dc1
	kind delete cluster --name dc2
	kind delete cluster --name dc3
	kind delete cluster --name dc4

.PHONY: kind-cni
kind-cni: kind-delete ## Helper target for doing local cni acceptance testing
	@if [ "$(DUAL_STACK)" = "true" ]; then \
		echo "Creating IPv6 clusters..."; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind-ipv6.config --name dc1 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind-ipv6.config --name dc2 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind-ipv6.config --name dc3 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind-ipv6.config --name dc4 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
	else \
		echo "Creating IPv4 clusters..."; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc1 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc2 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc3 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/cni-kind/kind.config --name dc4 --image $(KIND_NODE_IMAGE); \
		make kind-cni-calico; \
	fi

.PHONY: kind
kind: kind-delete ## Helper target for doing local acceptance testing (works in all cases)
	@if [ "$(DUAL_STACK)" = "true" ]; then \
		echo "Creating IPv6 clusters..."; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind-ipv6.config --name dc1 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind-ipv6.config --name dc2 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind-ipv6.config --name dc3 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind-ipv6.config --name dc4 --image $(KIND_NODE_IMAGE); \
	else \
		echo "Creating IPv4 clusters..."; \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind.config --name dc1 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind.config --name dc2 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind.config --name dc3 --image $(KIND_NODE_IMAGE); \
		kind create cluster --config=$(CURDIR)/acceptance/framework/environment/kind/kind.config --name dc4 --image $(KIND_NODE_IMAGE); \
	fi


.PHONY: kind-small
kind-small: kind-delete ## Helper target for doing local acceptance testing (when you only need two clusters)
	kind create cluster --name dc1 --image $(KIND_NODE_IMAGE)
	kind create cluster --name dc2 --image $(KIND_NODE_IMAGE)

.PHONY: kind-load
kind-load: ## Helper target for loading local dev images (run with `DEV_IMAGE=...` to load non-k8s images)
	kind load docker-image --name dc1 $(DEV_IMAGE)
	kind load docker-image --name dc2 $(DEV_IMAGE)
	kind load docker-image --name dc3 $(DEV_IMAGE)
	kind load docker-image --name dc4 $(DEV_IMAGE)

##@ Shared Targets

.PHONY: lint
lint: cni-plugin-lint ## Run linter in the control-plane, cli, and acceptance directories.
	for p in control-plane cli acceptance;  do cd $$p; golangci-lint run --path-prefix $$p -c ../.golangci.yml; cd ..; done

.PHONY: ctrl-manifests
ctrl-manifests: get-controller-gen ## Generate CRD manifests.
	make ensure-controller-gen-version
	cd control-plane; $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	make camel-crds
	make copy-crds-to-chart
	make generate-external-crds
	make add-copyright-header

.PHONY: get-controller-gen
get-controller-gen: ## Download controller-gen program needed for operator SDK.
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.PHONY: ensure-controller-gen-version
ensure-controller-gen-version: ## Ensure controller-gen version is v0.14.0.
ifeq (, $(shell which $(CONTROLLER_GEN)))
	@echo "You don't have $(CONTROLLER_GEN), please install it first."
else
ifeq (, $(shell $(CONTROLLER_GEN) --version | grep v0.14.0))
	@echo "controller-gen version is not v0.14.0, uninstall the binary and install the correct version with 'make get-controller-gen'."
	@echo "Found version: $(shell $(CONTROLLER_GEN) --version)"
	@exit 1
else
	@echo "Found correct version: $(shell $(CONTROLLER_GEN) --version)"
endif
endif

.PHONY: add-copyright-header
add-copyright-header: ## Add copyright header to all files in the project
ifeq (, $(shell which copywrite))
	@echo "Installing copywrite"
	@go install github.com/hashicorp/copywrite@latest
endif
	@copywrite headers --spdx "MPL-2.0" 

##@ CI Targets

.PHONY: ci.aws-acceptance-test-cleanup
ci.aws-acceptance-test-cleanup: ## Deletes AWS resources left behind after failed acceptance tests.
	@cd hack/aws-acceptance-test-cleanup; go run ./... -auto-approve

.PHONY: version
version: ## print version
	@echo $(VERSION)

.PHONY: consul-version
consul-version: ## print consul version
	@echo $(CONSUL_IMAGE_VERSION)

.PHONY: consul-enterprise-version
consul-enterprise-version: ## print consul ent version
	@echo $(CONSUL_ENTERPRISE_IMAGE_VERSION)

.PHONY: consul-dataplane-version
consul-dataplane-version: ## print consul data-plane version
	@echo $(CONSUL_DATAPLANE_IMAGE_VERSION)

.PHONY: kind-version
kind-version: ## print kind version
	@echo $(KIND_VERSION)

.PHONY: kind-node-image
kind-node-image: ## print kind node image
	@echo $(KIND_NODE_IMAGE)

.PHONY: kubectl-version
kubectl-version: ## print kubectl version
	@echo $(KUBECTL_VERSION)

.PHONY: kind-test-packages
kind-test-packages: ## kind test packages
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/kind_acceptance_test_packages.yaml"

.PHONY: gke-test-packages
gke-test-packages: ## gke test packages
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/gke_acceptance_test_packages.yaml"

.PHONY: eks-test-packages
eks-test-packages: ## eks test packages
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/eks_acceptance_test_packages.yaml"

.PHONY: aks-test-packages
aks-test-packages: ## aks test packages
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/aks_acceptance_test_packages.yaml"


.PHONY: openshift-test-packages
openshift-test-packages: ## openshift test packages
	@./control-plane/build-support/scripts/set_test_package_matrix.sh "acceptance/ci-inputs/openshift_acceptance_test_packages.yaml"

.PHONY: go-mod-tidy
go-mod-tidy: ## Recursively run go mod tidy on all subdirectories
	@./control-plane/build-support/scripts/mod_tidy.sh

.PHONY: check-mod-tidy
check-mod-tidy: ## Recursively run go mod tidy on all subdirectories and check if there are any changes
	@./control-plane/build-support/scripts/mod_tidy.sh --check

.PHONY: go-mod-get
go-mod-get: $(foreach mod,$(GO_MODULES),go-mod-get/$(mod)) ## Run go get and go mod tidy in every module for the given dependency

.PHONY: go-mod-get/%
go-mod-get/%:
ifndef DEP_VERSION
	$(error DEP_VERSION is undefined: set this to <dependency>@<version>, e.g. github.com/hashicorp/go-hclog@v1.5.0)
endif
	@echo "--> Running go get ${DEP_VERSION} ($*)"
	@cd $* && go get $(DEP_VERSION)
	@echo "--> Running go mod tidy ($*)"
	@cd $* && go mod tidy

##@ Release Targets

.PHONY: check-env
check-env: ## check env
	@printenv | grep "CONSUL_K8S"

.PHONY: prepare-release-script
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

.PHONY: prepare-release
prepare-release: prepare-release-script check-preview-containers

.PHONY: prepare-rc-script
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

.PHONY: prepare-rc-branch
prepare-rc-branch: prepare-rc-script

.PHONY: prepare-main-dev
prepare-main-dev: ## prepare main dev
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

.PHONY: prepare-release-dev
prepare-release-dev: ## prepare release dev
ifndef CONSUL_K8S_RELEASE_VERSION
	$(error CONSUL_K8S_RELEASE_VERSION is required)
endif
ifndef CONSUL_K8S_RELEASE_DATE
	$(error CONSUL_K8S_RELEASE_DATE is required, use format <Month> <Day>, <Year> (ex. October 4, 2022))
endif
ifndef CONSUL_K8S_NEXT_RELEASE_VERSION
	$(error CONSUL_K8S_NEXT_RELEASE_VERSION is required)
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
SHELL = bash
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
DEV_IMAGE?=consul-k8s-control-plane-dev
DOCKER_HUB_USER=$(shell cat $(HOME)/.dockerhub)
GIT_COMMIT?=$(shell git rev-parse --short HEAD)
GIT_DIRTY?=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
GIT_DESCRIBE?=$(shell git describe --tags --always)
CRD_OPTIONS ?= "crd:ignoreUnexportedFields=true,allowDangerousTypes=true"

##@ Help

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php
.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
