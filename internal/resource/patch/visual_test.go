// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
)

// mockResource implements a simple test resource for visual editing tests.
type mockResource struct {
	name string
}

func (m mockResource) Type() resource.Type {
	return resource.Type{Name: "mock", Names: "mocks"}
}

type mockKey string

func (k mockKey) String() string {
	return string(k)
}

func (k mockKey) Canonical() string {
	return string(k)
}

func (m mockResource) Key() resource.Key {
	return mockKey(m.name)
}

func (m mockResource) Fields(ctx context.Context) ([]resource.Field, error) {
	return nil, nil
}

func (m mockResource) Raw() any {
	return m
}

func TestEdit_Basic(t *testing.T) {
	res := mockResource{name: "test-resource"}

	// Define fields with actual values in patch.Set
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original-name",
			Edit: &resource.Patch{
				Set: "original-name",
			},
		},
		{
			Name:  "count",
			Value: 42,
			Edit: &resource.Patch{
				Set: 42,
			},
		},
	}

	// Editor that changes the name field but leaves count unchanged
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: new-name
count: 42
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)
	// Only the changed field should be in the result
	require.Len(t, result, 1)

	// Check that name was updated
	var nameField *resource.Field
	for i := range result {
		if result[i].Name == "name" {
			nameField = &result[i]
		}
	}

	require.NotNil(t, nameField)
	assert.Equal(t, "new-name", nameField.Edit.Set)
}

func TestCreate_Basic(t *testing.T) {
	res := mockResource{name: ""}

	// Define fields with empty/zero values for creation
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "",
			Create: &resource.Patch{
				Set: "",
			},
		},
		{
			Name:  "count",
			Value: 0,
			Create: &resource.Patch{
				Set: 0,
			},
		},
	}

	// Editor that sets values
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: created-resource
count: 100
`), nil
	}

	result, err := Create(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)
	require.Len(t, result, 2)

	// Check values were set
	var nameField, countField *resource.Field
	for i := range result {
		switch result[i].Name {
		case "name":
			nameField = &result[i]
		case "count":
			countField = &result[i]
		}
	}

	require.NotNil(t, nameField)
	require.NotNil(t, countField)
	assert.Equal(t, "created-resource", nameField.Create.Set)
	assert.Equal(t, 100, countField.Create.Set)
}

func TestEdit_NestedFields(t *testing.T) {
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name: "settings",
			Subfields: []resource.Field{
				{
					Name:  "foo",
					Value: 10,
					Edit: &resource.Patch{
						Set: 10,
					},
				},
				{
					Name:  "bar",
					Value: "hello",
					Edit: &resource.Patch{
						Set: "hello",
					},
				},
			},
		},
	}

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
settings:
  foo: 20
  bar: world
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)
	require.Len(t, result, 1)

	settings := result[0]
	require.Equal(t, "settings", settings.Name)
	require.Len(t, settings.Subfields, 2)

	var fooField, barField *resource.Field
	for i := range settings.Subfields {
		switch settings.Subfields[i].Name {
		case "foo":
			fooField = &settings.Subfields[i]
		case "bar":
			barField = &settings.Subfields[i]
		}
	}

	require.NotNil(t, fooField)
	require.NotNil(t, barField)
	assert.Equal(t, 20, fooField.Edit.Set)
	assert.Equal(t, "world", barField.Edit.Set)
}

func TestEdit_WithPendingPatches(t *testing.T) {
	res := mockResource{name: "test"}

	// Fields with original values
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
	}

	// Pending patches from previous operation
	patches := []resource.Field{
		{
			Name: "name",
			Edit: &resource.Patch{
				Set: "previously-set",
			},
		},
	}

	// Editor receives the pending patch value and changes it
	var receivedInput []byte
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		receivedInput = input
		return []byte(`
name: final-value
`), nil
	}

	result, err := Edit(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	// Check that editor received the pending patch value
	assert.Contains(t, string(receivedInput), "previously-set")

	// Check final result
	require.Len(t, result, 1)
	assert.Equal(t, "final-value", result[0].Edit.Set)
}

func TestEdit_UnknownFieldsError(t *testing.T) {
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
	}

	// Editor adds an unknown field
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: original
unknown_field: should_fail
`), nil
	}

	_, err := Edit(context.Background(), res, fields, nil, editor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown fields")
}

