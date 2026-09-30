# wimy — build helpers. Linux: `go build ./cmd/...` or the e2e scripts;
# macOS: `make mac-install` builds, signs and installs Wimy.app, and
# restarts a running wimy in place (keeping its state).

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

# macOS app bundle
APP      := bin/Wimy.app
BUNDLE_ID := io.github.jaym.wimy
SIGN_ID  ?= wimy-dev
INSTALL_DIR ?= $(HOME)/Applications
BINDIR   ?= $(HOME)/.local/bin

.PHONY: build test mac-app mac-install mac-signing-identity

build:
	go build -ldflags "$(LDFLAGS)" -o bin/wimy ./cmd/wimy
	go build -ldflags "$(LDFLAGS)" -o bin/wimyctl ./cmd/wimyctl

test:
	go vet ./...
	go test ./...

# Wimy.app, signed with the stable identity $(SIGN_ID) so updates keep
# the Accessibility permission (create it once: make mac-signing-identity).
# Without it the bundle is signed ad hoc and every changed build must be
# re-approved in Privacy & Security.
mac-app: build
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources $(APP)/Contents/Library/LaunchAgents
	sed 's/@VERSION@/$(VERSION)/g' contrib/macos/Wimy.app/Contents/Info.plist > $(APP)/Contents/Info.plist
	cp contrib/macos/Wimy.app/Contents/Library/LaunchAgents/$(BUNDLE_ID).plist $(APP)/Contents/Library/LaunchAgents/
	cp bin/wimy bin/wimyctl $(APP)/Contents/MacOS/
	@if security find-identity -v -p codesigning | grep -q '"$(SIGN_ID)"'; then \
		id='$(SIGN_ID)'; \
	else \
		echo "warning: no code-signing identity \"$(SIGN_ID)\": signing ad hoc, so this build needs"; \
		echo "         the Accessibility permission approved again (run: make mac-signing-identity)"; \
		id=-; \
	fi; \
	codesign --force --timestamp=none --sign "$$id" --identifier $(BUNDLE_ID).wimyctl $(APP)/Contents/MacOS/wimyctl && \
	codesign --force --timestamp=none --sign "$$id" --identifier $(BUNDLE_ID) $(APP)
	codesign --verify --strict $(APP)
	@echo "built $(APP) ($(VERSION))"

# Install into $(INSTALL_DIR), link wimyctl into $(BINDIR), and restart a
# running wimy in place with the new version (or start it).
mac-install: mac-app
	mkdir -p $(INSTALL_DIR) $(BINDIR)
	rsync -a --delete $(APP)/ $(INSTALL_DIR)/Wimy.app/
	ln -sf $(INSTALL_DIR)/Wimy.app/Contents/MacOS/wimyctl $(BINDIR)/wimyctl
	@if $(BINDIR)/wimyctl version >/dev/null 2>&1; then \
		$(BINDIR)/wimyctl restart; \
	else \
		open $(INSTALL_DIR)/Wimy.app && echo "started $(INSTALL_DIR)/Wimy.app"; \
	fi

mac-signing-identity:
	contrib/macos/make-signing-identity.sh $(SIGN_ID)
