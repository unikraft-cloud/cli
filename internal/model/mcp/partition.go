// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

import (
	"errors"
)

func (ts *Tools) syncPartition(run func() error) error {
	p := ts.opts.Partition
	if p == nil {
		return run()
	}
	ts.partitionMu.Lock()
	defer ts.partitionMu.Unlock()
	if err := p.Reload(); err != nil {
		return err
	}
	return errors.Join(run(), p.Save())
}
