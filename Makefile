# SPDX-License-Identifier: BSD-3-Clause
# Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
# Licensed under the BSD-3-Clause License (the "License").
# You may not use this file except in compliance with the License.

# Prelude
SHELL         := bash
.DELETE_ON_ERROR:
.SHELLFLAGS   := -eu -o pipefail -c
.DEFAULT_GOAL := all
Q             ?= @
GO            ?= go

ifeq ($(shell command -v task >/dev/null 2>&1 && echo yes),)
TASK          ?= $(GO) run -v github.com/go-task/task/v3/cmd/task@v3.54.0 --yes
else
TASK          ?= task --yes
endif
ifeq ($(Q),)
TASK          := $(TASK) --verbose
endif

.DEFAULT_GOAL := help
.PHONY: help

help:
	$(Q)$(TASK) -l

%:
	$(Q)$(TASK) $(MAKECMDGOALS)