func TestEdit_EmptyDataError(t *testing.T) {
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
	}

	// Editor returns empty data
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte("   \n  "), nil
	}

	_, err := Edit(context.Background(), res, fields, nil, editor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestEdit_WrongTypeError(t *testing.T) {
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "count",
			Value: 42,
			Edit: &resource.Patch{
				Set: 42, // int type
			},
		},
	}

	// Editor returns a string value where int is expected
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
count: not_a_number
`), nil
	}

	_, err := Edit(context.Background(), res, fields, nil, editor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "convert")
}

func TestEdit_FieldRemoved(t *testing.T) {
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
		{
			Name:  "count",
			Value: 42,
			Edit: &resource.Patch{
				Set: 42,
			},
		},
	}

	// Editor removes the count field (leaving name unchanged)
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: original
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Since name is unchanged and count is removed (no change), result should be empty
	assert.Empty(t, result, "no fields should have changes when one is unchanged and one is removed")
}

func TestEdit_WithTestResource(t *testing.T) {
	// Test using the shared TestResource from testing package
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-test1",
		Name: "test1",
		Settings: resourcet.TestSettings{
			Foo: 42,
			Bar: "hello",
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)

	// Get the resource and its fields
	resources, err := resourcet.TestResource{}.Get(ctx, []string{"test1"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	res := resources[0].(resourcet.TestResource)
	fields, err := res.Fields(ctx)
	require.NoError(t, err)

	// Editor that changes settings
	// Note: 'name' field only has Create patch, not Edit, so it won't be in the editable fields
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		// The input should contain the current patch.Set values for editable fields
		assert.Contains(t, string(input), "42")    // settings.foo
		assert.Contains(t, string(input), "hello") // settings.bar
		// Check that 'name' is not in the YAML body (only in the header comment)
		assert.Contains(t, string(input), "# test test1") // header with resource key
		// But 'name:' field should not be present as it's not editable
		assert.NotContains(t, string(input), "name:")

		return []byte(`
settings:
  foo: 100
  bar: world
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Find the settings fields in result
	var settingsFoo, settingsBar *resource.Field
	for _, f := range result {
		if f.Name == "settings" {
			for i := range f.Subfields {
				switch f.Subfields[i].Name {
				case "foo":
					settingsFoo = &f.Subfields[i]
				case "bar":
					settingsBar = &f.Subfields[i]
				}
			}
		}
	}

	require.NotNil(t, settingsFoo, "settings.foo should be in result")
	require.NotNil(t, settingsBar, "settings.bar should be in result")
	assert.Equal(t, 100, settingsFoo.Edit.Set)
	assert.Equal(t, "world", settingsBar.Edit.Set)
}

func TestCreate_WithTestResource(t *testing.T) {
	// Get template fields from empty resource
	res := resourcet.TestResource{}
	fields, err := res.Fields(context.Background())
	require.NoError(t, err)

	// Editor that sets creation values
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: new-resource
settings:
  foo: 999
  bar: created
`), nil
	}

	result, err := Create(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Find the name and settings fields in result
	var nameField *resource.Field
	var settingsFoo, settingsBar *resource.Field
	for i := range result {
		switch result[i].Name {
		case "name":
			nameField = &result[i]
		case "settings":
			for j := range result[i].Subfields {
				switch result[i].Subfields[j].Name {
				case "foo":
					settingsFoo = &result[i].Subfields[j]
				case "bar":
					settingsBar = &result[i].Subfields[j]
				}
			}
		}
	}

	require.NotNil(t, nameField, "name should be in result")
	require.NotNil(t, settingsFoo, "settings.foo should be in result")
	require.NotNil(t, settingsBar, "settings.bar should be in result")

	assert.Equal(t, "new-resource", nameField.Create.Set)
	assert.Equal(t, 999, settingsFoo.Create.Set)
	assert.Equal(t, "created", settingsBar.Create.Set)
}

func TestEdit_HiddenFieldAddedWithZeroValue(t *testing.T) {
	// Tests that when a user manually adds a hidden field with a zero value
	// (e.g., vsock: false), it should be preserved in the result.
	res := mockResource{name: "test"}

	// Fields: 'name' is displayed (has Value), 'hidden' is not (Value is nil)
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
		{
			Name:  "hidden",
			Value: nil, // Not displayed because Value is nil
			Edit: &resource.Patch{
				Set: false, // Template type is bool
			},
		},
	}

	// Editor adds the hidden field with the same value as template (false)
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		// Input should NOT contain 'hidden' since Value is nil
		assert.NotContains(t, string(input), "hidden")
		// User manually adds hidden: false
		return []byte(`
name: original
hidden: false
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Find the hidden field
	var hiddenField *resource.Field
	for i := range result {
		if result[i].Name == "hidden" {
			hiddenField = &result[i]
			break
		}
	}

	// Hidden field should be in result with the value set, even though it's false
	require.NotNil(t, hiddenField, "hidden field should be in result")
	assert.Equal(t, false, hiddenField.Edit.Set, "hidden field should have Set=false")
}

