// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package testing

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

// TestEnv holds all test state for TestResource operations.
// Store it in context using WithTestEnv.
type TestEnv struct {
	Store map[string]TestResource
	Hooks Hooks
}

// NewTestEnv creates a new test environment with an empty store.
func NewTestEnv() *TestEnv {
	return &TestEnv{
		Store: make(map[string]TestResource),
	}
}

// Add adds a TestResource to the environment's store.
func (e *TestEnv) Add(r TestResource) {
	e.Store[r.Name] = r
}

// NewResource returns a new zero-value TestResource.
// The actual test state is stored in context via WithTestEnv.
func (e *TestEnv) NewResource() TestResource {
	return TestResource{}
}

type testEnvKey struct{}

// WithTestEnv stores a TestEnv in the context.
func WithTestEnv(ctx context.Context, env *TestEnv) context.Context {
	return context.WithValue(ctx, testEnvKey{}, env)
}

// testEnvFrom retrieves the TestEnv from context, or panics if not present.
func testEnvFrom(ctx context.Context) *TestEnv {
	env, ok := ctx.Value(testEnvKey{}).(*TestEnv)
	if !ok || env == nil {
		panic("TestEnv not found in context; use WithTestEnv")
	}
	return env
}

type TestResource struct {
	ID        string
	Name      string
	State     string
	URL       string
	Hidden    string
	Invisible string
	Lazy      string // Computed via callback when requested
	Created   time.Time
	Settings  TestSettings
	Authors   []TestAuthor
	Tags      []string
	Usage     types.MeterUsage[int]
}

var (
	_ resource.Resource          = (*TestResource)(nil)
	_ resource.GettableResource  = (*TestResource)(nil)
	_ resource.EditableResource  = (*TestResource)(nil)
	_ resource.CreatableResource = (*TestResource)(nil)
	_ resource.DeletableResource = (*TestResource)(nil)
)

type TestSettings struct {
	Foo   int
	Bar   string
	Score float64
	Flag  bool
}

type TestAuthor struct {
	Name  string
	Email string
}

type Hooks struct {
	List          func(context.Context, func(context.Context) ([]resource.Resource, error)) ([]resource.Resource, error)
	Get           func(context.Context, []string, func(context.Context, []string) ([]resource.Resource, error)) ([]resource.Resource, error)
	Create        func(context.Context, []resource.Field, func(context.Context, []resource.Field) ([]resource.Resource, error)) ([]resource.Resource, error)
	Delete        func(context.Context, []string, func(context.Context, []string) error) error
	OnLazyResolve func(resourceName string)
}

func (TestResource) Type() resource.Type {
	return resource.Type{
		Name:  "test",
		Names: "tests",
	}
}

type staticKey string

func (k staticKey) String() string {
	return string(k)
}

func (k staticKey) Canonical() string {
	return string(k)
}

func (t TestResource) Key() resource.Key {
	return staticKey(t.Name)
}

func (t TestResource) Raw() any {
	return t
}

func (t TestResource) Fields(ctx context.Context) ([]resource.Field, error) {
	// Note: ValueCallback receives context and can access TestEnv from there
	return []resource.Field{
		{
			Name:      "id",
			Value:     t.ID,
			Verbosity: resource.FieldVerbosityShort,
		},
		{
			Name:      "name",
			Value:     t.Name,
			Verbosity: resource.FieldVerbosityShort,
			Create: &resource.Patch{
				Set: t.Name, // Use actual value, not empty string
			},
		},
		{
			Name:      "state",
			Value:     t.State,
			Verbosity: resource.FieldVerbosityShort,
		},
		{
			Name:      "url",
			Value:     t.URL,
			Verbosity: resource.FieldVerbosityShort,
		},
		{
			Name:      "created",
			Value:     t.Created,
			Verbosity: resource.FieldVerbosityLong,
		},
		{
			Name:      "hidden",
			Value:     t.Hidden,
			Verbosity: resource.FieldVerbosityHidden,
		},
		{
			Name:      "invisible",
			Value:     t.Invisible,
			Verbosity: resource.FieldVerbosityInvisible,
		},
		{
			Name:      "lazy",
			Value:     t.Lazy,
			Verbosity: resource.FieldVerbosityHidden,
			ValueCallback: func(ctx context.Context) (any, error) {
				env := testEnvFrom(ctx)
				if env.Hooks.OnLazyResolve != nil {
					env.Hooks.OnLazyResolve(t.Name)
				}
				return "computed-" + t.Name, nil
			},
		},
		// Create-only field: Value is nil, so it only appears during create
		{
			Name:      "create_only",
			Value:     nil,
			Verbosity: resource.FieldVerbosityLong,
			Create: &resource.Patch{
				Set: "", // Type template only - no actual value since Value is nil
			},
		},
		// Edit-only field: Value is nil, so it only appears during edit
		{
			Name:      "edit_only",
			Value:     nil,
			Verbosity: resource.FieldVerbosityLong,
			Edit: &resource.Patch{
				Set: "", // Type template only - no actual value since Value is nil
			},
		},
		{
			Name:      "settings",
			Verbosity: resource.FieldVerbosityLong,
			Subfields: []resource.Field{
				{
					Name:      "foo",
					Value:     t.Settings.Foo,
					Verbosity: resource.FieldVerbosityLong,
					Flag:      &resource.FlagSpec{Name: "foo", Tag: `help:"Foo setting."`},
					Create: &resource.Patch{
						Set: t.Settings.Foo, // Use actual value
					},
					Edit: &resource.Patch{
						Set: t.Settings.Foo, // Use actual value
					},
				},
				{
					Name:      "bar",
					Value:     t.Settings.Bar,
					Verbosity: resource.FieldVerbosityLong,
					Flag:      &resource.FlagSpec{Name: "bar", Tag: `help:"Bar setting."`},
					Create: &resource.Patch{
						Set: t.Settings.Bar, // Use actual value
					},
					Edit: &resource.Patch{
						Set: t.Settings.Bar, // Use actual value
					},
				},
				{
					Name:      "score",
					Value:     t.Settings.Score,
					Verbosity: resource.FieldVerbosityLong,
				},
				{
					Name:      "flag",
					Value:     t.Settings.Flag,
					Verbosity: resource.FieldVerbosityLong,
				},
			},
		},
		{
			Name:      "authors",
			Verbosity: resource.FieldVerbosityLong,
			Elem: &resource.Field{
				Subfields: []resource.Field{
					{
						Name:      "name",
						Verbosity: resource.FieldVerbosityLong,
					},
					{
						Name:      "email",
						Verbosity: resource.FieldVerbosityLong,
					},
				},
			},
			Subfields: func() []resource.Field {
				var fields []resource.Field
				for i, author := range t.Authors {
					fields = append(fields, resource.Field{
						Name:      fmt.Sprintf("%d", i),
						Verbosity: resource.FieldVerbosityLong,
						Subfields: []resource.Field{
							{
								Name:      "name",
								Value:     author.Name,
								Verbosity: resource.FieldVerbosityLong,
							},
							{
								Name:      "email",
								Value:     author.Email,
								Verbosity: resource.FieldVerbosityLong,
							},
						},
					})
				}
				return fields
			}(),
		},
		{
			Name:      "tags",
			Value:     t.Tags,
			Verbosity: resource.FieldVerbosityLong,
		},
		{
			Name:      "usage",
			Value:     t.Usage,
			Verbosity: resource.FieldVerbosityLong,
		},
	}, nil
}

