// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/opencontainers/go-digest"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/image-spec/reference"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/types"
	"unikraft.com/cli/pkg/model"
	"unikraft.com/cli/pkg/model/cmd"
)

type ImageCache struct {
	Ref    types.ImageRef `field:",short" help:"Image reference, such as user/app:tag. An image from another registry keeps its host."`
	Digest digest.Digest  `field:",short" help:"Content digest of the image."`

	Active   []string                    `field:",short" help:"Metros that have the image in their node cache."`
	Pinned   []string                    `field:",short" edit:"add,del" help:"Metros where the image is pinned, so the node cache never evicts it. Give one metro per --add or --del."`
	Autokill map[string]types.DurationMS `field:",long" edit:"add,del=keys" help:"Time without use after which a pinned image is unpinned, per metro, as METRO=DURATION such as fra=24h. A bare number is milliseconds. The metro must be pinned. Give one metro per --add or --del."`

	Images map[string]platform.Image `field:"-" json:"images"`
}

func (ImageCache) Type() model.Type {
	return model.Type{
		Name:  "image-cache",
		Names: "image-caches",
	}
}

// canonical returns the reference of the row together with its digest.
func (i ImageCache) canonical() reference.Reference {
	ref := i.Ref.Reference()
	if i.Digest == "" {
		return ref
	}
	withDigest, err := ref.WithDigest(i.Digest)
	if err != nil {
		return ref
	}
	return withDigest
}

func (i ImageCache) Key() model.Key {
	return model.StringKey(i.canonical().String())
}

func (i ImageCache) Raw() any {
	return i.Images
}

func (i ImageCache) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(i)
}

// load returns one row for each tag of image on metro.
// An image without a tag gives one row with its digest.
func (ImageCache) load(image platform.Image, metro string) ([]ImageCache, error) {
	url := strings.TrimSpace(image.Url)
	if image.Uuid != nil && url == *image.Uuid {
		return nil, nil
	}
	if url == "" {
		return nil, fmt.Errorf("platform image has no url")
	}
	parsed, err := images.ParseRef(url)
	if err != nil {
		return nil, fmt.Errorf("could not parse platform image url %q: %w", url, err)
	}

	var refs []reference.Reference
	if !parsed.Scheme().IsHTTP() {
		base := parsed.WithoutTag().WithoutDigest()
		for _, tag := range image.Tags {
			if strings.HasPrefix(tag, "sha256:") {
				continue
			}
			if _, legacy, ok := strings.Cut(tag, ":"); ok {
				tag = legacy
			}
			if tag == "" || strings.HasPrefix(tag, "sha256:") {
				continue
			}
			ref, err := base.WithTag(tag)
			if err != nil {
				return nil, fmt.Errorf("could not parse platform image tag %q: %w", tag, err)
			}
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		refs = []reference.Reference{parsed}
	}

	rows := make([]ImageCache, 0, len(refs))
	for _, ref := range refs {
		rows = append(rows, ImageCache{
			Ref:    types.NewImageRef(ref),
			Digest: parsed.Digest(),
			Images: map[string]platform.Image{metro: image},
		})
	}
	return rows, nil
}

// mergeImageCache joins the rows of all metros. Each tag stays a row, and
// the rows of one image get the metros of that image.
func mergeImageCache(rows []ImageCache) []ImageCache {
	type imageState struct {
		images map[string]platform.Image
		tagged bool
	}
	states := make(map[string]*imageState)
	ids := make([]string, len(rows))
	for n, row := range rows {
		ids[n] = row.Key().String()
		if row.Digest != "" {
			ids[n] = row.canonical().WithoutTag().String()
		}
		state, ok := states[ids[n]]
		if !ok {
			state = &imageState{images: make(map[string]platform.Image)}
			states[ids[n]] = state
		}
		state.tagged = state.tagged || row.Ref.Reference().Tag() != ""
		for metro, image := range row.Images {
			kept, ok := state.images[metro]
			if !ok || (kept.Persistent == nil && image.Persistent != nil) {
				state.images[metro] = image
			}
		}
	}

	seen := make(map[string]struct{}, len(rows))
	merged := make([]ImageCache, 0, len(rows))
	for n, row := range rows {
		state := states[ids[n]]
		key := row.Key().String()
		if _, ok := seen[key]; ok || (state.tagged && row.Ref.Reference().Tag() == "") {
			continue
		}
		seen[key] = struct{}{}

		row.Images = state.images
		row.Active = slices.Sorted(maps.Keys(state.images))
		row.Pinned, row.Autokill = nil, nil
		for _, metro := range row.Active {
			image := state.images[metro]
			if image.Persistent != nil && *image.Persistent {
				row.Pinned = append(row.Pinned, metro)
			}
			if image.Autokill != nil && image.Autokill.TimeMs > 0 {
				if row.Autokill == nil {
					row.Autokill = make(map[string]types.DurationMS)
				}
				row.Autokill[metro] = types.DurationMS(image.Autokill.TimeMs)
			}
		}
		merged = append(merged, row)
	}
	return merged
}

func (ImageCache) List(ctx context.Context) ([]model.Model, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]ImageCache, error) {
		log.G(ctx).Trace().Msg("listing cached images")
		resp, err := c.GetImages(ctx, nil, platform.GetImagesOpts{})
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
		var rows []ImageCache
		var errs []error
		for _, image := range resp.Data.Images {
			loaded, err := ImageCache{}.load(image, c.Metro.Name)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			rows = append(rows, loaded...)
		}
		return rows, errors.Join(errs...)
	})

	var results []model.Model
	for _, row := range mergeImageCache(rows) {
		results = append(results, row)
	}
	return results, err
}

