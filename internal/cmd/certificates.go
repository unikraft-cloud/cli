// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/types"
)

type CertificatesCmd struct {
	cmd.ModelCmd[Certificate]
	cmd.GettableModelCmd[Certificate]
	cmd.WaitableModelCmd[Certificate]
	cmd.ListableModelCmd[Certificate]
	cmd.BulkDeletableModelCmd[Certificate]

	Create cmd.ModelCreateCmd[Certificate] `cmd:"" help:"Create a certificate."`
	Edit   cmd.ModelEditCmd[Certificate]   `cmd:"" help:"Replace the chain and private key of an uploaded certificate."`
}

type Certificate struct {
	Metro LinkName[Metro] `field:"metro,short" create:"set,required" flag:"metro" help:"Metro to create in. Defaults to the profile's default metro." placeholder:"metro" example:"fra,sfo"`
	Name  string          `mirror:"certificate.name" field:",short" create:"set" flag:"name" help:"Certificate name. Generated when omitted." placeholder:"name"`
	UUID  string          `mirror:"certificate.uuid" field:",long" help:"Unique identifier assigned at creation."`
	Node  string          `mirror:"certificate.node" field:",long" create:"set" flag:"node" help:"Node to place the certificate on." placeholder:"node"`

	CommonName   string `mirror:"certificate.common_name" field:",short" help:"Common name: the fully qualified domain name the certificate is for."`
	Subject      string `mirror:"certificate.subject" field:",long" help:"Subject distinguished name, present once issued."`
	Issuer       string `mirror:"certificate.issuer" field:",long" help:"Issuing certificate authority."`
	SerialNumber string `mirror:"certificate.serial_number" field:",long" help:"Serial number assigned by the issuer."`

	State types.CertificateState `mirror:"certificate.state" field:",short" help:"Lifecycle state: pending (being issued or validated), valid or error (issuing failed after several attempts)."`

	CN    string `field:"cn,invisible,valueless" create:"set,required" flag:"common-name" aliases:"cn" help:"Common name: the fully qualified domain name the certificate is for, with a trailing dot." placeholder:"fqdn" example:"demo.unikraft.dev."`
	Chain string `field:"chain,invisible,valueless" create:"set,required" edit:"set" flag-file:"chain" help:"PEM certificate chain: the certificate followed by any intermediates. On edit, set it with pkey. Only uploaded certificates can be edited, and the common name must not change." placeholder:"file"`
	Pkey  string `field:"pkey,invisible,valueless" create:"set,required" edit:"set" flag-file:"private-key" aliases:"pkey" help:"PEM private key matching the certificate chain. On edit, set it with chain." placeholder:"file"`

	Timestamps struct {
		Created   types.RelativeTime `mirror:"certificate.created_at" field:",short" help:"Creation time."`
		NotBefore types.RelativeTime `mirror:"certificate.not_before" field:",long" help:"Start of the validity period."`
		NotAfter  types.RelativeTime `mirror:"certificate.not_after" field:",short" help:"Expiry. Platform-issued certificates renew automatically, uploaded ones must be replaced with certificate edit before this time."`
	} `help:"Lifecycle and validity timestamps."`

	Certificate platform.Certificate `field:"-" json:"certificate"`

	key multimetro.Key
}

func (Certificate) Type() model.Type {
	return model.Type{
		Name:  "certificate",
		Names: "certificates",
	}
}

func (c Certificate) Key() model.Key {
	return c.key
}

func (c Certificate) Raw() any {
	return c.Certificate
}

func (c Certificate) Fields(ctx context.Context) ([]model.Field, error) {
	c.Metro = LinkName[Metro](defaultMetro(ctx, string(c.Metro)))
	return model.FieldsFromStruct(c)
}

func (Certificate) List(ctx context.Context) ([]model.Model, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]model.Model, error) {
		log.G(ctx).Trace().Msg("listing certificates")
		resp, err := c.GetCertificates(ctx, nil, platform.GetCertificatesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []model.Model
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
		for _, certificate := range resp.Data.Certificates {
			result, err := Certificate{}.load(nil, certificate, &c.Metro)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (Certificate) Get(ctx context.Context, keys []string) ([]model.Model, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]model.Model, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting certificates")
		resp, err := c.GetCertificates(ctx, refs.NameOrUUIDs(), platform.GetCertificatesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []model.Model
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, certificate := range resp.Data.Certificates {
			if certificate.Status == nil || *certificate.Status != platform.ResponseStatusSuccess {
				continue
			}

			matchedRef := matchRef(refs, certificate.Name, certificate.Uuid)
			result, err := Certificate{}.load(matchedRef, certificate, &c.Metro)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if matchedRef != nil {
				found = append(found, *matchedRef)
			} else {
				found = append(found, group.Ref{Metro: c.Metro.Name, Name: result.Name, UUID: result.UUID})
			}
			results = append(results, result)
		}
		return results, found, errors.Join(errs...)
	})
}

func (Certificate) load(ref *group.Ref, certificate platform.Certificate, metro *config.Metro) (Certificate, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  certificate.Name,
			UUID:  certificate.Uuid,
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, certificate.Name)
		ref.UUID = cmp.Or(ref.UUID, certificate.Uuid)
	}

	result := Certificate{
		Certificate: certificate,
		Metro:       LinkName[Metro](metro.Name),
		key:         multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Certificate{}, fmt.Errorf("could not mirror certificate data: %w", err)
	}
	return result, nil
}