func (TestResource) List(ctx context.Context) ([]resource.Resource, error) {
	env := testEnvFrom(ctx)
	original := func(context.Context) ([]resource.Resource, error) {
		var resources []resource.Resource
		for _, r := range env.Store {
			resources = append(resources, r)
		}
		// Sort by ID for deterministic output
		slices.SortFunc(resources, func(a, b resource.Resource) int {
			return strings.Compare(a.(TestResource).ID, b.(TestResource).ID)
		})
		return resources, nil
	}
	if env.Hooks.List != nil {
		return env.Hooks.List(ctx, original)
	}
	return original(ctx)
}

func (TestResource) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	env := testEnvFrom(ctx)
	original := func(_ context.Context, keys []string) ([]resource.Resource, error) {
		// Build a map for lookup
		resourceMap := make(map[string]TestResource)
		for _, key := range keys {
			if r, ok := env.Store[key]; ok {
				resourceMap[key] = r
			}
		}

		// Return resources in the order of keys provided
		var resources []resource.Resource
		for _, key := range keys {
			if r, ok := resourceMap[key]; ok {
				resources = append(resources, r)
			}
		}
		return resources, nil
	}
	if env.Hooks.Get != nil {
		return env.Hooks.Get(ctx, keys, original)
	}
	return original(ctx, keys)
}

func (TestResource) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	env := testEnvFrom(ctx)
	original := func(_ context.Context, fields []resource.Field) ([]resource.Resource, error) {
		r := TestResource{
			Settings: TestSettings{},
		}

		for key, field := range resource.IterFields(fields) {
			if field.Create == nil || field.Create.Set == nil {
				continue
			}
			switch key.String() {
			case "name":
				r.Name = field.Create.Set.(string)
			case "settings.foo":
				r.Settings.Foo = field.Create.Set.(int)
			case "settings.bar":
				r.Settings.Bar = field.Create.Set.(string)
			}
		}

		env.Store[r.Name] = r
		return []resource.Resource{r}, nil
	}
	if env.Hooks.Create != nil {
		return env.Hooks.Create(ctx, fields, original)
	}
	return original(ctx, fields)
}

func (TestResource) Edit(ctx context.Context, key string, fields []resource.Field) error {
	env := testEnvFrom(ctx)
	r, ok := env.Store[key]
	if !ok {
		return fmt.Errorf("resource %q not found", key)
	}

	for key, field := range resource.IterFields(fields) {
		if field.Edit == nil || field.Edit.Set == nil {
			continue
		}
		switch key.String() {
		case "settings.foo":
			r.Settings.Foo = field.Edit.Set.(int)
		case "settings.bar":
			r.Settings.Bar = field.Edit.Set.(string)
		}
	}

	env.Store[r.Name] = r
	return nil
}

func (TestResource) Delete(ctx context.Context, keys []string) error {
	env := testEnvFrom(ctx)
	original := func(_ context.Context, keys []string) error {
		for _, key := range keys {
			delete(env.Store, key)
		}
		return nil
	}
	if env.Hooks.Delete != nil {
		return env.Hooks.Delete(ctx, keys, original)
	}
	return original(ctx, keys)
}
