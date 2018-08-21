SHELL = bash

GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
GOPATH=$(shell go env GOPATH)
GOTAGS ?=

DEV_IMAGE?=consul-k8s-dev
GIT_COMMIT?=$(shell git rev-parse --short HEAD)
GIT_DIRTY?=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
GIT_DESCRIBE?=$(shell git describe --tags --always)
GIT_IMPORT=github.com/hashicorp/consul-k8s/version
GOLDFLAGS=-X $(GIT_IMPORT).GitCommit=$(GIT_COMMIT)$(GIT_DIRTY) -X $(GIT_IMPORT).GitDescribe=$(GIT_DESCRIBE)

export GIT_COMMIT
export GIT_DIRTY
export GIT_DESCRIBE
export GOLDFLAGS
export GOTAGS

all: bin

bin:
	@$(SHELL) $(CURDIR)/build-support/scripts/build-local.sh

dev:
	@$(SHELL) $(CURDIR)/build-support/scripts/build-local.sh -o $(GOOS) -a $(GOARCH)

dev-docker:
	@docker build -t '$(DEV_IMAGE)' --build-arg 'GIT_COMMIT=$(GIT_COMMIT)' --build-arg 'GIT_DIRTY=$(GIT_DIRTY)' --build-arg 'GIT_DESCRIBE=$(GIT_DESCRIBE)' -f $(CURDIR)/build-support/docker/Dev.dockerfile $(CURDIR)

test:
	go test ./...

clean:
	@rm -rf \
		$(CURDIR)/bin \
		$(CURDIR)/pkg


.PHONY: all bin clean dev test
