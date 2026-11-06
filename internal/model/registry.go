// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package model

type Registry struct {
	entries []*ModelDescriptor
	byName  map[string]*ModelDescriptor
}

type ModelDescriptor struct {
	Name  string
	Names string
	List  ListableModel
	Get   GettableModel
}

func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]*ModelDescriptor),
	}
}

func (r *Registry) Register(list ListableModel, get GettableModel) *ModelDescriptor {
	if list == nil {
		return nil
	}
	info := list.Type()
	desc := &ModelDescriptor{
		Name:  info.Name,
		Names: info.Names,
		List:  list,
		Get:   get,
	}
	r.entries = append(r.entries, desc)
	r.byName[info.Name] = desc
	r.byName[info.Names] = desc
	return desc
}

func (r *Registry) Entries() []*ModelDescriptor {
	return r.entries
}

func (r *Registry) Resolve(name string) (*ModelDescriptor, bool) {
	if name == "" {
		return nil, false
	}
	desc, ok := r.byName[name]
	return desc, ok
}
