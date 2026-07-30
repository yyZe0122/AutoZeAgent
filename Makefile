APP_NAME := autozeagent
# Prefer PATH go; fall back when make inherits a minimal PATH.
GO ?= $(shell if command -v go >/dev/null 2>&1; then command -v go; elif test -x /usr/local/go/bin/go; then echo /usr/local/go/bin/go; else echo go; fi)
COMMANDS := autozeagent autozeagentd
# User-local install (no root). Override: make install PREFIX=/usr/local
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: format format-check vet test check all build build-cross build-platforms \
	build-windows-amd64 build-linux-amd64 install uninstall systemd-check clean

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
	ln -sfn autozeagent bin/aze

build-cross:
	@test -n "$(GOOS_TARGET)" || (echo 'GOOS_TARGET is required' && exit 1)
	@test -n "$(GOARCH_TARGET)" || (echo 'GOARCH_TARGET is required' && exit 1)
	mkdir -p "dist/$(GOOS_TARGET)-$(GOARCH_TARGET)"
	@set -e; for command in $(COMMANDS); do \
		CGO_ENABLED=0 GOOS=$(GOOS_TARGET) GOARCH=$(GOARCH_TARGET) $(GO) build \
			-o "dist/$(GOOS_TARGET)-$(GOARCH_TARGET)/$$command$(EXE_SUFFIX)" "./cmd/$$command"; \
	done
	@set -e; \
	cli="dist/$(GOOS_TARGET)-$(GOARCH_TARGET)/autozeagent$(EXE_SUFFIX)"; \
	alias="dist/$(GOOS_TARGET)-$(GOARCH_TARGET)/aze$(EXE_SUFFIX)"; \
	if [ "$(GOOS_TARGET)" = "windows" ]; then \
		cp "$$cli" "$$alias"; \
	else \
		ln -sfn "autozeagent$(EXE_SUFFIX)" "$$alias"; \
	fi

build-windows-amd64:
	$(MAKE) build-cross GOOS_TARGET=windows GOARCH_TARGET=amd64 EXE_SUFFIX=.exe

build-linux-amd64:
	$(MAKE) build-cross GOOS_TARGET=linux GOARCH_TARGET=amd64

build-platforms: build-windows-amd64 build-linux-amd64

# Install CLI + TUI alias + daemon into BINDIR (default: ~/.local/bin).
install: build
	mkdir -p "$(BINDIR)"
	install -m 0755 bin/autozeagent "$(BINDIR)/autozeagent"
	install -m 0755 bin/autozeagentd "$(BINDIR)/autozeagentd"
	ln -sfn autozeagent "$(BINDIR)/aze"
	@echo "Installed to $(BINDIR): autozeagent aze autozeagentd"
	@case ":$$PATH:" in \
		*":$(BINDIR):"*) ;; \
		*) echo "Add to PATH: export PATH=\"$(BINDIR):$$PATH\"" ;; \
	esac

uninstall:
	rm -f "$(BINDIR)/autozeagent" "$(BINDIR)/autozeagentd" "$(BINDIR)/aze"
	@echo "Removed from $(BINDIR): autozeagent aze autozeagentd"

clean:
	$(GO) clean -cache
