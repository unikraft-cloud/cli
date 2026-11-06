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

	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/types"
)

// TestEnv holds all test state for TestModel operations.
// Store it in context using WithTestEnv.
type TestEnv struct {
	Store map[string]TestModel
	Hooks Hooks
}

// NewTestEnv creates a new test environment with an empty store.
func NewTestEnv() *TestEnv {
	return &TestEnv{
		Store: make(map[string]TestModel),
	}
}

// Add adds a TestModel to the environment's store.
func (e *TestEnv) Add(r TestModel) {
	e.Store[r.Name] = r
}

// NewModel returns a new zero-value TestModel.
// The actual test state is stored in context via WithTestEnv.
func (e *TestEnv) NewModel() TestModel {
	return TestModel{}
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

type TestModel struct {
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
	_ model.Model          = (*TestModel)(nil)
	_ model.GettableModel  = (*TestModel)(nil)
	_ model.EditableModel  = (*TestModel)(nil)
	_ model.CreatableModel = (*TestModel)(nil)
	_ model.DeletableModel = (*TestModel)(nil)
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
	List          func(context.Context, func(context.Context) ([]model.Model, error)) ([]model.Model, error)
	Get           func(context.Context, []string, func(context.Context, []string) ([]model.Model, error)) ([]model.Model, error)
	Create        func(context.Context, []model.Field, func(context.Context, []model.Field) ([]model.Model, error)) ([]model.Model, error)
	Delete        func(context.Context, []string, func(context.Context, []string) error) error
	OnLazyResolve func(resourceName string)
}

func (TestModel) Type() model.Type {
	return model.Type{
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

func (t TestModel) Key() model.Key {
	return staticKey(t.Name)
}

func (t TestModel) Raw() any {
	return t
}

func (t TestModel) Fields(ctx context.Context) ([]model.Field, error) {
	// Note: ValueCallback receives context and can access TestEnv from there
	return []model.Field{
		{
			Name:      "id",
			Value:     t.ID,
			Verbosity: model.FieldVerbosityShort,
		},
		{
			Name:      "name",
			Value:     t.Name,
			Verbosity: model.FieldVerbosityShort,
			Create: &model.Patch{
				Set: t.Name, // Use actual value, not empty string
			},
		},
		{
			Name:      "state",
			Value:     t.State,
			Verbosity: model.FieldVerbosityShort,
			Help:      "Lifecycle state: running or stopped.",
		},
		{
			Name:      "url",
			Value:     t.URL,
			Verbosity: model.FieldVerbosityShort,
		},
		{
			Name:      "created",
			Value:     t.Created,
			Verbosity: model.FieldVerbosityLong,
		},
		{
			Name:      "hidden",
			Value:     t.Hidden,
			Verbosity: model.FieldVerbosityHidden,
		},
		{
			Name:      "invisible",
			Value:     t.Invisible,
			Verbosity: model.FieldVerbosityInvisible,
		},
		{
			Name:      "lazy",
			Value:     t.Lazy,
			Verbosity: model.FieldVerbosityHidden,
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
			Verbosity: model.FieldVerbosityLong,
			Create: &model.Patch{
				Set: "", // Type template only - no actual value since Value is nil
			},
		},
		// Edit-only field: Value is nil, so it only appears during edit
		{
			Name:      "edit_only",
			Value:     nil,
			Verbosity: model.FieldVerbosityLong,
			Edit: &model.Patch{
				Set: "", // Type template only - no actual value since Value is nil
			},
		},
		{
			Name:      "settings",
			Verbosity: model.FieldVerbosityLong,
			Subfields: []model.Field{
				{
					Name:      "foo",
					Value:     t.Settings.Foo,
					Verbosity: model.FieldVerbosityLong,
					Help:      "Foo setting.",
					Flag:      &model.FlagSpec{Name: "foo", Tag: `help:"Foo setting."`},
					Create: &model.Patch{
						Set: t.Settings.Foo, // Use actual value
					},
					Edit: &model.Patch{
						Set: t.Settings.Foo, // Use actual value
					},
				},
				{
					Name:      "bar",
					Value:     t.Settings.Bar,
					Verbosity: model.FieldVerbosityLong,
					Help:      "Bar setting.",
					Flag:      &model.FlagSpec{Name: "bar", Tag: `help:"Bar setting."`},
					Create: &model.Patch{
						Set: t.Settings.Bar, // Use actual value
					},
					Edit: &model.Patch{
						Set: t.Settings.Bar, // Use actual value
					},
				},
				{
					Name:      "score",
					Value:     t.Settings.Score,
					Verbosity: model.FieldVerbosityLong,
				},
				{
					Name:      "flag",
					Value:     t.Settings.Flag,
					Verbosity: model.FieldVerbosityLong,
				},
			},
		},
		{
			Name:      "authors",
			Verbosity: model.FieldVerbosityLong,
			Elem: &model.Field{
				Subfields: []model.Field{
					{
						Name:      "name",
						Verbosity: model.FieldVerbosityLong,
					},
					{
						Name:      "email",
						Verbosity: model.FieldVerbosityLong,
					},
				},
			},
			Subfields: func() []model.Field {
				var fields []model.Field
				for i, author := range t.Authors {
					fields = append(fields, model.Field{
						Name:      fmt.Sprintf("%d", i),
						Verbosity: model.FieldVerbosityLong,
						Subfields: []model.Field{
							{
								Name:      "name",
								Value:     author.Name,
								Verbosity: model.FieldVerbosityLong,
							},
							{
								Name:      "email",
								Value:     author.Email,
								Verbosity: model.FieldVerbosityLong,
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
			Verbosity: model.FieldVerbosityLong,
		},
		{
			Name:      "usage",
			Value:     t.Usage,
			Verbosity: model.FieldVerbosityLong,
		},
	}, nil
}

func (TestModel) List(ctx context.Context) ([]model.Model, error) {
	env := testEnvFrom(ctx)
	original := func(context.Context) ([]model.Model, error) {
		var resources []model.Model
		for _, r := range env.Store {
			resources = append(resources, r)
		}
		// Sort by ID for deterministic output
		slices.SortFunc(resources, func(a, b model.Model) int {
			return strings.Compare(a.(TestModel).ID, b.(TestModel).ID)
		})
		return resources, nil
	}
	if env.Hooks.List != nil {
		return env.Hooks.List(ctx, original)
	}
	return original(ctx)
}

func (TestModel) Get(ctx context.Context, keys []string) ([]model.Model, error) {
	env := testEnvFrom(ctx)
	original := func(_ context.Context, keys []string) ([]model.Model, error) {
		// Build a map for lookup
		resourceMap := make(map[string]TestModel)
		for _, key := range keys {
			if r, ok := env.Store[key]; ok {
				resourceMap[key] = r
			}
		}

		// Return resources in the order of keys provided
		var resources []model.Model
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

func (TestModel) Create(ctx context.Context, fields []model.Field) ([]model.Model, error) {
	env := testEnvFrom(ctx)
	original := func(_ context.Context, fields []model.Field) ([]model.Model, error) {
		r := TestModel{
			Settings: TestSettings{},
		}

		for key, field := range model.IterFields(fields) {
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
		return []model.Model{r}, nil
	}
	if env.Hooks.Create != nil {
		return env.Hooks.Create(ctx, fields, original)
	}
	return original(ctx, fields)
}

func (TestModel) Edit(ctx context.Context, key string, fields []model.Field) error {
	env := testEnvFrom(ctx)
	r, ok := env.Store[key]
	if !ok {
		return fmt.Errorf("resource %q not found", key)
	}

	for key, field := range model.IterFields(fields) {
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

func (TestModel) Delete(ctx context.Context, keys []string) error {
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