// Get returns the rows that the keys address. A row key or a key with only a
// digest gives one row. A tag can give more than one row.
func (ImageCache) Get(ctx context.Context, keys []string) ([]model.Model, error) {
	patterns := make([]reference.Reference, len(keys))
	for n, key := range keys {
		parsed, err := images.ParseRef(key)
		if err != nil {
			return nil, fmt.Errorf("could not parse image key %q: %w", key, err)
		}
		patterns[n] = parsed.WithDefaultTag()
	}

	rows, listErr := ImageCache{}.List(ctx)

	var results []model.Model
	var missing group.Refs
	selected := make(map[string]struct{})
	for n, pattern := range patterns {
		var matches []model.Model
		for _, row := range rows {
			canonical := row.(ImageCache).canonical()
			if canonical.String() == pattern.String() {
				matches = []model.Model{row}
				break
			}
			if canonical.Matches(pattern) {
				matches = append(matches, row)
			}
		}
		if pattern.Tag() == "" && len(matches) > 1 {
			matches = matches[:1]
		}
		if len(matches) == 0 {
			missing = append(missing, group.Ref{Name: keys[n]})
		}
		for _, match := range matches {
			key := match.Key().String()
			if _, ok := selected[key]; ok {
				continue
			}
			selected[key] = struct{}{}
			results = append(results, match)
		}
	}

	if len(missing) > 0 {
		if listErr != nil {
			return results, listErr
		}
		return results, multimetro.NotFound(group.ErrRefNotFound{Refs: missing})
	}
	if listErr != nil {
		log.G(ctx).Warn().Err(listErr).Msg("could not list the cached images of all metros")
	}
	return results, nil
}

// requests returns the pin and unpin request of each metro in known for the
// edit in fields. A pin request keeps the autokill that the image has on the metro.
func (i ImageCache) requests(fields []model.Field, known []string) (map[string]platform.PinImageRequestItem, map[string]platform.UnpinImageRequestItem, error) {
	var pin, unpin, noAutokill []string
	var autokill map[string]types.DurationMS
	for path, field := range model.IterFields(fields) {
		if field.Edit == nil {
			continue
		}
		switch path.String() {
		case "pinned":
			pin, _ = field.Edit.Add.([]string)
			unpin, _ = field.Edit.Del.([]string)
		case "autokill":
			autokill, _ = field.Edit.Add.(map[string]types.DurationMS)
			noAutokill, _ = field.Edit.Del.([]string)
		}
	}
	autokillMetros := slices.Sorted(maps.Keys(autokill))
	if err := checkMetros(known, slices.Concat(pin, unpin, autokillMetros, noAutokill)); err != nil {
		return nil, nil, err
	}

	for _, metro := range autokillMetros {
		if autokill[metro] <= 0 {
			return nil, nil, fmt.Errorf("autokill must be greater than 0, use --del autokill=%s to remove it", metro)
		}
	}
	for _, metro := range slices.Concat(autokillMetros, noAutokill) {
		pinned := slices.Contains(i.Pinned, metro) && !slices.Contains(unpin, metro)
		if !pinned && !slices.Contains(pin, metro) {
			return nil, nil, fmt.Errorf("autokill on metro %q needs the image pinned there", metro)
		}
	}

	unpins := make(map[string]platform.UnpinImageRequestItem)
	for _, metro := range unpin {
		image, ok := i.Images[metro]
		if !ok {
			return nil, nil, fmt.Errorf("image is not cached on metro %q", metro)
		}
		req := platform.UnpinImageRequestItem{Uuid: image.Uuid}
		if image.Uuid == nil {
			req.Url = new(image.Url)
		}
		unpins[metro] = req
	}

	pins := make(map[string]platform.PinImageRequestItem)
	for _, metro := range slices.Concat(pin, autokillMetros, noAutokill) {
		req := platform.PinImageRequestItem{Url: i.Ref.String(), TimeoutS: -1}
		if i.Digest != "" {
			req.Url = i.canonical().WithoutTag().String()
		}
		value, set := autokill[metro]
		if !set && !slices.Contains(noAutokill, metro) {
			value = i.Autokill[metro]
		}
		if value > 0 {
			req.Autokill = &platform.PinImageRequestItemAutokill{TimeMs: uint64(value)}
		}
		pins[metro] = req
	}
	return pins, unpins, nil
}

