APP_NAME := autozeagent
GO ?= go
COMMANDS := autozeagent autozeagentd

.PHONY: format format-check vet test check all build build-cross build-platforms \
	build-windows-amd64 build-linux-amd64 systemd-check clean

format:
	$(GO) fmt ./...

format-check:
	@files="$$(find . -type f -name '*.go' \
		-not -path './.git/*' -not -path './.cache/*' -not -path './bin/*' \
		-not -path './dist/*' -not -path './.crush/*')"; \
	unformatted="$$(gofmt -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Go formatting check failed:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

check: format-check vet test systemd-check

systemd-check:
	sh ./scripts/check-systemd.sh

all: check build

build:
	mkdir -p bin
	@set -e; for command in $(COMMANDS); do \
		$(GO) build -o "bin/$$command" "./cmd/$$command"; \
	done

build-cross:
	@test -n "$(GOOS_TARGET)" || (echo 'GOOS_TARGET is required' && exit 1)
	@test -n "$(GOARCH_TARGET)" || (echo 'GOARCH_TARGET is required' && exit 1)
	mkdir -p "dist/$(GOOS_TARGET)-$(GOARCH_TARGET)"
	@set -e; for command in $(COMMANDS); do \
		CGO_ENABLED=0 GOOS=$(GOOS_TARGET) GOARCH=$(GOARCH_TARGET) $(GO) build \
			-o "dist/$(GOOS_TARGET)-$(GOARCH_TARGET)/$$command$(EXE_SUFFIX)" "./cmd/$$command"; \
	done

build-windows-amd64:
	$(MAKE) build-cross GOOS_TARGET=windows GOARCH_TARGET=amd64 EXE_SUFFIX=.exe

build-linux-amd64:
	$(MAKE) build-cross GOOS_TARGET=linux GOARCH_TARGET=amd64

build-platforms: build-windows-amd64 build-linux-amd64

clean:
	$(GO) clean -cache
