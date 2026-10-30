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

	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/joinerrgroup"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	wslices "unikraft.com/cli/internal/w/slices"
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
		return backend.Type().Name == typ || backend.Type().Names == typ
	})
}

func backendByType(typ string) (resource.Resource, bool) {
	idx := backendIndexByType(typ)
	if idx < 0 {
		return nil, false
	}
	return resourceBackends[idx], true
}

func wrapAnyResources(resources []resource.Resource) []resource.Resource {
	wrapped := make([]resource.Resource, 0, len(resources))
	for _, res := range resources {
		wrapped = append(wrapped, wrapAnyResource(res))
	}
	return wrapped
}

func wrapAnyResource(res resource.Resource) AnyResource {
	typ := res.Type().Name
	return AnyResource{
		Kind:       typ,
		Key_:       res.Key().String(),
		underlying: res,
	}
}

// AnyResource is a special resource that multiplexes to different backend
// resources based on the key prefix (e.g., "instance/", "volume/", etc.).
// When populated, resources have their full fields. When empty (header field),
// it only has "kind" and "key" fields.
type AnyResource struct {
	Kind string `field:"kind,short" create:"set,required" flag:"kind" help:"Resource kind." placeholder:"kind" example:"instance,volume,service,certificate"`
	Key_ string `field:"key,short"`

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
	return k.typ + "/" + k.key
}

func (k anyResourceKey) Canonical() string {
	return k.key
}

// parseAnyResourceKey splits a key into its kind and underlying key. The kind
// is empty if the key isn't prefixed with a known kind.
func parseAnyResourceKey(key string) (typ, underlyingKey string) {
	if typ, rest, ok := strings.Cut(key, "/"); ok && backendIndexByType(typ) >= 0 {
		return typ, rest
	}
	return "", key
}

func (a AnyResource) Key() resource.Key {
	if a.underlying != nil {
		return anyResourceKey{
			typ: a.underlying.Type().Name,
			key: a.underlying.Key().String(),
		}
	}
	return anyResourceKey{
		typ: a.Kind,
		key: a.Key_,
	}
}

