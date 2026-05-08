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
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string          `mirror:"certificate.name" field:",short" create:"set"`
	UUID      string          `mirror:"certificate.uuid" field:",long"`

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
	Metro       *config.Metro        `field:"-" json:"metro"`

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
	c.MetroName = LinkName[Metro](defaultMetro(ctx, string(c.MetroName)))
	return resource.FieldsFromStruct(c)
}

func (Certificate) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing certificates")
		resp, err := c.GetCertificates(ctx, nil, platform.GetCertificatesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
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

func (Certificate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting certificates")
		resp, err := c.GetCertificates(ctx, refs.NameOrUUIDs(), platform.GetCertificatesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, certificate := range resp.Data.Certificates {
			result, err := Certificate{}.load(&refs[i], certificate, &c.Metro)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			found = append(found, group.Ref{
				Metro: c.Metro.Name,
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, errors.Join(errs...)
	})
}

func (Certificate) load(ref *group.Ref, certificate platform.Certificate, metro *config.Metro) (Certificate, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(certificate.Name),
			UUID:  ptr.ZeroIfNil(certificate.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(certificate.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(certificate.Uuid))
	}

	result := Certificate{
		Certificate: certificate,
		Metro:       metro,
		key:         multimetro.Key(*ref),
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
		keys = append(keys, certificate.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting certificates")
		resp, err := c.DeleteCertificates(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, certificate := range resp.Data.Certificates {
			if certificate.Status == nil || *certificate.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  ptr.ZeroIfNil(certificate.Name),
				UUID:  ptr.ZeroIfNil(certificate.Uuid),
			})
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
		if len(resp.Data.Certificates) == 0 {
			return nil, fmt.Errorf("no certificates created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Certificates))
		for _, certificate := range resp.Data.Certificates {
			key := multimetro.Key{
				Metro: c.Metro.Name,
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
				Description: "Inspect a certificate by name",
				Commands:    []string{"unikraft certificate get demo-cert"},
			},
			{
				Description: "Inspect a certificate by UUID",
				Commands:    []string{"unikraft certificate get d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Inspect a certificate in a specific metro",
				Commands:    []string{"unikraft certificate get fra/demo-cert"},
			},
			{
				Description: "Show certificate details in JSON format",
				Commands:    []string{"unikraft certificate get demo-cert -o json"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all certificates across metros",
				Commands:    []string{"unikraft certificate list"},
			},
			{
				Description: "List certificates and watch for changes",
				Commands:    []string{"unikraft certificate list -w"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a self-signed certificate and upload it",
				Commands: []string{
					`openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
  -subj "/CN=demo.unikraft.dev" \
  -keyout cert.key \
  -out cert.pem`,
					`unikraft certificate create \
	  --name demo-cert \
	  --cn demo.unikraft.dev. \
	  --chain cert.pem \
	  --pkey cert.key \
	  --metro fra`,
				},
			},
			{
				Description: "Preview certificate creation without applying",
				Commands: []string{
					`unikraft certificate create \
	  --cn example.com. \
	  --chain cert.pem \
	  --pkey cert.key \
	  --metro sfo \
	  --dry-run`,
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a certificate by name",
				Commands:    []string{"unikraft certificate delete demo-cert"},
			},
			{
				Description: "Delete a certificate by UUID",
				Commands:    []string{"unikraft certificate delete d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Delete all certificates (with confirmation prompt)",
				Commands:    []string{"unikraft certificate delete --all"},
			},
		},
	}
}
