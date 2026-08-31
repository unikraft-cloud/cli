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

	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

type CertificatesCmd struct {
	cmd.ResourceCmd[Certificate]
	cmd.GettableResourceCmd[Certificate]
	cmd.WaitableResourceCmd[Certificate]
	cmd.ListableResourceCmd[Certificate]
	cmd.BulkDeletableResourceCmd[Certificate]

	Create CertificateCreateCmd `cmd:"" help:"Create a certificate."`
}

// CertificateCreateCmd extends the generic resource create command with shortcut
// flags for commonly used certificate fields. Each field tagged with
// `shortcut:"<path>"` or `shortcut-file:"<path>"` is translated into a --set or
// --set-file entry before the standard create pipeline runs.
type CertificateCreateCmd struct {
	cmd.ResourceCreateCmd[Certificate]

	Metro string `group:"flag-create" shortcut:"metro" help:"Metro to create in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name  string `group:"flag-create" shortcut:"name" help:"Certificate name." placeholder:"name"`

	CommonName string `group:"flag-create" shortcut:"cn" help:"Certificate common name." placeholder:"fqdn" example:"demo.unikraft.dev." aliases:"cn"`
	Chain      string `group:"flag-create" shortcut-file:"chain" help:"Certificate chain file." placeholder:"file"`
	PrivateKey string `group:"flag-create" shortcut-file:"pkey" help:"Certificate private key file." placeholder:"file" aliases:"pkey"`
}

func (c *CertificateCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

type Certificate struct {
	Metro LinkName[Metro] `field:"metro,short" create:"set,required"`
	Name  string          `mirror:"certificate.name" field:",short" create:"set"`
	UUID  string          `mirror:"certificate.uuid" field:",long"`

	CommonName   string `mirror:"certificate.common_name" field:",short"`
	Subject      string `mirror:"certificate.subject" field:",long"`
	Issuer       string `mirror:"certificate.issuer" field:",long"`
	SerialNumber string `mirror:"certificate.serial_number" field:",long"`

	State types.CertificateState `mirror:"certificate.state" field:",short"`

	CN    string `field:"cn,invisible,valueless" create:"set,required"`
	Chain string `field:"chain,invisible,valueless" create:"set,required"`
	Pkey  string `field:"pkey,invisible,valueless" create:"set,required"`

	Timestamps struct {
		Created   types.RelativeTime `mirror:"certificate.created_at" field:",short"`
		NotBefore types.RelativeTime `mirror:"certificate.not_before" field:",long"`
		NotAfter  types.RelativeTime `mirror:"certificate.not_after" field:",short"`
	}

	Certificate platform.Certificate `field:"-" json:"certificate"`

	key multimetro.Key
}

func (Certificate) Type() resource.Type {
	return resource.Type{
		Name:  "certificate",
		Names: "certificates",
	}
}

func (c Certificate) Key() resource.Key {
	return c.key
}

func (c Certificate) Raw() any {
	return c.Certificate
}

func (c Certificate) Fields(ctx context.Context) ([]resource.Field, error) {
	c.Metro = LinkName[Metro](defaultMetro(ctx, string(c.Metro)))
	return resource.FieldsFromStruct(c)
}

func (Certificate) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing certificates")
		resp, opErr := c.GetCertificates(ctx, nil, platform.GetCertificatesOpts{Details: new(true)})
		var certificates []platform.Certificate
		if resp != nil && resp.Data != nil {
			certificates = resp.Data.Certificates
		}
		var results []resource.Resource
		errs := []error{opErr}
		for _, certificate := range certificates {
			result, err := Certificate{}.load(nil, certificate, &c.Metro)
			errs = append(errs, err)
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (Certificate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting certificates")
		resp, opErr := c.GetCertificates(ctx, refs.NameOrUUIDs(), platform.GetCertificatesOpts{Details: new(true)})
		var certificates []platform.Certificate
		if resp != nil && resp.Data != nil {
			certificates = resp.Data.Certificates
		}
		var found []group.Ref
		var results []resource.Resource
		errs := []error{ignoreNotFound(opErr)}
		for _, certificate := range certificates {
			// Deliberately do not filter by status: pending certificates should
			// be returned by GET as well.
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
		var deleted []group.Ref
		if resp != nil && resp.Data != nil {
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
		}
		return deleted, ignoreNotFound(err)
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
				metro = string(field.Create.Set.(LinkName[Metro]))
			case "cn":
				req.Cn = new(field.Create.Set.(string)) //nolint:staticcheck // CommonName not on stable yet
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
	return Certificate{}.Get(ctx, keys.Strings())
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
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a certificate by name or UUID",
				Commands:    []string{"unikraft certificate delete demo-cert"},
			},
		},
	}
}