func TestEdit_HiddenFieldAddedWithNonZeroValue(t *testing.T) {
	// Tests that when a user manually adds a hidden field with a non-zero value
	// (e.g., vsock: true), it should be preserved in the result.
	res := mockResource{name: "test"}

	// Fields: 'name' is displayed (has Value), 'hidden' is not (Value is nil)
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
		{
			Name:  "hidden",
			Value: nil, // Not displayed because Value is nil
			Edit: &resource.Patch{
				Set: false, // Template type is bool
			},
		},
	}

	// Editor adds the hidden field with a different value (true)
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
name: original
hidden: true
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Find the hidden field
	var hiddenField *resource.Field
	for i := range result {
		if result[i].Name == "hidden" {
			hiddenField = &result[i]
			break
		}
	}

	// Hidden field should be in result with the value set
	require.NotNil(t, hiddenField, "hidden field should be in result")
	assert.Equal(t, true, hiddenField.Edit.Set, "hidden field should have Set=true")
}

func TestEdit_DisplayedFieldUnchanged(t *testing.T) {
	// Tests that when a displayed field is unchanged, it should NOT be in the result.
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "visible",
			Value: false, // Displayed because Value is not nil
			Edit: &resource.Patch{
				Set: false,
			},
		},
	}

	// Editor returns the same value
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		return []byte(`
visible: false
`), nil
	}

	result, err := Edit(context.Background(), res, fields, nil, editor)
	require.NoError(t, err)

	// Field is unchanged, so it should NOT be in the result
	assert.Empty(t, result, "unchanged displayed field should not be in result")
}

func TestEdit_PendingPatchShownInEditor(t *testing.T) {
	// Tests that when a field is set via --set (pending patch), it appears in the editor
	// even if the field has Value: nil
	res := mockResource{name: "test"}

	// Fields: 'name' is displayed (has Value), 'hidden' is not (Value is nil)
	// Using *bool for hidden so nil means "not set" vs explicit true/false
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "original",
			Edit: &resource.Patch{
				Set: "original",
			},
		},
		{
			Name:  "hidden",
			Value: nil, // Not displayed by default
			Edit: &resource.Patch{
				Set: (*bool)(nil), // Template type is *bool, nil means not set
			},
		},
	}

	// Pending patches - simulates --set hidden=true
	patches := []resource.Field{
		{
			Name: "hidden",
			Edit: &resource.Patch{
				Set: new(true),
			},
		},
	}

	var receivedInput []byte
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		receivedInput = input
		// Return the same values
		return []byte(`
name: original
hidden: true
`), nil
	}

	result, err := Edit(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	// Check that editor received the hidden field with the pending patch value
	assert.Contains(t, string(receivedInput), "hidden: true", "hidden field should appear in editor with pending patch value")

	// Find the hidden field in result
	var hiddenField, nameField *resource.Field
	for i := range result {
		switch result[i].Name {
		case "hidden":
			hiddenField = &result[i]
		case "name":
			nameField = &result[i]
		}
	}

	// Hidden field was displayed with pending patch value (true).
	// Since original field.Value was nil, and edited value (true) differs from original,
	// the patch should be PRESERVED.
	require.NotNil(t, hiddenField, "hidden field should be in result")
	assert.True(t, *hiddenField.Edit.Set.(*bool), "patch should be preserved")

	// Name field was displayed with original value ("original").
	// Since edited value equals original, it should NOT be in the result.
	assert.Nil(t, nameField, "unchanged field should not be in result")
}