func (Certificate) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting certificates")
		resp, err := c.DeleteCertificates(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
		for _, certificate := range resp.Data.Certificates {
			if certificate.Status != platform.ResponseStatusSuccess {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  certificate.Name,
				UUID:  certificate.Uuid,
			})
		}
		return deleted, nil
	})
}

func (Certificate) Create(ctx context.Context, fields []model.Field) ([]model.Model, error) {
	var req platform.CreateCertificateRequest
	var metro string
	for key, field := range model.IterFields(fields) {
		if field.Create.Set != nil {
			switch key.String() {
			case "name":
				name := field.Create.Set.(string)
				req.Name = &name
			case "node":
				req.Node = new(field.Create.Set.(string))
			case "metro":
				metro = string(field.Create.Set.(LinkName[Metro]))
			case "cn":
				req.CommonName = new(field.Create.Set.(string))
			case "chain":
				req.Chain = field.Create.Set.(string)
			case "pkey":
				req.Pkey = field.Create.Set.(string)
			}
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating certificate")
		resp, err := c.CreateCertificate(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Certificates) == 0 {
			return nil, fmt.Errorf("no certificates created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Certificates))
		for _, certificate := range resp.Data.Certificates {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  certificate.Uuid,
				Name:  certificate.Name,
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := Certificate{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Edit replaces the chain and private key of an uploaded certificate.
// The name does not change. The new chain must have the same common name.
func (Certificate) Edit(ctx context.Context, key string, fields []model.Field) error {
	var chain, pkey string
	for path, field := range model.IterFields(fields) {
		if field.Edit == nil || field.Edit.Set == nil {
			continue
		}
		switch path.String() {
		case "chain":
			chain = field.Edit.Set.(string)
		case "pkey":
			pkey = field.Edit.Set.(string)
		}
	}
	if chain == "" || pkey == "" {
		return errors.New("chain and pkey must be set together")
	}

	ref := multimetro.ParseKey(key).Ref()
	if ref.Metro == "" {
		found, err := Certificate{}.Get(ctx, []string{key})
		if err != nil {
			return err
		}
		if len(found) != 1 {
			return fmt.Errorf("expected one certificate for %s, found %d", key, len(found))
		}
		ref = found[0].(Certificate).key.Ref()
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, group.Refs{ref}, func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateCertificatesRequestItem, 0, len(refs))
		for _, ref := range refs {
			req := platform.UpdateCertificatesRequestItem{Chain: chain, Pkey: pkey}
			if ref.UUID != "" {
				req.Uuid = &ref.UUID
			} else {
				req.Name = &ref.Name
			}
			reqs = append(reqs, req)
		}
		log.G(ctx).Trace().Msg("updating certificate")
		_, err := c.UpdateCertificates(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}

func (Certificate) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Get a certificate by name or UUID",
				Commands:    []string{"unikraft certificate get demo-cert"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all certificates",
				Commands:    []string{"unikraft certificate list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new certificate",
				Commands: []string{
					`openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
  -subj "/CN=demo.unikraft.dev" \
  -keyout cert.key \
  -out cert.pem`,
					// `unikraft certificate create \
					//   --set name=demo-cert \
					//   --set cn=demo.unikraft.dev. \
					//   --set-file chain=cert.pem \
					//   --set-file pkey=cert.key \
					//   --set metro=fra`,
					`unikraft certificate create \
	  --name demo-cert \
	  --cn demo.unikraft.dev. \
	  --chain cert.pem \
	  --pkey cert.key \
	  --metro fra`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Replace a certificate before it expires",
				Commands: []string{
					`unikraft certificate edit demo-cert \
  --chain cert.pem \
  --pkey cert.key`,
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a certificate by name or UUID",
				Commands:    []string{"unikraft certificate delete demo-cert"},
			},
		},
	}
}
