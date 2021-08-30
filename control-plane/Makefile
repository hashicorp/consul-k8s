SHELL = bash

GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
DEV_IMAGE?=consul-k8s-control-plane-dev
GIT_COMMIT?=$(shell git rev-parse --short HEAD)
GIT_DIRTY?=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
GIT_DESCRIBE?=$(shell git describe --tags --always)

DEV_PUSH?=0
ifeq ($(DEV_PUSH),1)
DEV_PUSH_ARG=
else
DEV_PUSH_ARG=--no-push
endif

export GIT_COMMIT
export GIT_DIRTY
export GIT_DESCRIBE

CRD_OPTIONS ?= "crd:trivialVersions=true,allowDangerousTypes=true"

################
# CI Variables #
################
CI_DEV_DOCKER_NAMESPACE?=hashicorpdev
CI_DEV_DOCKER_IMAGE_NAME?=consul-k8s-control-plane
CI_DEV_DOCKER_WORKDIR?=.
CONSUL_K8S_IMAGE_VERSION?=latest
################

dev:
	@$(SHELL) $(CURDIR)/build-support/scripts/build-local.sh -o $(GOOS) -a $(GOARCH)

dev-docker:
	@$(SHELL) $(CURDIR)/build-support/scripts/build-local.sh -o linux -a amd64
	@docker build -t '$(DEV_IMAGE)' --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' -f $(CURDIR)/build-support/docker/Dev.dockerfile $(CURDIR)

dev-tree:
	@$(SHELL) $(CURDIR)/build-support/scripts/dev.sh $(DEV_PUSH_ARG)

test:
	go test ./...

# requires a consul enterprise binary on the path
ent-test:
	go test ./... -tags=enterprise

cov:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

clean:
	@rm -rf \
		$(CURDIR)/bin \
		$(CURDIR)/pkg

# Generate manifests e.g. CRD, RBAC etc.
ctrl-manifests: get-controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Generate code
ctrl-generate: get-controller-gen
	$(CONTROLLER_GEN) object:headerFile="build-support/controller/boilerplate.go.txt" paths="./..."

# find or download controller-gen
# download controller-gen if necessary
get-controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

get-kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# In CircleCI, the linux binary will be attached from a previous step at pkg/bin/linux_amd64/. This make target
# should only run in CI and not locally.
ci.dev-docker:
	@echo "Pulling consul-k8s container image - $(CONSUL_K8S_IMAGE_VERSION)"
	@docker pull hashicorp/consul-k8s:$(CONSUL_K8S_IMAGE_VERSION) >/dev/null #todo change this back after pulling it the first time since the dockerfile is FROM this image
	@echo "Building consul-k8s Development container - $(CI_DEV_DOCKER_IMAGE_NAME)"
	@docker build -t '$(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT)' \
	--build-arg CONSUL_K8S_IMAGE_VERSION=$(CONSUL_K8S_IMAGE_VERSION) \
	--label COMMIT_SHA=$(CIRCLE_SHA1) \
	--label PULL_REQUEST=$(CIRCLE_PULL_REQUEST) \
	--label CIRCLE_BUILD_URL=$(CIRCLE_BUILD_URL) \
	$(CI_DEV_DOCKER_WORKDIR) -f $(CURDIR)/build-support/docker/Dev.dockerfile
	@echo $(DOCKER_PASS) | docker login -u="$(DOCKER_USER)" --password-stdin
	@echo "Pushing dev image to: https://cloud.docker.com/u/$(CI_DEV_DOCKER_NAMESPACE)/repository/docker/$(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME)"
	@docker push $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT)
ifeq ($(CIRCLE_BRANCH), main)
	@docker tag $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT) $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):latest
	@docker push $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):latest
endif
ifeq ($(CIRCLE_BRANCH), crd-controller-base)
	@docker tag $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT) $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):crd-controller-base-latest
	@docker push $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):crd-controller-base-latest
endif
ifeq ($(CIRCLE_BRANCH), monorepo)
	@docker tag $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT) $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):monorepo
	@docker push $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):monorepo
endif

.PHONY: all bin clean dev dist docker-images go-build-image test tools ci.dev-docker
