// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	xslices "unikraft.com/cli/internal/x/slices"
	"unikraft.com/x/joinerrgroup"
)

type AnyResourceCmd struct {
	cmd.ResourceCmd[AnyResource]
	cmd.GettableResourceCmd[AnyResource]
	cmd.ListableResourceCmd[AnyResource]

	Create cmd.ResourceCreateCmd[AnyResource] `cmd:"" help:"Create a resource."`
	Edit   cmd.ResourceEditCmd[AnyResource]   `cmd:"" help:"Edit a resource."`

	cmd.BulkDeletableResourceCmd[AnyResource]
}

var resourceBackends = []resource.Resource{
	Instance{},
	InstanceTemplate{},
	Volume{},
	VolumeTemplate{},
	ServiceGroup{},
	Certificate{},
}

func backendIndexByType(typ string) int {
	return slices.IndexFunc(resourceBackends, func(backend resource.Resource) bool {
		return backend.Type().Name == typ
	})
}

func backendByType(typ string) (resource.Resource, bool) {
	idx := backendIndexByType(typ)
	if idx < 0 {
		return nil, false
	}
	return resourceBackends[idx], true
}

func wrapAnyResource(res resource.Resource) AnyResource {
	typ := res.Type().Name
	return AnyResource{
		Type_:      typ,
		Key_:       typ + ":" + res.Key().String(),
		underlying: res,
	}
}

// AnyResource is a special resource that multiplexes to different backend
// resources based on the key prefix (e.g., "instance:", "volume:", etc.).
// When populated, resources have their full fields. When empty (header field),
// it only has "type" and "key" fields.
type AnyResource struct {
	Type_ string `field:"type,short" create:"set,required" flag:"type" help:"Resource type." placeholder:"type" example:"instance,volume,service,certificate"`
	Key_  string `field:"key,short"`

	// The actual underlying resource, if populated
	underlying resource.Resource
}

func (a AnyResource) Type() resource.Type {
	if a.underlying != nil {
		return a.underlying.Type()
	}
	return resource.Type{
		Name:  "resource",
		Names: "resources",
	}
}

type anyResourceKey struct {
	typ string
	key string
}

func (k anyResourceKey) String() string {
	if k.typ == "" {
		return k.key
	}
	return k.typ + ":" + k.key
}

func (k anyResourceKey) Canonical() string {
	return k.key
}

func (k *anyResourceKey) UnmarshalText(text []byte) error {
	typ, key, found := strings.Cut(string(text), ":")
	if found {
		k.typ = typ
		k.key = key
	} else {
		k.key = string(text)
	}
	return nil
}

// parseAnyResourceKey parses a key string into its type and underlying key
// components. Returns the type prefix, underlying key, and whether the type
// was present.
func parseAnyResourceKey(key string) (typ, underlyingKey string, hasType bool) {
	var k anyResourceKey
	_ = k.UnmarshalText([]byte(key))
	return k.typ, k.key, k.typ != ""
}

func (a AnyResource) Key() resource.Key {
	if a.underlying != nil {
		return anyResourceKey{
			typ: a.underlying.Type().Name,
			key: a.underlying.Key().String(),
		}
	}
	return anyResourceKey{
		typ: a.Type_,
		key: a.Key_,
	}
}

func (a AnyResource) Fields(ctx context.Context) ([]resource.Field, error) {
	fields, err := resource.FieldsFromStruct(a)
	if err != nil {
		return nil, err
	}
	underlying := a.underlying
	if underlying == nil && a.Type_ != "" {
		if backend, ok := backendByType(a.Type_); ok {
			underlying = backend
		}
	}
	if underlying != nil {
		underlyingFields, err := underlying.Fields(ctx)
		if err != nil {
			return nil, err
		}
		fields = append(fields, underlyingFields...)
	}
	return fields, nil
}

func (a AnyResource) WithType(typ string) resource.Resource {
	a.Type_ = typ
	return a
}

func (a AnyResource) Raw() any {
	if a.underlying != nil {
		return a.underlying.Raw()
	}
	return nil
}

