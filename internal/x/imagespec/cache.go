// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package imagespec

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	spec "unikraft.com/x/image-spec"
	"unikraft.com/x/log"
)

// WrapCached wraps a File with caching support. If the file has a backing
// provider and descriptor, it returns a new ContentStoreFile that uses a
// pull-through cache at ~/.cache/unikraft. Otherwise, returns the original file.
func WrapCached(ctx context.Context, file spec.File) spec.File {
	if file == nil {
		return nil
	}

	desc, provider := file.Source()
	if desc.Digest == "" || provider == nil {
		return file
	}

	store, err := cacheStore()
	if err != nil {
		log.G(ctx).Debug().Err(err).Msg("failed to initialize content cache")
		return file
	}

	return spec.NewContentStoreFile(
		pullThroughProvider{cache: store, upstream: provider},
		desc,
		file.Path(),
	)
}

type pullThroughProvider struct {
	cache    content.Store
	upstream content.Provider
}

func (p pullThroughProvider) ReaderAt(ctx context.Context, desc ocispec.Descriptor) (content.ReaderAt, error) {
	if p.cache != nil {
		ra, err := p.cache.ReaderAt(ctx, desc)
		if err == nil {
			log.G(ctx).Debug().
				Str("digest", desc.Digest.String()).
				Msg("content cache hit")
			return ra, nil
		}
		if !errdefs.IsNotFound(err) {
			log.G(ctx).Debug().
				Err(err).
				Str("digest", desc.Digest.String()).
				Msg("content cache read failed")
		}
	}

	ra, err := p.upstream.ReaderAt(ctx, desc)
	if err != nil {
		return nil, err
	}

	if p.cache != nil {
		if err := cacheBlob(ctx, p.cache, ra, desc); err != nil {
			log.G(ctx).Debug().
				Err(err).
				Str("digest", desc.Digest.String()).
				Msg("failed to write content to cache")
			return ra, nil
		}
		// Close the upstream reader and return from the cache instead
		ra.Close()
		return p.cache.ReaderAt(ctx, desc)
	}

	return ra, nil
}

func cacheBlob(ctx context.Context, store content.Store, ra content.ReaderAt, desc ocispec.Descriptor) error {
	if desc.Digest == "" {
		return nil
	}
	if desc.Size <= 0 {
		desc.Size = ra.Size()
	}
	if desc.Size <= 0 {
		return nil
	}
	return content.WriteBlob(ctx, store, desc.Digest.String(), content.NewReader(ra), desc)
}

var (
	cacheStoreOnce sync.Once
	cacheStoreInst content.Store
	cacheStoreErr  error
)

func cacheStore() (content.Store, error) {
	cacheStoreOnce.Do(func() {
		root, err := cacheRoot()
		if err != nil {
			cacheStoreErr = err
			return
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			cacheStoreErr = err
			return
		}
		cacheStoreInst, cacheStoreErr = local.NewStore(root)
	})
	return cacheStoreInst, cacheStoreErr
}

func cacheRoot() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "unikraft"), nil
}
