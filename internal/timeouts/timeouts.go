// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package timeouts

import (
	"context"
	"reflect"
	"strings"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
)

// Tolerate reports whether a call failed only because its wait ran out. If it
// did, it warns with msg and returns no error, because the operation goes on.
// Every other error is returned as it is.
func Tolerate[T any](ctx context.Context, resp *platform.Response[T], err error, msg string) (bool, error) {
	if err == nil || resp == nil || resp.Data == nil {
		return false, err
	}
	// The platform puts this text at the top level of a response when a wait
	// ran out.
	if !platform.ErrorContains(err, platform.APIHTTPErrorTimedOut) &&
		(resp.Message != "Operation timed out" ||
			!platform.ErrorContainsOnly(err, platform.APIHTTPErrorUnknownError)) {
		return false, err
	}
	log.G(ctx).Warn().Str("reason", resp.Message).Msg(msg)
	return true, nil
}

// TryWithFallback runs a request and, when the metro rejects it for not knowing
// the timeout_s field, asks for the same wait through the deprecated field and
// runs it once more.
//
// HACK: only metros that predate timeout_s need this, so it can go once they
// have all been bumped.
func TryWithFallback[Req, Resp any](
	ctx context.Context,
	req Req,
	call func(context.Context, Req) (*platform.Response[Resp], error),
) (*platform.Response[Resp], error) {
	resp, err := call(ctx, req)
	if err == nil ||
		!strings.Contains(err.Error(), "timeout_s") ||
		!strings.Contains(err.Error(), "is not a valid member") {
		return resp, err
	}
	if !downgrade(reflect.ValueOf(&req).Elem()) {
		return resp, err
	}
	return call(ctx, req)
}

// downgrade rewrites a request in place to ask for its wait through the
// deprecated wait field, reporting whether it found anything to rewrite.
//
// HACK: only metros that predate timeout_s need this, so it can go once they
// have all been bumped.
func downgrade(v reflect.Value) bool {
	if v.Kind() != reflect.Slice {
		return downgradeItem(v)
	}
	downgraded := false
	for i := range v.Len() {
		downgraded = downgradeItem(v.Index(i)) || downgraded
	}
	return downgraded
}

// downgradeItem swaps timeout_s for the deprecated wait field on a single
// request item.
//
// HACK: only metros that predate timeout_s need this, so it can go once they
// have all been bumped.
func downgradeItem(v reflect.Value) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	timeoutS := v.FieldByName("TimeoutS")
	if !timeoutS.IsValid() {
		return false
	}
	timeoutS.SetZero()
	for _, name := range []string{"WaitTimeoutMs", "TimeoutMs"} {
		if field := v.FieldByName(name); field.IsValid() {
			field.Set(reflect.ValueOf(new(int64(-1))))
			break
		}
	}
	return true
}
