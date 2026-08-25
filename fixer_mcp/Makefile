SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
STUDIO_CHANNELS_SCRIPT := $(ROOT_DIR)/scripts/fixer_studio_channels
STUDIO_INSTALL_SCRIPT := $(ROOT_DIR)/scripts/install_fixer_studio_channels.sh
STUDIO_PROMOTE_SCRIPT := $(ROOT_DIR)/scripts/promote_fixer_studio_stable.sh
STUDIO_VERIFY_SCRIPT := $(ROOT_DIR)/scripts/verify_fixer_studio_channels.sh
STUDIO_STATE_DIR ?= $(HOME)/Library/Application Support/Fixer Studio Channels
STUDIO_STABLE_APP_PATH ?= /Applications/Fixer Studio Stable.app
STUDIO_EXPERIMENTAL_APP_PATH ?= /Applications/Fixer Studio Experimental.app
STUDIO_LEGACY_APP_PATH ?= /Applications/Fixer Studio.app

STUDIO_CHANNEL_ARGS = --root "$(ROOT_DIR)" --state-dir "$(STUDIO_STATE_DIR)" \
	--stable-app "$(STUDIO_STABLE_APP_PATH)" \
	--experimental-app "$(STUDIO_EXPERIMENTAL_APP_PATH)" \
	--legacy-app "$(STUDIO_LEGACY_APP_PATH)"

.PHONY: build-studio-app studio-channels-bootstrap studio-experimental-refresh \
	studio-stable-promote studio-channels-verify studio-updater-uninstall

# Compatibility entrypoint: a manual build now refreshes only Experimental.
build-studio-app: studio-experimental-refresh

studio-channels-bootstrap:
	@"$(STUDIO_INSTALL_SCRIPT)" $(STUDIO_CHANNEL_ARGS)

studio-experimental-refresh:
	@"$(STUDIO_CHANNELS_SCRIPT)" refresh-experimental $(STUDIO_CHANNEL_ARGS)

studio-stable-promote:
	@"$(STUDIO_PROMOTE_SCRIPT)" $(STUDIO_CHANNEL_ARGS)

studio-channels-verify:
	@"$(STUDIO_VERIFY_SCRIPT)" $(STUDIO_CHANNEL_ARGS)

studio-updater-uninstall:
	@"$(STUDIO_CHANNELS_SCRIPT)" uninstall-updater $(STUDIO_CHANNEL_ARGS)
