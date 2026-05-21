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
STATUSBAR_SRC := statusbar
STATUSBAR_BUILD_DIR := $(STATUSBAR_SRC)/.build/release
STATUSBAR_BIN := iogridd-statusbar
LAUNCHAGENTS_SRC := launchagents
# CI passes ARCH=arm64 or ARCH=amd64; SwiftPM expects "arm64" / "x86_64".
ifeq ($(ARCH),amd64)
SWIFT_ARCH := x86_64
else
SWIFT_ARCH := $(ARCH)
endif

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

.PHONY: all build app statusbar pkg product sign notarize clean

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
	# Status-bar LaunchAgent template (issue #388). The postinstall
	# script re-templates this into the installing user's
	# ~/Library/LaunchAgents/io.iogrid.statusbar.plist and bootstraps
	# it into the user's GUI domain on install.
	cp $(LAUNCHAGENTS_SRC)/io.iogrid.statusbar.plist $(ROOT)/Library/LaunchAgents/io.iogrid.statusbar.plist
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
	# Build the SwiftPM status-bar binary against the just-fetched
	# Sparkle.framework and stage it inside the same bundle. Issue #388.
	$(MAKE) statusbar SPARKLE_FRAMEWORK_DIR=$(CURDIR)/$(APP_BUNDLE_STAGED)/Contents/Frameworks
	cp $(STATUSBAR_BUILD_DIR)/$(STATUSBAR_BIN) $(APP_BUNDLE_STAGED)/Contents/MacOS/$(STATUSBAR_BIN)
	chmod 755 $(APP_BUNDLE_STAGED)/Contents/MacOS/$(STATUSBAR_BIN)
	# Drop the .app under /Applications/iogridd.app inside the pkg root so
	# pkgbuild lays it down at the standard location.
	mv $(APP_BUNDLE_STAGED) $(ROOT)/Applications/iogridd.app
	@echo "[macos-pkg] iogridd.app staged at $(ROOT)/Applications/iogridd.app"

# Build the Swift status-bar binary (issue #388). Requires:
#   * Xcode CLI tools / `swift` 5.9+ on PATH (macOS host only — the
#     SwiftPM target doesn't cross-compile from Linux).
#   * Sparkle.framework already fetched into SPARKLE_FRAMEWORK_DIR (the
#     `app` target above arranges that before calling us).
#
# SwiftPM's binaryTarget requires .xcframework, so we wrap the plain
# .framework into a single-platform .xcframework wrapper at build time.
# The wrapper is just a directory + Info.plist; `xcodebuild -create-xcframework`
# does it, OR we hand-write the minimal Info.plist. We use the
# hand-rolled path to avoid the xcodebuild dep on lean CI runners.
statusbar:
	@if [ -z "$(SPARKLE_FRAMEWORK_DIR)" ]; then \
		echo "[macos-pkg] ERROR: statusbar requires SPARKLE_FRAMEWORK_DIR=<path-to-dir-containing-Sparkle.framework>"; \
		exit 2; \
	fi
	@if [ ! -d "$(SPARKLE_FRAMEWORK_DIR)/Sparkle.framework" ]; then \
		echo "[macos-pkg] ERROR: $(SPARKLE_FRAMEWORK_DIR)/Sparkle.framework not found; run `make app` first"; \
		exit 2; \
	fi
	@echo "[macos-pkg] wrapping Sparkle.framework as Sparkle.xcframework for SwiftPM"
	rm -rf $(STATUSBAR_SRC)/Sparkle.xcframework
	mkdir -p $(STATUSBAR_SRC)/Sparkle.xcframework/macos-arm64_x86_64
	cp -R $(SPARKLE_FRAMEWORK_DIR)/Sparkle.framework $(STATUSBAR_SRC)/Sparkle.xcframework/macos-arm64_x86_64/
	printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0">' \
		'<dict>' \
		'  <key>AvailableLibraries</key>' \
		'  <array>' \
		'    <dict>' \
		'      <key>LibraryIdentifier</key><string>macos-arm64_x86_64</string>' \
		'      <key>LibraryPath</key><string>Sparkle.framework</string>' \
		'      <key>SupportedArchitectures</key><array><string>arm64</string><string>x86_64</string></array>' \
		'      <key>SupportedPlatform</key><string>macos</string>' \
		'    </dict>' \
		'  </array>' \
		'  <key>CFBundlePackageType</key><string>XFWK</string>' \
		'  <key>XCFrameworkFormatVersion</key><string>1.0</string>' \
		'</dict>' \
		'</plist>' \
		> $(STATUSBAR_SRC)/Sparkle.xcframework/Info.plist
	@echo "[macos-pkg] swift build (release, arch=$(SWIFT_ARCH))"
	cd $(STATUSBAR_SRC) && swift build \
		-c release \
		--arch $(SWIFT_ARCH)
	# `swift build --arch` lands the binary under .build/<arch>-apple-macosx/release/.
	# Normalise to .build/release/<bin> so the caller's cp target is stable
	# regardless of arch.
	mkdir -p $(STATUSBAR_BUILD_DIR)
	cp $(STATUSBAR_SRC)/.build/$(SWIFT_ARCH)-apple-macosx/release/$(STATUSBAR_BIN) \
		$(STATUSBAR_BUILD_DIR)/$(STATUSBAR_BIN)
	@echo "[macos-pkg] iogridd-statusbar built at $(STATUSBAR_BUILD_DIR)/$(STATUSBAR_BIN)"

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
