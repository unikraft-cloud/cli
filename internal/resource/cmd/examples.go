// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import "unikraft.com/x/kingkong"

type CmdType string

const (
	CmdTypeNone   CmdType = ""
	CmdTypeGet    CmdType = "get"
	CmdTypeWait   CmdType = "wait"
	CmdTypeList   CmdType = "list"
	CmdTypeCreate CmdType = "create"
	CmdTypeEdit   CmdType = "edit"
	CmdTypeDelete CmdType = "delete"
	CmdTypeAttach CmdType = "attach"
	CmdTypeDetach CmdType = "detach"
)

type ExampledResource interface {
	Examples() map[CmdType][]kingkong.Example
}
