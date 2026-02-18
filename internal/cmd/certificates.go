// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

type CertificatesCmd struct {
	cmd.ResourceCmd[Certificate]
	cmd.GettableResourceCmd[Certificate]      `set:"name=certificate" set:"names=certificates"`
	cmd.WaitableResourceCmd[Certificate]      `set:"name=certificate" set:"names=certificates"`
	cmd.ListableResourceCmd[Certificate]      `set:"name=certificate" set:"names=certificates"`
	cmd.BulkDeletableResourceCmd[Certificate] `set:"name=certificate" set:"names=certificates"`
	cmd.CreatableResourceCmd[Certificate]     `set:"name=certificate" set:"names=certificates"`
}

type Certificate struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"certificate.name" field:",short" create:"set"`
	UUID      string `mirror:"certificate.uuid" field:",long"`

	// create-only fields
	CN         string `field:"cn,invisible" create:"set,required"`
	Chain      string `field:"chain,invisible" create:"set,required"`
	PrivateKey string `field:"pkey,invisible" create:"set,required"`

	CommonName   string `mirror:"certificate.common_name" field:",short"`
	Subject      string `mirror:"certificate.subject" field:",long"`
	Issuer       string `mirror:"certificate.issuer" field:",long"`
	SerialNumber string `mirror:"certificate.serial_number" field:",long"`

	State types.CertificateState `mirror:"certificate.state" field:",short"`

	Timestamps struct {
		Created   types.RelativeTime `mirror:"certificate.created_at" field:",short"`
		NotBefore types.RelativeTime `mirror:"certificate.not_before" field:",long"`
		NotAfter  types.RelativeTime `mirror:"certificate.not_after" field:",short"`
	}

	Certificate platform.Certificate `field:"-" json:"certificate"`
	Metro       *config.Metro        `field:"-" json:"metro"`
}

func (Certificate) Type() resource.Type {
	return resource.Type{
		Name:  "certificate",
		Names: "certificates",
	}
}

func (c Certificate) key() multimetro.Key {
	return multimetro.Key{
		Metro: c.MetroName,
		Name:  c.Name,
		UUID:  c.UUID,
	}
}

func (c Certificate) Key() resource.Key {
	return c.key()
}

func (c Certificate) Raw() any {
	return c.Certificate
}

func (c Certificate) Fields() ([]resource.Field, error) {
	return resource.FieldsFromStruct(c)
}

func (Certificate) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing certificates")
		resp, err := c.GetCertificates(ctx, nil, true)
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		for _, certificate := range resp.Data.Certificates {
			result, err := Certificate{}.load(certificate, &c.Metro)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil
	})
}

func (Certificate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting certificates")
		resp, err := c.GetCertificates(ctx, refs.NameOrUUIDs(), true)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		for _, certificate := range resp.Data.Certificates {
			if certificate.Status == nil || *certificate.Status != "success" {
				continue
			}
			result, err := Certificate{}.load(certificate, &c.Metro)
			if err != nil {
				return nil, nil, err
			}
			found = append(found, group.Ref{
				Metro: c.Metro.Name.String(),
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, nil
	})
}

func (Certificate) load(certificate platform.Certificate, metro *config.Metro) (Certificate, error) {
	result := Certificate{
		Certificate: certificate,
		Metro:       metro,
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Certificate{}, fmt.Errorf("could not mirror certificate data: %w", err)
	}
	return result, nil
}

func (Certificate) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		certificate := target.(Certificate)
		keys = append(keys, certificate.key())
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting certificates")
		var deleted []group.Ref
		for _, key := range refs {
			_, err := c.DeleteCertificateByUUID(ctx, key.UUID)
			if err != nil {
				return nil, err
			}
			deleted = append(deleted, key)
		}
		return deleted, nil
	})
}

func (Certificate) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateCertificateRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create.Set != nil {
			switch key.String() {
			case "name":
				name := field.Create.Set.(string)
				req.Name = &name
			case "metro":
				metro = field.Create.Set.(string)
			case "cn":
				req.Cn = field.Create.Set.(string)
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
		if len(resp.Data.Certificates) == 0 {
			return nil, fmt.Errorf("no certificates created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Certificates))
		for _, certificate := range resp.Data.Certificates {
			key := multimetro.Key{
				Metro: c.Metro.Name.String(),
				UUID:  ptr.ZeroIfNil(certificate.Uuid),
				Name:  ptr.ZeroIfNil(certificate.Name),
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
					`unikraft certificate create \
  --set name=demo-cert \
  --set cn=demo.unikraft.dev. \
  --set-file chain=cert.pem \
  --set-file pkey=cert.key \
  --set metro=fra`,
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
