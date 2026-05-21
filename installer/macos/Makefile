# macOS .pkg installer for iogrid.
#
# Layout (matches Apple's productbuild recipe):
#   build/root/                # absolute paths the package writes
#     usr/local/iogrid/iogridd
#     Library/LaunchAgents/org.iogrid.daemon.plist   # template only
#   scripts/postinstall        # runs as root after extract
#   Resources/Welcome.html     # shown to user in the installer UI
#   distribution.xml           # productbuild definition
#
# Pipeline:
#   1. make build                 stages binary + scripts + plist
#   2. make pkg                   pkgbuild -> iogrid-component.pkg
#   3. make product               productbuild -> iogrid-VERSION.pkg
#   4. make sign                  productsign with APPLE_DEVELOPER_ID
#   5. make notarize              xcrun notarytool submit + staple
#   6. make all                   the lot, skipping sign/notarize when
#                                 APPLE_DEVELOPER_ID is unset
#
# Outputs:  dist/iogrid-VERSION-arch.pkg

VERSION      ?= 0.1.0
ARCH         ?= arm64
ROOT         := build/root
SCRIPTS_DIR  := build/scripts
RES_DIR      := Resources
DIST_DIR     := dist
APP_BUNDLE_SRC := app
APP_BUNDLE_STAGED := build/iogridd.app

# Apple Developer ID Installer cert from the env. When unset, sign/notarize
# steps become no-ops so CI builds without secrets still produce an
# (unsigned) .pkg artifact for testing.
APPLE_DEVELOPER_ID  ?=
APPLE_NOTARY_PROFILE ?= iogrid-notary

# Source of the daemon binary. Set this from CI to the staged build:
#   make all DAEMON_BIN=../../daemon/target/aarch64-apple-darwin/release/iogridd
DAEMON_BIN   ?= ../../daemon/target/release/iogridd

# Sparkle EdDSA public key. Embedded as SUPublicEDKey in the .app Info.plist
# at build time so each release ships its own trust anchor. Defaults to the
# committed pubkey file (sparkle/pubkey.ed25519); CI can override with
# SU_PUBLIC_ED_KEY="<base64-of-pubkey>" for ad-hoc / dev builds.
SU_PUBLIC_ED_KEY ?= $(shell tr -d '[:space:]' < sparkle/pubkey.ed25519)

PKG_ID       := org.iogrid.daemon
COMPONENT_PKG := build/iogrid-component-$(VERSION).pkg
PRODUCT_PKG   := $(DIST_DIR)/iogrid-$(VERSION)-$(ARCH).pkg
SIGNED_PKG    := $(DIST_DIR)/iogrid-$(VERSION)-$(ARCH)-signed.pkg

.PHONY: all build app pkg product sign notarize clean

all: build pkg product
	@if [ -n "$(APPLE_DEVELOPER_ID)" ]; then \
		$(MAKE) sign notarize; \
	else \
		echo "[macos-pkg] APPLE_DEVELOPER_ID empty — skipping sign + notarize (CI artifact)"; \
	fi

build:
	@echo "[macos-pkg] staging payload under $(ROOT)"
	rm -rf $(ROOT) $(SCRIPTS_DIR) $(APP_BUNDLE_STAGED)
	mkdir -p $(ROOT)/usr/local/iogrid
	mkdir -p $(ROOT)/Library/LaunchAgents
	mkdir -p $(ROOT)/Applications
	mkdir -p $(SCRIPTS_DIR)
	@if [ ! -f "$(DAEMON_BIN)" ]; then \
		echo "[macos-pkg] ERROR: $(DAEMON_BIN) not found — pass DAEMON_BIN=<path-to-iogridd>"; \
		exit 2; \
	fi
	cp $(DAEMON_BIN) $(ROOT)/usr/local/iogrid/iogridd
	chmod 755 $(ROOT)/usr/local/iogrid/iogridd
	cp templates/org.iogrid.daemon.plist.tmpl $(ROOT)/Library/LaunchAgents/org.iogrid.daemon.plist
	cp scripts/postinstall $(SCRIPTS_DIR)/postinstall
	chmod 755 $(SCRIPTS_DIR)/postinstall
	mkdir -p $(DIST_DIR)
	@echo "[macos-pkg] payload ready"
	$(MAKE) app

