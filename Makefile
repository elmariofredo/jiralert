GO := go

GITHUB_USERNAME=sysincz
BINARY = jiralert
DOCKER_RUN_OPTS ?= --name jira-alerter -v "/tmp/config:/config" --network host
DOCKER_RUN_ARG ?= /jiralert -v 3 -config /config/jiralert.yml
VERSION := $(shell git describe --tags 2>/dev/null)
ifeq "$(VERSION)" ""
VERSION := $(shell git rev-parse --short HEAD)
endif
COMMIT=$(shell git rev-parse --short HEAD)
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
BUILD_DATE=$(shell date +%FT%T%z)

LDFLAGS = -ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Branch=$(BRANCH) -X main.BuildDate=$(BUILD_DATE)"

RELEASE     := $(BINARY)-$(VERSION).linux-amd64
RELEASE_DIR := release/$(RELEASE)

PACKAGES           := $(shell $(GO) list ./... | grep -v /vendor/)
STATICCHECK_IGNORE :=

#all: clean format staticcheck build
all: clean format  build docker 

clean:
	@rm -rf jiralert release

format:
	@echo ">> formatting code"
	@$(GO) fmt $(PACKAGES)

staticcheck: get_staticcheck
	@echo ">> running staticcheck"
	@staticcheck -ignore "$(STATICCHECK_IGNORE)" $(PACKAGES)

build:
	@echo ">> building binaries"
	@GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) github.com/sysincz/jiralert/cmd/jiralert

tarball:
	@echo ">> packaging release $(VERSION)"
	@rm -rf "$(RELEASE_DIR)/*"
	@mkdir -p "$(RELEASE_DIR)"
	@cp $(BINARY) README.md LICENSE "$(RELEASE_DIR)"
	@mkdir -p "$(RELEASE_DIR)/config"
	@cp config/* "$(RELEASE_DIR)/config"
	@tar -zcvf "$(RELEASE).tar.gz" -C "$(RELEASE_DIR)"/.. "$(RELEASE)"
	@rm -rf "$(RELEASE_DIR)"


docker: clean  build
	docker build -t $(GITHUB_USERNAME)/$(BINARY):$(VERSION) .

test-docker-run: docker
	docker run --rm $(DOCKER_RUN_OPTS) -p 9097:9097 $(GITHUB_USERNAME)/$(BINARY):$(VERSION) $(DOCKER_RUN_ARG)

docker-push: docker
	docker push $(GITHUB_USERNAME)/$(BINARY):$(VERSION)

get_staticcheck:
	@echo ">> getting staticcheck"
	@GOOS= GOARCH= $(GO) get -u honnef.co/go/tools/cmd/staticcheck