// checkMetros returns an error for the first of metros that is not in known.
func checkMetros(known, metros []string) error {
	for _, metro := range metros {
		if !slices.Contains(known, metro) {
			return fmt.Errorf("unknown metro %q, the profile has %v", metro, known)
		}
	}
	return nil
}

// imageCacheApply sends the pin and unpin request of each metro.
func imageCacheApply(ctx context.Context, g *group.Group[multimetro.MetroClient], pins map[string]platform.PinImageRequestItem, unpins map[string]platform.UnpinImageRequestItem) error {
	metros := slices.AppendSeq(slices.Collect(maps.Keys(pins)), maps.Keys(unpins))
	slices.Sort(metros)
	metros = slices.Compact(metros)
	return group.DoMetros(ctx, g, metros, func(ctx context.Context, c multimetro.MetroClient) error {
		if req, ok := unpins[c.Metro.Name]; ok {
			log.G(ctx).Trace().Msg("unpinning image")
			if _, err := c.UnpinImages(ctx, []platform.UnpinImageRequestItem{req}); err != nil {
				return err
			}
		}
		if req, ok := pins[c.Metro.Name]; ok {
			log.G(ctx).Trace().Msg("pinning image")
			if _, err := c.PinImages(ctx, []platform.PinImageRequestItem{req}); err != nil {
				return err
			}
		}
		return nil
	})
}

// Edit pins and unpins the image on metros, and changes its autokill.
// A change of the autokill pins the image again with the new value.
func (ImageCache) Edit(ctx context.Context, key string, fields []model.Field) error {
	rows, err := ImageCache{}.Get(ctx, []string{key})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected one cached image for %s, found %d", key, len(rows))
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	pins, unpins, err := rows[0].(ImageCache).requests(fields, g.Names())
	if err != nil {
		return err
	}
	if err := imageCacheApply(ctx, g, pins, unpins); err != nil {
		return err
	}
	var autokill []string
	for metro, req := range pins {
		if req.Autokill != nil {
			autokill = append(autokill, metro)
		}
	}
	if len(autokill) == 0 {
		return nil
	}
	rows, err = ImageCache{}.Get(ctx, []string{key})
	if err == nil && len(rows) == 1 {
		warnAutokill(ctx, rows[0].(ImageCache), autokill)
	}
	return nil
}

// warnAutokill logs a warning for each of metros on which the image is pinned
// and the metro does not report an autokill.
func warnAutokill(ctx context.Context, image ImageCache, metros []string) {
	for _, metro := range metros {
		if slices.Contains(image.Pinned, metro) && image.Autokill[metro] == 0 {
			log.G(ctx).Warn().Str("metro", metro).Msg("the metro did not report an autokill for the pinned image")
		}
	}
}

// pinRequests returns the pin request of image for each metro. A metro looks
// up a tag in the registry again, so it pins the image that the tag names now.
func pinRequests(image string, metros []string, autokill types.DurationMS) (map[string]platform.PinImageRequestItem, error) {
	var ref types.ImageRef
	if err := ref.UnmarshalText([]byte(image)); err != nil {
		return nil, err
	}
	url := ref.Reference()
	var policy *platform.PullPolicy
	if url.Digest() != "" {
		url = url.WithoutTag()
	} else {
		policy = new(platform.PullPolicyAlways)
	}
	pins := make(map[string]platform.PinImageRequestItem, len(metros))
	for _, metro := range metros {
		req := platform.PinImageRequestItem{Url: url.String(), PullPolicy: policy, TimeoutS: -1}
		if autokill > 0 {
			req.Autokill = &platform.PinImageRequestItemAutokill{TimeMs: uint64(autokill)}
		}
		pins[metro] = req
	}
	return pins, nil
}