# Build the iogridd.app bundle that hosts Sparkle.framework. Sparkle 2's
# update path needs a real .app Info.plist (with SUFeedURL + SUPublicEDKey)
# at the bundle root — that's what makes the framework's XPC installer
# trust the update flow. The bundle's CFBundleExecutable is a copy of the
# same iogridd binary that's also dropped at /usr/local/iogrid/iogridd; the
# LaunchAgent keeps targeting the latter for backwards compat. Future PRs
# can switch the LaunchAgent to launch the .app directly once the status-
# bar menu lands.
app:
	@echo "[macos-pkg] building iogridd.app bundle"
	rm -rf $(APP_BUNDLE_STAGED)
	mkdir -p $(APP_BUNDLE_STAGED)/Contents/MacOS
	mkdir -p $(APP_BUNDLE_STAGED)/Contents/Resources
	mkdir -p $(APP_BUNDLE_STAGED)/Contents/Frameworks
	cp $(APP_BUNDLE_SRC)/Contents/PkgInfo $(APP_BUNDLE_STAGED)/Contents/PkgInfo
	# Substitute SUPublicEDKey + CFBundleShortVersionString + CFBundleVersion
	# into the Info.plist template. Using `sed` (not PlistBuddy) so the
	# Linux lint job in installer-ci can dry-run the same substitution.
	sed \
		-e "s|__SU_PUBLIC_ED_KEY__|$(SU_PUBLIC_ED_KEY)|g" \
		-e "s|<string>0.1.0</string>|<string>$(VERSION)</string>|g" \
		$(APP_BUNDLE_SRC)/Contents/Info.plist > $(APP_BUNDLE_STAGED)/Contents/Info.plist
	cp $(DAEMON_BIN) $(APP_BUNDLE_STAGED)/Contents/MacOS/iogridd
	chmod 755 $(APP_BUNDLE_STAGED)/Contents/MacOS/iogridd
	# Fetch + stage Sparkle.framework. SPARKLE_TARBALL_SHA256 should be set
	# in CI so the fetch verifies; local-dev runs see a WARN and continue.
	./sparkle/fetch-sparkle.sh $(APP_BUNDLE_STAGED)/Contents/Frameworks
	# Drop the .app under /Applications/iogridd.app inside the pkg root so
	# pkgbuild lays it down at the standard location.
	mv $(APP_BUNDLE_STAGED) $(ROOT)/Applications/iogridd.app
	@echo "[macos-pkg] iogridd.app staged at $(ROOT)/Applications/iogridd.app"

pkg: build
	@echo "[macos-pkg] building component package -> $(COMPONENT_PKG)"
	pkgbuild \
		--root $(ROOT) \
		--scripts $(SCRIPTS_DIR) \
		--identifier $(PKG_ID) \
		--version $(VERSION) \
		--install-location / \
		$(COMPONENT_PKG)

product: pkg
	@echo "[macos-pkg] building product archive -> $(PRODUCT_PKG)"
	productbuild \
		--distribution distribution.xml \
		--resources $(RES_DIR) \
		--package-path build \
		--version $(VERSION) \
		$(PRODUCT_PKG)

sign: product
	@if [ -z "$(APPLE_DEVELOPER_ID)" ]; then \
		echo "[macos-pkg] APPLE_DEVELOPER_ID unset — skip sign"; \
		exit 0; \
	fi
	@echo "[macos-pkg] signing -> $(SIGNED_PKG)"
	productsign --sign "$(APPLE_DEVELOPER_ID)" $(PRODUCT_PKG) $(SIGNED_PKG)
	mv $(SIGNED_PKG) $(PRODUCT_PKG)
	@echo "[macos-pkg] signed pkg at $(PRODUCT_PKG)"

notarize: sign
	@if [ -z "$(APPLE_DEVELOPER_ID)" ]; then \
		echo "[macos-pkg] APPLE_DEVELOPER_ID unset — skip notarize"; \
		exit 0; \
	fi
	@echo "[macos-pkg] submitting to notarytool"
	xcrun notarytool submit $(PRODUCT_PKG) \
		--keychain-profile "$(APPLE_NOTARY_PROFILE)" \
		--wait
	xcrun stapler staple $(PRODUCT_PKG)
	@echo "[macos-pkg] notarized + stapled: $(PRODUCT_PKG)"

clean:
	rm -rf build dist