func (a AnyResource) Fields(ctx context.Context) ([]resource.Field, error) {
	fields, err := resource.FieldsFromStruct(a)
	if err != nil {
		return nil, err
	}
	underlying := a.underlying
	if underlying == nil && a.Kind != "" {
		if backend, ok := backendByType(a.Kind); ok {
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

func (a AnyResource) WithKind(kind string) resource.Resource {
	a.Kind = kind
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
	var bareKeys []string
	for _, key := range keys {
		typ, underlyingKey := parseAnyResourceKey(key)
		if typ == "" {
			bareKeys = append(bareKeys, key)
			continue
		}

		idx := backendIndexByType(typ)
		if _, ok := resourceBackends[idx].(resource.GettableResource); !ok {
			return nil, fmt.Errorf("resource type %s does not support Get", typ)
		}
		keysByBackend[idx] = append(keysByBackend[idx], underlyingKey)
	}

	perBackend := make([][]resource.Resource, len(resourceBackends))
	perBackendBare := make([][]resource.Resource, len(resourceBackends))
	bareNotFound := make([]group.Refs, len(resourceBackends))
	searched := 0

	eg := joinerrgroup.Group{}
	for i, backend := range resourceBackends {
		gettable, ok := backend.(resource.GettableResource)
		if !ok {
			continue
		}
		typ := backend.Type().Name
		if keyList := keysByBackend[i]; len(keyList) > 0 {
			eg.Go(func() error {
				resources, err := gettable.Get(ctx, keyList)
				perBackend[i] = wrapAnyResources(resources)
				if err != nil {
					return fmt.Errorf("failed to get %s resources: %w", typ, err)
				}
				return nil
			})
		}
		if len(bareKeys) > 0 {
			searched++
			eg.Go(func() error {
				resources, err := gettable.Get(ctx, bareKeys)
				perBackendBare[i] = wrapAnyResources(resources)
				bareNotFound[i], err = splitRefNotFound(err)
				if err != nil {
					return fmt.Errorf("failed to get %s resources: %w", typ, err)
				}
				return nil
			})
		}
	}
	err := eg.Wait()

	notFoundCount := map[group.Ref]int{}
	for _, refs := range bareNotFound {
		for _, ref := range refs {
			notFoundCount[ref]++
		}
	}
	var missing group.Refs
	for _, key := range bareKeys {
		ref := multimetro.ParseKey(key).Ref()
		ref.Display = ""
		if notFoundCount[ref] == searched {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		err = errors.Join(err, group.ErrRefNotFound{Refs: missing})
	}

	var results []resource.Resource
	for i := range resourceBackends {
		results = append(results, perBackend[i]...)
		results = append(results, perBackendBare[i]...)
	}
	return results, err
}

func splitRefNotFound(err error) (group.Refs, error) {
	if err == nil {
		return nil, nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var refs group.Refs
		var errs []error
		for _, err := range joined.Unwrap() {
			r, err := splitRefNotFound(err)
			refs = append(refs, r...)
			errs = append(errs, err)
		}
		return refs, errors.Join(errs...)
	}
	if notFound, ok := errors.AsType[group.ErrRefNotFound](err); ok {
		return notFound.Refs, nil
	}
	return nil, err
}

// qualifyKeys replaces each key without a kind with the kind-prefixed keys
// of every resource it matches.
func (a AnyResource) qualifyKeys(ctx context.Context, keys []string) ([]string, error) {
	qualified := make([]string, 0, len(keys))
	var bareKeys []string
	for _, key := range keys {
		if typ, _ := parseAnyResourceKey(key); typ == "" {
			bareKeys = append(bareKeys, key)
		} else {
			qualified = append(qualified, key)
		}
	}
	if len(bareKeys) == 0 {
		return qualified, nil
	}

	resources, err := a.Get(ctx, bareKeys)
	for _, res := range resources {
		qualified = append(qualified, res.Key().String())
	}
	return qualified, err
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
			perBackend[i] = wrapAnyResources(resources)
			if err != nil {
				return fmt.Errorf("failed to list %s resources: %w", typ, err)
			}
			return nil
		})
	}
	err := eg.Wait()
	return wslices.Flatten(perBackend), err
}

func (a AnyResource) Edit(ctx context.Context, key string, fields []resource.Field) error {
	keys, err := a.qualifyKeys(ctx, []string{key})
	if err != nil {
		return err
	}
	switch {
	case len(keys) == 0:
		return fmt.Errorf("resource not found: %s", key)
	case len(keys) > 1:
		return fmt.Errorf("ambiguous resource key: %s (found %v)", key, keys)
	}

	typ, underlyingKey := parseAnyResourceKey(keys[0])
	backend, _ := backendByType(typ)

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
	kindFields := resource.GetFieldByPath(fields, []string{"kind"})
	if len(kindFields) == 0 {
		return nil, fmt.Errorf("kind field is required")
	}
	typ := kindFields[0].Value.(string)

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

	return wrapAnyResources(results), nil
}

func (a AnyResource) underlyingFields(fields []resource.Field) []resource.Field {
	filtered := slices.Clone(fields)
	filtered = slices.DeleteFunc(filtered, func(field resource.Field) bool {
		return field.Name == "kind" || field.Name == "key"
	})
	return filtered
}

func (a AnyResource) Delete(ctx context.Context, keys []string) error {
	keys, qualifyErr := a.qualifyKeys(ctx, keys)

	keysByBackend := make([][]string, len(resourceBackends))
	for _, key := range keys {
		typ, underlyingKey := parseAnyResourceKey(key)
		idx := backendIndexByType(typ)
		backend := resourceBackends[idx]
		if _, ok := backend.(resource.DeletableResource); !ok {
			return fmt.Errorf("resource type %s does not support Delete", typ)
		}
		keysByBackend[idx] = append(keysByBackend[idx], underlyingKey)
	}

	// Delete in backend order.
	// HACK: This is intentionally NOT parallelized because backend order matters
	// (e.g. dependencies between resource types).
	errs := []error{qualifyErr}
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