func (a AnyResource) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no keys provided")
	}

	// Group keys by backend index so we can run per-backend Get calls in
	// parallel while keeping output ordering stable according to resourceBackends.
	keysByBackend := make([][]string, len(resourceBackends))
	for _, key := range keys {
		var k anyResourceKey
		if err := k.UnmarshalText([]byte(key)); err != nil {
			return nil, fmt.Errorf("invalid resource key %q: %w", key, err)
		}
		if k.typ == "" {
			return nil, fmt.Errorf("resource key %q must include resource type prefix", key)
		}

		idx := backendIndexByType(k.typ)
		if idx < 0 {
			return nil, fmt.Errorf("unknown resource type: %s", k.typ)
		}
		backend := resourceBackends[idx]
		if _, ok := backend.(resource.GettableResource); !ok {
			return nil, fmt.Errorf("resource type %s does not support Get", k.typ)
		}

		keysByBackend[idx] = append(keysByBackend[idx], k.key)
	}

	perBackend := make([][]resource.Resource, len(resourceBackends))

	eg := joinerrgroup.Group{}
	for i, keyList := range keysByBackend {
		if len(keyList) == 0 {
			continue
		}
		backend := resourceBackends[i].(resource.GettableResource)
		typ := backend.Type().Name
		eg.Go(func() error {
			resources, err := backend.Get(ctx, keyList)
			wrapped := make([]resource.Resource, 0, len(resources))
			for _, res := range resources {
				wrapped = append(wrapped, wrapAnyResource(res))
			}
			perBackend[i] = wrapped
			if err != nil {
				return fmt.Errorf("failed to get %s resources: %w", typ, err)
			}
			return nil
		})
	}
	err := eg.Wait()
	return xslices.Flatten(perBackend), err
}

func (a AnyResource) List(ctx context.Context) ([]resource.Resource, error) {
	perBackend := make([][]resource.Resource, len(resourceBackends))
	eg := joinerrgroup.Group{}
	for i, backend := range resourceBackends {
		listable, ok := backend.(resource.ListableResource)
		if !ok {
			continue
		}
		typ := backend.Type().Name
		eg.Go(func() error {
			resources, err := listable.List(ctx)
			wrapped := make([]resource.Resource, 0, len(resources))
			for _, res := range resources {
				wrapped = append(wrapped, wrapAnyResource(res))
			}
			perBackend[i] = wrapped
			if err != nil {
				return fmt.Errorf("failed to list %s resources: %w", typ, err)
			}
			return nil
		})
	}
	err := eg.Wait()
	return xslices.Flatten(perBackend), err
}

func (a AnyResource) Edit(ctx context.Context, key string, fields []resource.Field) error {
	typ, underlyingKey, hasType := parseAnyResourceKey(key)
	if !hasType {
		return fmt.Errorf("resource key %q must include resource type prefix", key)
	}

	backend, ok := backendByType(typ)
	if !ok {
		return fmt.Errorf("unknown resource type: %s", typ)
	}

	editable, ok := backend.(resource.EditableResource)
	if !ok {
		return fmt.Errorf("resource type %s does not support Edit", typ)
	}

	if err := editable.Edit(ctx, underlyingKey, fields); err != nil {
		return fmt.Errorf("failed to edit %s resource: %w", typ, err)
	}
	return nil
}

func (a AnyResource) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	typeFields := resource.GetFieldByPath(fields, []string{"type"})
	if len(typeFields) == 0 {
		return nil, fmt.Errorf("type field is required")
	}
	typ := typeFields[0].Value.(string)

	backend, ok := backendByType(typ)
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", typ)
	}

	creatable, ok := backend.(resource.CreatableResource)
	if !ok {
		return nil, fmt.Errorf("resource type %s does not support Create", typ)
	}

	results, err := creatable.Create(ctx, a.underlyingFields(fields))
	if err != nil {
		return nil, fmt.Errorf("failed to create %s resource: %w", typ, err)
	}

	wrapped := make([]resource.Resource, 0, len(results))
	for _, res := range results {
		wrapped = append(wrapped, wrapAnyResource(res))
	}

	return wrapped, nil
}

func (a AnyResource) underlyingFields(fields []resource.Field) []resource.Field {
	filtered := slices.Clone(fields)
	filtered = slices.DeleteFunc(filtered, func(field resource.Field) bool {
		return field.Name == "type" || field.Name == "key"
	})
	return filtered
}

func (a AnyResource) Delete(ctx context.Context, keys []string) error {
	keysByBackend := make([][]string, len(resourceBackends))
	for _, key := range keys {
		typ, underlyingKey, hasType := parseAnyResourceKey(key)
		if !hasType {
			return fmt.Errorf("resource key %q must include resource type prefix", key)
		}

		idx := backendIndexByType(typ)
		if idx < 0 {
			return fmt.Errorf("unknown resource type: %s", typ)
		}
		backend := resourceBackends[idx]
		if _, ok := backend.(resource.DeletableResource); !ok {
			return fmt.Errorf("resource type %s does not support Delete", typ)
		}
		keysByBackend[idx] = append(keysByBackend[idx], underlyingKey)
	}

	// Delete in backend order.
	// HACK: This is intentionally NOT parallelized because backend order matters
	// (e.g. dependencies between resource types).
	var errs []error
	for i, typeKeys := range keysByBackend {
		if len(typeKeys) == 0 {
			continue
		}
		backend := resourceBackends[i]
		typ := backend.Type().Name
		deletable := backend.(resource.DeletableResource)

		if err := deletable.Delete(ctx, typeKeys); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s resources: %w", typ, err))
		}
	}
	return errors.Join(errs...)
}
