// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package volimport

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/timeouts"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
)

func deleteInstanceByUUID(ctx context.Context, c multimetro.MetroClient, instUUID string) error {
	req := platform.DeleteInstanceByUUIDRequestBody{TimeoutS: new(int64(-1))}
	_, err := timeouts.TryWithFallback(ctx, req,
		func(ctx context.Context, req platform.DeleteInstanceByUUIDRequestBody) (*platform.Response[platform.DeleteInstancesResponseData], error) {
			return c.DeleteInstanceByUUID(ctx, instUUID, req)
		})
	return err
}

const (
	internalPort = uint32(42069)
	memoryMB     = int64(128)
)

// Start creates a short-lived Unikraft Cloud instance running the
// volimport service with the given volume attached at /mnt, and returns the
// instance UUID and the public FQDN on which the import service listens.
func Start(ctx context.Context, c multimetro.MetroClient, image, volUUID, authStr string, timeoutS uint64, servicePort int) (instUUID, fqdn string, err error) {
	args := []string{
		"volimport",
		"-p", strconv.FormatUint(uint64(internalPort), 10),
		"-a", authStr,
		"-t", strconv.FormatUint(timeoutS, 10),
	}
	destPort := internalPort
	req := platform.CreateInstanceRequest{
		Image:         &platform.ImageSpec{Url: image},
		MemoryMb:      new(memoryMB),
		Args:          args,
		Autostart:     new(true),
		TimeoutS:      new(int64(-1)),
		RestartPolicy: new(platform.InstanceRestartPolicyNever),
		Features:      []platform.InstanceFeature{platform.InstanceFeatureDeleteOnStop},
		Volumes: []platform.CreateInstanceRequestVolume{{
			Uuid: &volUUID,
			At:   "/mnt",
		}},
		ServiceGroup: &platform.CreateInstanceRequestServiceGroup{
			Services: []platform.Service{{
				Port:            uint32(servicePort),
				DestinationPort: &destPort,
				Handlers:        []platform.ConnectionHandler{platform.ConnectionHandlerTls},
			}},
		},
	}

	log.G(ctx).Trace().Msg("creating volume data import instance")
	resp, err := timeouts.TryWithFallback(ctx, req, c.CreateInstance)
	if err != nil {
		return "", "", fmt.Errorf("creating volume data import instance: %w", err)
	}
	if len(resp.Data.Instances) == 0 {
		return "", "", fmt.Errorf("no instance created by the API")
	}

	inst := resp.Data.Instances[0]
	instUUID = inst.Uuid

	if inst.ServiceGroup == nil || len(inst.ServiceGroup.Domains) == 0 {
		if instUUID != "" {
			log.G(ctx).Trace().Str("uuid", instUUID).Msg("deleting instance: no service group domain returned")
			_ = deleteInstanceByUUID(ctx, c, instUUID)
		}
		return "", "", fmt.Errorf("import instance has no service group domain")
	}

	fqdn = inst.ServiceGroup.Domains[0].Fqdn
	if fqdn == "" {
		if instUUID != "" {
			log.G(ctx).Trace().Str("uuid", instUUID).Msg("deleting instance: empty FQDN returned")
			_ = deleteInstanceByUUID(ctx, c, instUUID)
		}
		return "", "", fmt.Errorf("import instance has an empty FQDN")
	}
	// Strip trailing dot if present (DNS FQDNs conventionally end with ".").
	fqdn = strings.TrimSuffix(fqdn, ".")

	return instUUID, fqdn, nil
}

// Terminate deletes the given instance, ignoring "not found" errors.
func Terminate(ctx context.Context, c multimetro.MetroClient, instUUID string) error {
	log.G(ctx).Trace().Str("uuid", instUUID).Msg("deleting volume data import instance")
	err := deleteInstanceByUUID(ctx, c, instUUID)
	if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		return fmt.Errorf("deleting volume data import instance: %w", err)
	}
	return nil
}

// Wait waits for the given instance to reach the stopped state.
func Wait(ctx context.Context, c multimetro.MetroClient, instUUID string) error {
	req := platform.WaitInstanceByUUIDRequestBody{
		State:    platform.InstanceStateStopped,
		TimeoutS: new(int64(-1)),
	}
	_, err := timeouts.TryWithFallback(ctx, req,
		func(ctx context.Context, req platform.WaitInstanceByUUIDRequestBody) (*platform.Response[platform.WaitInstancesResponseData], error) {
			return c.WaitInstanceByUUID(ctx, instUUID, req)
		})
	if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		return fmt.Errorf("waiting for import instance to stop: %w", err)
	}
	return nil
}

// GenRandAuth generates a 32-character cryptographically random alphanumeric
// token used to authenticate with the volimport unikernel.
func GenRandAuth() (string, error) {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const length = 32
	maxIdx := big.NewInt(int64(len(charset)))
	var buf strings.Builder
	buf.Grow(length)
	for range length {
		idx, err := rand.Int(rand.Reader, maxIdx)
		if err != nil {
			return "", err
		}
		buf.WriteByte(charset[idx.Int64()])
	}
	return buf.String(), nil
}
