BINARY   := terraform-provider-gravitee
VERSION  ?= 0.1.0
OS_ARCH  ?= $(shell go env GOOS)_$(shell go env GOARCH)
MIRROR   ?= $(HOME)/.terraform.d/plugins

.PHONY: build
build:
	go build -ldflags "-X main.versao=$(VERSION)" -o $(BINARY) .

.PHONY: test
test:
	go test ./... -race

.PHONY: lint
lint:
	gofmt -l .
	go vet ./...

# Installs into a local filesystem mirror, so `terraform init` resolves the
# provider offline without a dev override.
.PHONY: install
install: build
	mkdir -p $(MIRROR)/registry.terraform.io/klinux/gravitee/$(VERSION)/$(OS_ARCH)
	cp $(BINARY) $(MIRROR)/registry.terraform.io/klinux/gravitee/$(VERSION)/$(OS_ARCH)/$(BINARY)_v$(VERSION)
	@echo "installed $(VERSION) for $(OS_ARCH) in $(MIRROR)"

# Regenerates docs/ from the schemas and examples/.
.PHONY: docs
docs:
	go tool github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate \
		--provider-name gravitee --rendered-provider-name Gravitee

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf dist
