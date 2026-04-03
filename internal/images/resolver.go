// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	dockerconfig "github.com/docker/cli/cli/config"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/version"
)

func Resolver(profile *config.Profile) remotes.Resolver {
	headers := http.Header{}
	headers.Set("User-Agent", version.UserAgent())

	var indexes []config.Index
	if profile != nil {
		indexes = make([]config.Index, len(profile.Metros))
		for i, metro := range profile.Metros {
			indexes[i] = metro.Index()
		}
	}

	httpHost := func(host string) (bool, error) {
		for _, index := range indexes {
			if host == index.Host {
				return index.HTTP, nil
			}
		}
		return false, nil
	}
	insecureHost := func(host string) (bool, error) {
		for _, index := range indexes {
			if host == index.Host {
				return index.Insecure, nil
			}
		}
		return false, nil
	}

	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)
	opts := []docker.RegistryOpt{
		docker.WithAuthorizer(docker.NewDockerAuthorizer(docker.WithAuthCreds(func(hostname string) (string, string, error) {
			username, password, err := hostCreds(profile, hostname)
			if err != nil {
				return "", "", err
			}
			if username != "" || password != "" {
				return username, password, nil
			}

			auth, err := dockerConfig.GetAuthConfig(hostname)
			if err != nil {
				return "", "", err
			}
			if auth.IdentityToken != "" {
				return "", auth.IdentityToken, nil
			}
			return auth.Username, auth.Password, nil
		},
		))),
		docker.WithPlainHTTP(httpHost),
	}

	dro := docker.ResolverOptions{
		Headers: headers,
		Hosts: fallbackHost(
			insecureHosts(docker.ConfigureDefaultRegistries(opts...), insecureHost),
			docker.ConfigureDefaultRegistries(append(opts, docker.WithHostTranslator(func(s string) (string, error) {
				if profile != nil {
					for _, metro := range profile.Metros {
						if s == metro.Index().Host {
							return DefaultRegistry, nil
						}
					}
				}
				return s, nil
			}))...),
		),
	}
	return docker.NewResolver(dro)
}

func fallbackHost(registryHosts ...docker.RegistryHosts) docker.RegistryHosts {
	return func(host string) ([]docker.RegistryHost, error) {
		var allHosts []docker.RegistryHost
		for _, registryHost := range registryHosts {
			hosts, err := registryHost(host)
			if err != nil {
				return nil, err
			}
			allHosts = append(allHosts, hosts...)
		}
		return allHosts, nil
	}
}

func insecureHosts(hosts docker.RegistryHosts, f func(host string) (bool, error)) docker.RegistryHosts {
	return func(host string) ([]docker.RegistryHost, error) {
		hosts, err := hosts(host)
		if err != nil {
			return nil, err
		}
		for i, host := range hosts {
			ok, err := f(host.Host)
			if err != nil {
				return nil, err
			}
			if ok {
				host.Client = httpclient.InsecureHTTPClient
				hosts[i] = host
			}
		}
		return hosts, nil
	}
}

// hostCreeds returns the username and password for the given hostname index,
// if they're somehow available in the profile.
func hostCreds(profile *config.Profile, hostname string) (string, string, error) {
	if profile == nil {
		return "", "", nil
	}

	// FIXME: why are there two different auth schemes?
	if slices.Contains(defaultRegistries, hostname) {
		return decodeAuth(profile.Token)
	}

	if len(profile.Metros) == 0 {
		username := profile.Organization
		if username == "" {
			// organization may not be set on old or manually created
			// profiles - so fall back to decoding the username from the
			// token itself
			username, _, _ = decodeAuth(profile.Token)
		}
		return username, profile.Token, nil
	}

	for _, metro := range profile.Metros {
		if hostname == metro.Index().Host {
			username := profile.Organization
			if username == "" {
				// organization may not be set on old or manually created
				// profiles - so fall back to decoding the username from the
				// token itself
				username, _, _ = decodeAuth(profile.Token)
			}
			return username, profile.Token, nil
		}
	}

	return "", "", nil
}

// decodeAuth is imported from github.com/docker/cli/cli/config/configfile, and
// is the same logic used to decode the "auth" field in the Docker config file.
func decodeAuth(authStr string) (string, string, error) {
	if authStr == "" {
		return "", "", nil
	}

	decLen := base64.StdEncoding.DecodedLen(len(authStr))
	decoded := make([]byte, decLen)
	authByte := []byte(authStr)
	n, err := base64.StdEncoding.Decode(decoded, authByte)
	if err != nil {
		return "", "", err
	}
	if n > decLen {
		return "", "", errors.New("something went wrong decoding auth config")
	}
	userName, password, ok := strings.Cut(string(decoded), ":")
	if !ok || userName == "" {
		return "", "", errors.New("invalid auth configuration file")
	}
	return userName, strings.Trim(password, "\x00"), nil
}
