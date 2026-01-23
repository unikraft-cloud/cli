// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	xslices "unikraft.com/cli/internal/x/slices"
)

type CertificatesCmd struct {
	cmd.ResourceCmd[Certificate]
	cmd.GettableResourceCmd[Certificate]  `set:"name=certificate" set:"names=certificates"`
	cmd.ListableResourceCmd[Certificate]  `set:"name=certificate" set:"names=certificates"`
	cmd.DeletableResourceCmd[Certificate] `set:"name=certificate" set:"names=certificates"`
	cmd.CreatableResourceCmd[Certificate] `set:"name=certificate" set:"names=certificates"`
}

type Certificate struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"certificate.name" field:",short" create:"set"`
	UUID      string `mirror:"certificate.uuid" field:",long"`

	CN         string `field:"cn,invisible" create:"set,required"`
	Chain      string `field:"chain,invisible" create:"set,required"`
	PrivateKey string `field:"pkey,invisible" create:"set,required"`

	CommonName   string `mirror:"certificate.common_name" field:",short"`
	Subject      string `mirror:"certificate.subject" field:",long"`
	Issuer       string `mirror:"certificate.issuer" field:",long"`
	SerialNumber string `mirror:"certificate.serial_number" field:",long"`

	State CertificateState `mirror:"certificate.state" field:",short"`

	Timestamps struct {
		CreatedAt time.Time `mirror:"certificate.created_at"`
		NotBefore time.Time `mirror:"certificate.not_before"`
		NotAfter  time.Time `mirror:"certificate.not_after"`
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
		Metro: c.Metro.Name,
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
	cl, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := multimetro.DoAll(ctx, cl, func(ctx context.Context, mc *multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing certificates")
		resp, err := mc.GetCertificates(ctx, nil, true)
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		for _, certificate := range resp.Data.Certificates {
			result, err := Certificate{}.load(certificate, &mc.Metro)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil
	})
	if err != nil {
		return nil, err
	}
	return xslices.Flatten(resources), nil
}

func (Certificate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	cl, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	resources, err := multimetro.DoKeys(ctx, cl, multimetro.ParseKeys(keys), func(ctx context.Context, mc *multimetro.MetroClient, keys multimetro.Keys) ([]resource.Resource, []multimetro.Key, error) {
		log.G(ctx).Trace().Msg("getting certificates")
		resp, err := mc.GetCertificates(ctx, keys.NamesOrUUIDs(), true)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []multimetro.Key
		var results []resource.Resource
		for _, certificate := range resp.Data.Certificates {
			if certificate.Status == nil || *certificate.Status != "success" {
				continue
			}
			result, err := Certificate{}.load(certificate, &mc.Metro)
			if err != nil {
				return nil, nil, err
			}
			found = append(found, multimetro.Key{
				Metro: mc.Metro.Name,
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, nil
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
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

	cl, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	_, err = multimetro.DoKeys(ctx, cl, keys, func(ctx context.Context, metroClient *multimetro.MetroClient, keys multimetro.Keys) ([]struct{}, []multimetro.Key, error) {
		log.G(ctx).Trace().Msg("deleting certificates")
		var deleted []multimetro.Key
		for _, key := range keys {
			_, err := metroClient.DeleteCertificateByUUID(ctx, key.UUID)
			if err != nil {
				return nil, nil, err
			}
			deleted = append(deleted, key)
		}
		return nil, deleted, nil
	})
	return err
}

func (Certificate) Create(ctx context.Context, fields []resource.Field) (resource.Resource, error) {
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

	cl, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	uuid, err := multimetro.DoMetro(ctx, cl, metro, func(ctx context.Context, mc *multimetro.MetroClient) (string, error) {
		log.G(ctx).Trace().Msg("creating certificate")
		resp, err := mc.CreateCertificate(ctx, req)
		if err != nil {
			return "", err
		}
		return *resp.Data.Certificates[0].Uuid, nil
	})
	if err != nil {
		return nil, err
	}

	key := multimetro.Key{
		Metro: metro,
		UUID:  uuid,
	}
	results, err := Certificate{}.Get(ctx, []string{key.String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}