func TestEdit_PendingPatchChangedInEditor(t *testing.T) {
	// Tests that when a field is set via --set and then changed in the editor
	res := mockResource{name: "test"}

	fields := []resource.Field{
		{
			Name:  "hidden",
			Value: nil,
			Edit: &resource.Patch{
				Set: false,
			},
		},
	}

	// Pending patches - simulates --set hidden=true
	patches := []resource.Field{
		{
			Name: "hidden",
			Edit: &resource.Patch{
				Set: true,
			},
		},
	}

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		// User changes it back to false
		return []byte(`
hidden: false
`), nil
	}

	result, err := Edit(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	var hiddenField *resource.Field
	for i := range result {
		if result[i].Name == "hidden" {
			hiddenField = &result[i]
			break
		}
	}

	// Hidden field was changed from true to false
	require.NotNil(t, hiddenField, "hidden field should be in result")
	assert.Equal(t, false, hiddenField.Edit.Set, "changed field should have new value")
}

func TestCreate_PendingPatchShownInEditor(t *testing.T) {
	// Tests that fields set via --set appear in visual editor during create
	// Simulates: unikraft instance create --set image=test --set vsock=true -e --dry-run
	res := mockResource{name: ""}

	// Fields for creation - using pointer types so nil means "not set"
	fields := []resource.Field{
		{
			Name:  "image",
			Value: "",
			Create: &resource.Patch{
				Set: (*string)(nil), // nil means not set
			},
		},
		{
			Name:  "vsock",
			Value: nil, // Not shown by default
			Create: &resource.Patch{
				Set: (*bool)(nil), // nil means not set
			},
		},
	}

	// Pending patches - simulates --set image=test --set vsock=true
	patches := []resource.Field{
		{
			Name: "image",
			Create: &resource.Patch{
				Set: new("test-image"),
			},
		},
		{
			Name: "vsock",
			Create: &resource.Patch{
				Set: new(true),
			},
		},
	}

	var receivedInput []byte
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		receivedInput = input
		// Return unchanged
		return []byte(`
image: test-image
vsock: true
`), nil
	}

	result, err := Create(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	// Check that editor received both fields
	t.Logf("Editor input: %s", string(receivedInput))
	assert.Contains(t, string(receivedInput), "image: test-image", "image should appear in editor")
	assert.Contains(t, string(receivedInput), "vsock: true", "vsock should appear in editor with pending patch value")

	// Find fields in result
	var imageField, vsockField *resource.Field
	for i := range result {
		switch result[i].Name {
		case "image":
			imageField = &result[i]
		case "vsock":
			vsockField = &result[i]
		}
	}

	// Both fields were displayed with pending patch values.
	// Since the original field.Value was empty/nil, and the edited values differ from original,
	// the patches should be PRESERVED (not cleared).
	require.NotNil(t, imageField, "image field should be in result")
	require.NotNil(t, vsockField, "vsock field should be in result")
	assert.Equal(t, "test-image", *imageField.Create.Set.(*string), "patch should be preserved")
	assert.True(t, *vsockField.Create.Set.(*bool), "patch should be preserved")
}

func TestCreate_PendingPatchUnchangedInEditorReturnsInput(t *testing.T) {
	// Tests that when editor returns unchanged input, pending patches are preserved
	// This simulates what happens when user opens editor and just saves without changes
	res := mockResource{name: ""}

	fields := []resource.Field{
		{
			Name:  "image",
			Value: "",
			Create: &resource.Patch{
				Set: "",
			},
		},
		{
			Name:  "metro",
			Value: "",
			Create: &resource.Patch{
				Set: "",
			},
		},
		{
			Name:  "vsock",
			Value: nil,
			Create: &resource.Patch{
				Set: false,
			},
		},
	}

	patches := []resource.Field{
		{
			Name: "image",
			Create: &resource.Patch{
				Set: "sklj",
			},
		},
		{
			Name: "metro",
			Create: &resource.Patch{
				Set: "dfalk",
			},
		},
		{
			Name: "vsock",
			Create: &resource.Patch{
				Set: true,
			},
		},
	}

	// Editor returns unchanged - simulates user saving without edits
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		t.Logf("Editor received:\n%s", string(input))
		return input, nil
	}

	result, err := Create(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	t.Logf("Result fields:")
	for _, f := range result {
		t.Logf("  %s: Create=%+v", f.Name, f.Create)
	}

	// After FilterCreatableFields, we should have fields with Create != nil
	// But since values are unchanged from what was displayed, Create.Set should be nil
	// This is the PROBLEM: we're clearing the patches even though the user intended them

	// Actually wait - this is CORRECT behavior per our design:
	// If user didn't change the values in the editor, the patches are "no-ops"
	// But the ORIGINAL --set values should still be applied!

	// The issue is that when --cmd is used, the comparison happens against the
	// DISPLAYED value (which includes pending patches), and if unchanged, we clear Set.
	// But the original --set patches should still be applied to the create operation.
}

