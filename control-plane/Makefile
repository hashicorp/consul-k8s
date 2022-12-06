SHELL = bash

################
# CI Variables #
################
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

CI_DEV_DOCKER_NAMESPACE?=hashicorpdev
CI_DEV_DOCKER_IMAGE_NAME?=consul-k8s-control-plane
CI_DEV_DOCKER_WORKDIR?=.
CONSUL_K8S_IMAGE_VERSION?=latest
################

# TODO: Remove this ci.dev-docker target once we move the acceptance tests to Github Actions.
# In CircleCI, the linux binary will be attached from a previous step at pkg/bin/linux_amd64/. This make target
# should only run in CI and not locally.
ci.dev-docker:
	@echo "Building consul-k8s Development container - $(CI_DEV_DOCKER_IMAGE_NAME)"
	@echo $(DOCKER_PASS) | docker login -u="$(DOCKER_USER)" --password-stdin
	@docker buildx build -t '$(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT)' \
	--platform linux/amd64,linux/arm64 \
	--target=dev \
	--push \
	--label COMMIT_SHA=$(CIRCLE_SHA1) \
	--label PULL_REQUEST=$(CIRCLE_PULL_REQUEST) \
	--label CIRCLE_BUILD_URL=$(CIRCLE_BUILD_URL) \
	$(CI_DEV_DOCKER_WORKDIR) -f $(CURDIR)/Dockerfile
	@echo "Pushed dev image to: $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):$(GIT_COMMIT)"
ifeq ($(CIRCLE_BRANCH), main)
	@docker buildx build -t '$(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):latest' \
	--platform linux/amd64,linux/arm64 \
	--target=dev \
	--push \
	--label COMMIT_SHA=$(CIRCLE_SHA1) \
	--label PULL_REQUEST=$(CIRCLE_PULL_REQUEST) \
	--label CIRCLE_BUILD_URL=$(CIRCLE_BUILD_URL) \
	$(CI_DEV_DOCKER_WORKDIR) -f $(CURDIR)/Dockerfile
	@echo "Pushed dev image to: $(CI_DEV_DOCKER_NAMESPACE)/$(CI_DEV_DOCKER_IMAGE_NAME):latest"
endif

.PHONY: ci.dev-tree ci.dev-docker ci.dev-docker-github
