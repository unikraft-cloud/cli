// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package slices

import (
	"fmt"
	"iter"
)

// Flatten flattens a slice of slices into a single slice.
func Flatten[T any](slices [][]T) []T {
	total := 0
	for _, slice := range slices {
		total += len(slice)
	}
	result := make([]T, 0, total)
	for _, slice := range slices {
		result = append(result, slice...)
	}
	return result
}

// Collect2 collects an iter.Seq2 into two slices.
func Collect2[A any, B any](iter iter.Seq2[A, B]) (as []A, bs []B) {
	for k, v := range iter {
		as = append(as, k)
		bs = append(bs, v)
	}
	return as, bs
}

// Dedupe removes duplicate elements from a slice while preserving order.
func Dedupe[T comparable](slice []T) []T {
	seen := make(map[T]struct{})
	result := make([]T, 0, len(slice))
	for _, item := range slice {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// DedupeStringer removes duplicate elements from a slice of fmt.Stringer while
// preserving order.
func DedupeStringer[T fmt.Stringer](slice []T) []T {
	seen := make(map[string]struct{})
	result := make([]T, 0, len(slice))
	for _, item := range slice {
		str := item.String()
		if _, ok := seen[str]; !ok {
			seen[str] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