// TestEdit_FieldWithAddDelTemplate tests that fields with add/del templates
// (from struct tags like edit:"set,add,del=keys") work correctly when the
// templates have empty (but non-nil) Add/Del values.
func TestEdit_FieldWithAddDelTemplate(t *testing.T) {
	res := mockResource{name: "test-instance"}

	// Simulate fields like runtime.env which has edit:"set,add,del=keys"
	// The struct tag parser creates empty (non-nil) slices/maps for Add/Del
	fields := []resource.Field{
		{
			Name:  "name",
			Value: "test-instance",
			Edit:  &resource.Patch{Set: new(string)},
		},
		{
			Name:  "env",
			Value: map[string]string{"FOO": "bar"},
			Edit: &resource.Patch{
				Set: map[string]string{}, // set template
				Add: map[string]string{}, // add template (empty, from struct tag)
				Del: []string{},          // del template (empty, from struct tag)
			},
		},
	}

	patches := []resource.Field{} // No --set patches

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		t.Logf("Editor input:\n%s", string(input))
		return input, nil // Return unchanged
	}

	_, err := Edit(context.Background(), res, fields, patches, editor)
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
}

// TestEdit_FieldWithAddDelTemplateFromStruct tests visual editing with fields
// generated from actual struct tags (using FieldsFromStruct).
func TestEdit_FieldWithAddDelTemplateFromStruct(t *testing.T) {
	res := mockResource{name: "test-instance"}

	// Use resourcet.TestResource which has fields with add/del support
	type Runtime struct {
		Env map[string]string `field:",long" edit:"set,add,del=keys"`
	}
	type TestInstance struct {
		Name    string  `field:",short" edit:"set"`
		Runtime Runtime `field:",embed"`
	}

	inst := TestInstance{
		Name: "test-instance",
		Runtime: Runtime{
			Env: map[string]string{"FOO": "bar"},
		},
	}

	fields, err := resource.FieldsFromStruct(inst)
	require.NoError(t, err)

	// Log what we got
	for path, field := range resource.IterFields(fields) {
		if field.Edit != nil {
			t.Logf("Field %s: Edit.Set=%v, Edit.Add=%v (nil=%v), Edit.Del=%v (nil=%v)",
				path, field.Edit.Set, field.Edit.Add, field.Edit.Add == nil, field.Edit.Del, field.Edit.Del == nil)
		}
	}

	patches := []resource.Field{} // No --set patches

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		t.Logf("Editor input:\n%s", string(input))
		return input, nil // Return unchanged
	}

	_, err = Edit(context.Background(), res, fields, patches, editor)
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
}

// TestEdit_SimulateActualCommand simulates the actual command flow:
// `unikraft instance edit -e http-go121-yd6jz --dry-run --set vsock=false`
func TestEdit_SimulateActualCommand(t *testing.T) {
	res := mockResource{name: "test-instance"}

	// Simulate the Instance struct's fields (simplified)
	type Runtime struct {
		Env map[string]string `field:",long" edit:"set,add,del=keys"`
	}
	type TestInstance struct {
		Name    string  `field:",short" edit:"set"`
		Vsock   bool    `field:",short" edit:"set"`
		Runtime Runtime `field:",embed"`
	}

	inst := TestInstance{
		Name:  "test-instance",
		Vsock: true,
		Runtime: Runtime{
			Env: map[string]string{"FOO": "bar"},
		},
	}

	// Step 1: Get fields from struct (like res.Fields())
	fields, err := resource.FieldsFromStruct(inst)
	require.NoError(t, err)

	t.Log("Fields from struct:")
	for path, field := range resource.IterFields(fields) {
		if field.Edit != nil {
			t.Logf("  %s: Edit.Set=%v, Edit.Add=%v, Edit.Del=%v",
				path, field.Edit.Set, field.Edit.Add, field.Edit.Del)
		}
	}

	// Step 2: Create PatchSpec from --set vsock=false (like cmd.toPatchSpec())
	spec := PatchSpec{
		Set: map[string][]string{
			"vsock": {"false"},
		},
	}

	// Step 3: Apply patches (like patch.PatchedFields)
	patched, err := PatchedFields(t.Context(), fields, spec)
	require.NoError(t, err)

	t.Log("Patched fields (from --set):")
	for path, field := range resource.IterFields(patched) {
		if field.Edit != nil {
			t.Logf("  %s: Edit.Set=%v, Edit.Add=%v, Edit.Del=%v",
				path, field.Edit.Set, field.Edit.Add, field.Edit.Del)
		}
	}

	// Step 4: Visual edit (like patch.VisualEdit)
	// This is where the error occurs
	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		t.Logf("Editor input:\n%s", string(input))
		return input, nil // Return unchanged
	}

	_, err = Edit(context.Background(), res, fields, patched, editor)
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
}