func (ImageCache) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeEdit: {
			{
				Description: "Pin an image on a metro, and unpin it after 24 hours without use",
				Commands:    []string{"unikraft image edit myuser/app:v2 --add pinned=sfo --add autokill=sfo=24h"},
			},
			{
				Description: "Change the autokill of a pinned image",
				Commands:    []string{"unikraft image edit myuser/app:v2 --add autokill=fra=48h"},
			},
			{
				Description: "Remove the autokill of a pinned image",
				Commands:    []string{"unikraft image edit myuser/app:v2 --del autokill=fra"},
			},
			{
				Description: "Unpin an image on a metro",
				Commands:    []string{"unikraft image edit myuser/app:v2 --del pinned=sfo"},
			},
		},
	}
}

// ImagesPinCmd pins an image on metros. The image does not have to be
// in a node cache.
type ImagesPinCmd struct {
	Image    string           `arg:"" completion-predictor:"resource-key-cached-image" help:"Image reference to pin. The metro looks up a tag in the registry again, and pulls the image when it is not in the node cache."`
	Metro    []string         `required:"" completion-predictor:"resource-key-metro" placeholder:"metro" help:"Metros to pin the image on."`
	Autokill types.DurationMS `placeholder:"duration" help:"Unpin the image after this time without use. A bare number is milliseconds. Without it the image stays pinned." example:"24h"`

	cmd.FormatOpts
}

func (ImagesPinCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Pin an image on two metros",
			Commands:    []string{"unikraft image pin myuser/app:v2 --metro fra,sfo"},
		},
		{
			Description: "Pin an image, and unpin it after 24 hours without use",
			Commands:    []string{"unikraft image pin docker.io/library/nginx:1.27 --metro fra --autokill 24h"},
		},
	}
}

func (c *ImagesPinCmd) Run(ctx context.Context, stdio xstdio.Stdio) error {
	if c.Autokill < 0 {
		return fmt.Errorf("autokill must not be negative")
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	if err := checkMetros(g.Names(), c.Metro); err != nil {
		return err
	}
	pins, err := pinRequests(c.Image, c.Metro, c.Autokill)
	if err != nil {
		return err
	}
	if err := imageCacheApply(ctx, g, pins, nil); err != nil {
		return err
	}
	rows, err := ImageCache{}.Get(ctx, []string{c.Image})
	if _, missing := errors.AsType[model.ErrNotFound](err); missing {
		log.G(ctx).Warn().Err(err).Str("image", c.Image).Msg("image pinned, but the cache does not list it under this reference")
		return nil
	}
	if err != nil {
		return fmt.Errorf("pinned the image, but could not list the cache: %w", err)
	}
	if c.Autokill > 0 {
		for _, row := range rows {
			warnAutokill(ctx, row.(ImageCache), c.Metro)
		}
	}
	return c.Output.
		WithDefault(cmd.PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, c.Field, ImageCache{}, rows...)
}

// ImagesUnpinCmd unpins a cached image on metros.
type ImagesUnpinCmd struct {
	Image string   `arg:"" completion-predictor:"resource-key-cached-image" help:"Reference of the cached image to unpin."`
	Metro []string `required:"" completion-predictor:"resource-key-metro" placeholder:"metro" help:"Metros to unpin the image on."`

	cmd.FormatOpts
}

func (ImagesUnpinCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Unpin an image on two metros",
			Commands:    []string{"unikraft image unpin docker.io/library/nginx:1.27 --metro fra,sfo"},
		},
	}
}

// Run unpins, on each metro of c.Metro, the row of c.Image that is pinned there.
func (c *ImagesUnpinCmd) Run(ctx context.Context, stdio xstdio.Stdio, partition *model.Partition) error {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	if err := checkMetros(g.Names(), c.Metro); err != nil {
		return err
	}
	rows, err := ImageCache{}.Get(ctx, []string{c.Image})
	if err != nil {
		return err
	}
	var targets []string
	dels := make(map[string][]map[string]string)
	for _, metro := range c.Metro {
		var pinned []string
		for _, row := range rows {
			if slices.Contains(row.(ImageCache).Pinned, metro) {
				pinned = append(pinned, row.Key().String())
			}
		}
		switch len(pinned) {
		case 0:
			return fmt.Errorf("image is not pinned on metro %q", metro)
		case 1:
		default:
			return fmt.Errorf("more than one image is pinned on metro %q as %s: %s", metro, c.Image, strings.Join(pinned, ", "))
		}
		if _, ok := dels[pinned[0]]; !ok {
			targets = append(targets, pinned[0])
		}
		dels[pinned[0]] = append(dels[pinned[0]], map[string]string{"pinned": metro})
	}

	for _, target := range targets {
		editCmd := &cmd.ModelEditCmd[ImageCache]{
			Target:     target,
			Del:        dels[target],
			FormatOpts: c.FormatOpts,
		}
		if err := editCmd.Run(ctx, stdio, partition); err != nil {
			return err
		}
	}
	return nil
}
