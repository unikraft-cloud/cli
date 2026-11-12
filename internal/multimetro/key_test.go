// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/model"
)

const testUUID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

func TestParseKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Key
	}{
		{
			name:     "metro with uuid",
			input:    "ams/" + testUUID,
			expected: Key{Metro: "ams", UUID: testUUID, Display: "ams/" + testUUID},
		},
		{
			name:     "metro with explicit uuid",
			input:    "ams/uuid:" + testUUID,
			expected: Key{Metro: "ams", UUID: testUUID, Display: "ams/uuid:" + testUUID},
		},
		{
			name:     "metro with name",
			input:    "ams/api-key",
			expected: Key{Metro: "ams", Name: "api-key", Display: "ams/api-key"},
		},
		{
			name:     "metro with explicit name",
			input:    "ams/name:api-key",
			expected: Key{Metro: "ams", Name: "api-key", Display: "ams/name:api-key"},
		},
		{
			name:     "uuid only",
			input:    testUUID,
			expected: Key{UUID: testUUID, Display: testUUID},
		},
		{
			name:     "explicit uuid only",
			input:    "uuid:" + testUUID,
			expected: Key{UUID: testUUID, Display: "uuid:" + testUUID},
		},
		{
			name:     "name only",
			input:    "api-key",
			expected: Key{Name: "api-key", Display: "api-key"},
		},
		{
			name:     "explicit name only",
			input:    "name:api-key",
			expected: Key{Name: "api-key", Display: "name:api-key"},
		},
		{
			name:     "name is uuid",
			input:    "name:" + testUUID,
			expected: Key{Name: testUUID, Display: "name:" + testUUID},
		},
		{
			name:     "explicit uuid non-uuid",
			input:    "uuid:not-a-uuid",
			expected: Key{UUID: "not-a-uuid", Display: "uuid:not-a-uuid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseKey(tt.input))
		})
	}
}

func TestKeyString(t *testing.T) {
	tests := []struct {
		name     string
		key      Key
		expected string
	}{
		{
			name:     "uuid output",
			key:      Key{UUID: testUUID},
			expected: testUUID,
		},
		{
			name:     "uuid output with metro",
			key:      Key{Metro: "ams", UUID: testUUID},
			expected: "ams/" + testUUID,
		},
		{
			name:     "uuid needs prefix for non-uuid",
			key:      Key{UUID: "not-a-uuid"},
			expected: "uuid:not-a-uuid",
		},
		{
			name:     "uuid needs prefix for uuid prefix",
			key:      Key{UUID: "uuid:foo"},
			expected: "uuid:uuid:foo",
		},
		{
			name:     "name output",
			key:      Key{Name: "api-key"},
			expected: "api-key",
		},
		{
			name:     "name output with metro",
			key:      Key{Metro: "ams", Name: "api-key"},
			expected: "ams/api-key",
		},
		{
			name:     "name needs prefix for uuid",
			key:      Key{Name: testUUID},
			expected: "name:" + testUUID,
		},
		{
			name:     "name needs prefix for name prefix",
			key:      Key{Name: "name:foo"},
			expected: "name:name:foo",
		},
		{
			name:     "name needs prefix for uuid prefix",
			key:      Key{Name: "uuid:foo"},
			expected: "name:uuid:foo",
		},
		{
			name:     "metro name needs prefix",
			key:      Key{Metro: "ams", Name: testUUID},
			expected: "ams/name:" + testUUID,
		},
		{
			name:     "metro uuid non-uuid",
			key:      Key{Metro: "ams", UUID: "not-a-uuid"},
			expected: "ams/uuid:not-a-uuid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.key.String())
		})
	}
}

func TestParseKeys(t *testing.T) {
	inputs := []string{
		"ams/" + testUUID,
		"lhr/name:api-key",
		"uuid:not-a-uuid",
	}
	expected := Keys{
		{Metro: "ams", UUID: testUUID, Display: "ams/" + testUUID},
		{Metro: "lhr", Name: "api-key", Display: "lhr/name:api-key"},
		{UUID: "not-a-uuid", Display: "uuid:not-a-uuid"},
	}

	assert.Equal(t, expected, ParseKeys(inputs))
}

func TestCompleteKey(t *testing.T) {
	key := Key{Metro: "ams", Name: "api-key", UUID: testUUID}

	tests := []struct {
		name     string
		prefix   string
		expected []string
	}{
		{
			name:     "empty prefix",
			prefix:   "",
			expected: []string{"api-key"},
		},
		{
			name:     "non-matching prefix",
			prefix:   "non-match",
			expected: nil,
		},

		{
			name:     "matching metro prefix",
			prefix:   "am",
			expected: []string{"ams/api-key"},
		},
		{
			name:     "matching double",
			prefix:   "a",
			expected: []string{"api-key", "ams/api-key"},
		},

		{
			name:     "matching metro",
			prefix:   "ams/",
			expected: []string{"ams/api-key"},
		},
		{
			name:     "non-matching metro",
			prefix:   "lhr/",
			expected: nil,
		},

		{
			name:     "matching name",
			prefix:   "api",
			expected: []string{"api-key"},
		},
		{
			name:     "matching metro and name",
			prefix:   "ams/api",
			expected: []string{"ams/api-key"},
		},
		{
			name:     "matching uuid",
			prefix:   testUUID[:8],
			expected: []string{testUUID},
		},
		{
			name:     "matching metro and uuid",
			prefix:   "ams/" + testUUID[:8],
			expected: []string{"ams/" + testUUID},
		},

		{
			name:     "matching explicit name",
			prefix:   "name:api",
			expected: []string{"name:api-key"},
		},
		{
			name:     "matching explicit name clean",
			prefix:   "name:",
			expected: []string{"name:api-key"},
		},
		{
			name:     "matching explicit name unclear",
			prefix:   "name",
			expected: nil, // don't complete to name:..., that's a bit weird
		},
		{
			name:     "matching explicit uuid",
			prefix:   "uuid:" + testUUID[:8],
			expected: []string{"uuid:" + testUUID},
		},
		{
			name:     "matching explicit uuid clean",
			prefix:   "uuid:",
			expected: []string{"uuid:" + testUUID},
		},
		{
			name:     "matching explicit uuid unclear",
			prefix:   "uuid",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, key.Complete(tt.prefix))
		})
	}
}

func TestNotFound(t *testing.T) {
	sdkErr := group.ErrRefNotFound{Refs: group.Refs{{Metro: "fra", Name: "foo"}}}
	err := NotFound(sdkErr)

	notFound, ok := errors.AsType[model.ErrNotFound](err)
	require.True(t, ok)
	require.Len(t, notFound.Keys, 1)
	assert.Equal(t, "fra/foo", notFound.Keys[0].String())
	assert.Equal(t, sdkErr.Error(), err.Error())

	_, ok = errors.AsType[group.ErrRefNotFound](err)
	assert.True(t, ok)
}