// imageRef simulates an ImageRef type that implements TextMarshaler/TextUnmarshaler
type imageRef struct{ ref string }

func (t imageRef) MarshalText() ([]byte, error) { return []byte(t.ref), nil }
func (t *imageRef) UnmarshalText(b []byte) error {
	t.ref = string(b)
	return nil
}

// TestEdit_TextMarshalerEquality tests that fields with TextMarshaler values
// that marshal to the same text are considered equal. This is important for
// types like ImageRef where the value might have different internal state
// after YAML round-tripping but should be considered equal.
func TestEdit_TextMarshalerEquality(t *testing.T) {
	res := mockResource{name: "test"}

	// The current value and template both use imageRef with "nginx"
	currentValue := imageRef{ref: "nginx"}

	fields := []resource.Field{
		{
			Name:  "image",
			Value: currentValue,
			Edit:  &resource.Patch{Set: currentValue}, // Set contains current value
		},
	}

	patches := []resource.Field{}

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		// Return the same value - simulates `--cmd cat`
		return input, nil
	}

	result, err := Edit(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	// Since the value was unchanged, there should be no patches
	assert.Empty(t, result, "expected no patches when TextMarshaler values are semantically equal")
}

// TestEdit_TextMarshalerInequality tests that fields with TextMarshaler values
// that marshal to different text are considered different.
func TestEdit_TextMarshalerInequality(t *testing.T) {
	res := mockResource{name: "test"}

	currentValue := imageRef{ref: "nginx"}

	fields := []resource.Field{
		{
			Name:  "image",
			Value: currentValue,
			Edit:  &resource.Patch{Set: currentValue},
		},
	}

	patches := []resource.Field{}

	editor := func(ctx context.Context, input []byte) ([]byte, error) {
		// Change the value
		return []byte("image: alpine\n"), nil
	}

	result, err := Edit(context.Background(), res, fields, patches, editor)
	require.NoError(t, err)

	// Since the value was changed, there should be a patch
	require.Len(t, result, 1)
	assert.Equal(t, "image", result[0].Name)
	assert.NotNil(t, result[0].Edit.Set)
	assert.Equal(t, imageRef{ref: "alpine"}, result[0].Edit.Set)
}

func TestSetNestedValue_AncestorWithOwnValue(t *testing.T) {
	type link struct {
		Name string `json:"name"`
	}

	t.Run("expands so the descendant lands", func(t *testing.T) {
		m := map[string]any{}
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service"}, link{Name: "my-group"}))
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service", "domains"}, []string{"a.com"}))

		assert.Equal(t, map[string]any{"service": map[string]any{
			"name":    "my-group",
			"domains": []string{"a.com"},
		}}, m)
	})

	t.Run("descendant wins over the ancestor's own key", func(t *testing.T) {
		m := map[string]any{}
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service"}, link{Name: "from-ancestor"}))
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service", "name"}, "from-descendant"))

		assert.Equal(t, map[string]any{"service": map[string]any{
			"name": "from-descendant",
		}}, m)
	})

	t.Run("ancestor without an object form still nests", func(t *testing.T) {
		m := map[string]any{}
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service"}, "my-group"))
		require.NoError(t, setNestedValue(m, resource.FieldPath{"service", "domains"}, []string{"a.com"}))

		assert.Equal(t, map[string]any{"service": map[string]any{
			"domains": []string{"a.com"},
		}}, m)
	})

	// Expanding an unmarshalable ancestor into an empty map would drop its
	// value while still emitting valid YAML.
	t.Run("ancestor that cannot marshal errors", func(t *testing.T) {
		m := map[string]any{"service": failingMarshaler{}}
		err := setNestedValue(m, resource.FieldPath{"service", "domains"}, []string{"a.com"})
		require.ErrorContains(t, err, "no compact form")
	})
}

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("no compact form")
}
