// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/jason"
)

type APICmd struct {
	Endpoint string   `arg:"" help:"API endpoint path (e.g. /v1/instances). May be a full URL, in which case the metro endpoint is ignored."`
	Method   string   `short:"X" name:"method" help:"HTTP method to use. Defaults to GET, or POST if data is provided." placeholder:"method"`
	Metro    string   `help:"Metro to target. Defaults to the profile's default metro." placeholder:"name"`
	Header   []string `short:"H" name:"header" help:"Add an HTTP header in 'Key: Value' format. May be repeated."`
	Data     string   `short:"d" name:"data" help:"(deprecated) Use positional arguments instead." hidden:""`
	Insecure bool     `short:"k" name:"insecure" help:"Skip TLS certificate verification."`
	Args     []string `arg:"" optional:"" name:"data" help:"Request body data. Can be JSON (e.g. '{\"key\":\"val\"}'), nested JSON (e.g. key=val or \"key=line value\"), @file, or @- (stdin)."`
}

func (c *APICmd) Run(ctx context.Context, stdio config.Stdio) error {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return err
	}

	var (
		reqURL   string
		insecure bool
		trusted  bool
	)
	if strings.HasPrefix(c.Endpoint, "http://") || strings.HasPrefix(c.Endpoint, "https://") {
		reqURL = c.Endpoint
		// Only attach credentials if the host matches a configured metro for
		// the current profile, to avoid leaking the bearer token to
		// arbitrary hosts.
		u, err := url.Parse(reqURL)
		if err != nil {
			return jujuerrors.Annotate(err, "parsing endpoint URL")
		}
		for i := range profile.Metros {
			mu, err := url.Parse(profile.Metros[i].Endpoint)
			if err != nil {
				continue
			}
			if mu.Host == u.Host {
				trusted = true
				insecure = ptr.ZeroIfNil(profile.Metros[i].Insecure)
				break
			}
		}
	} else {
		metroName := c.Metro
		if metroName == "" {
			metroName = profile.GetDefaultMetro()
		}
		if metroName == "" {
			return jujuerrors.New("no metro specified and no default metro set for the current profile")
		}

		var metro *config.Metro
		for i := range profile.Metros {
			if profile.Metros[i].Name == metroName {
				metro = &profile.Metros[i]
				break
			}
		}
		if metro == nil {
			return jujuerrors.Errorf("metro %q is not configured in profile %q", metroName, profile.Name)
		}

		path := c.Endpoint
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		reqURL = strings.TrimRight(metro.Endpoint, "/") + path
		insecure = ptr.ZeroIfNil(metro.Insecure)
		trusted = true
	}
	if c.Insecure {
		insecure = true
	}

	// Resolve the request body.
	bodyBytes, err := c.resolveBody(ctx, stdio)
	if err != nil {
		return err
	}

	method := strings.ToUpper(c.Method)
	if method == "" {
		if c.Data != "" || len(c.Args) > 0 {
			method = http.MethodPost
		} else {
			method = http.MethodGet
		}
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return err
	}
	if trusted {
		req.Header.Set("Authorization", "Bearer "+profile.Token)
	}
	req.Header.Set("Accept", "application/json")
	if bodyBytes != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range c.Header {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			return jujuerrors.Errorf("invalid header %q: expected 'Key: Value'", h)
		}
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}

	log.G(ctx).
		Debug().
		Str("method", method).
		Str("url", reqURL).
		Msg("sending API request")

	resp, err := httpclient.GetClient(insecure).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(raw) > 0 {
		if json.Valid(raw) {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, raw, "", "  "); err == nil {
				pretty.WriteByte('\n')
				if _, err := stdio.Stdout.Write(pretty.Bytes()); err != nil {
					return err
				}
			} else {
				if _, err := stdio.Stdout.Write(raw); err != nil {
					return err
				}
			}
		} else {
			if _, err := stdio.Stdout.Write(raw); err != nil {
				return err
			}
			if !bytes.HasSuffix(raw, []byte("\n")) {
				fmt.Fprintln(stdio.Stdout)
			}
		}
	}

	if resp.StatusCode >= 400 {
		return jujuerrors.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

func (c *APICmd) resolveBody(ctx context.Context, stdio config.Stdio) ([]byte, error) {
	var body bytes.Buffer
	var wroteSource bool
	legacyOnly := c.Data != "" && len(c.Args) == 0

	if c.Data != "" {
		log.G(ctx).Warn().Msg("-d/--data is deprecated; use positional arguments instead")
		raw, err := readBodySource(c.Data, stdio)
		if err != nil {
			return nil, err
		}
		body.Write(raw)
		wroteSource = true
	}

	for _, arg := range c.Args {
		raw, err := readBodySource(arg, stdio)
		if err != nil {
			return nil, err
		}
		if wroteSource {
			body.WriteByte('\n')
		}
		body.Write(raw)
		wroteSource = true
	}

	if !wroteSource {
		return nil, nil
	}

	var bodyData jason.Jason[any]
	if err := jason.Unmarshal(body.Bytes(), &bodyData); err != nil {
		if legacyOnly {
			return body.Bytes(), nil
		}
		return nil, err
	}
	return jason.Marshal(bodyData)
}

// readBodySource reads a body source from stdin, file, or inline text
func readBodySource(source string, stdio config.Stdio) ([]byte, error) {
	var raw []byte
	var err error
	switch {
	case source == "@-":
		raw, err = io.ReadAll(stdio.Stdin)
		if err != nil {
			return nil, jujuerrors.Annotate(err, "reading request body from stdin")
		}
	case strings.HasPrefix(source, "@"):
		raw, err = os.ReadFile(source[1:])
		if err != nil {
			return nil, jujuerrors.Annotatef(err, "reading %s", source)
		}
	default:
		raw = []byte(source)
	}
	return raw, nil
}

func (APICmd) Help() string {
	return heredoc.Docf(`
		The API command allows you to make direct HTTP requests to the
		Unikraft Cloud API without using a higher-level CLI subcommand. This
		is useful for endpoints that do not yet have a dedicated command, or
		for scripting advanced workflows.

		## Request body syntax

		The request body can be specified as positional arguments. Each
		argument is one of:

		%[2]s
		@file        Read the body from a file on disk.
		@-           Read the body from standard input (stdin).
		{...}        Inline JSON object (merges with other arguments).
		[...]        Inline JSON array (replaces the entire body).
		key=value    Set a key to a literal string value.
		key:=raw     Set a key to a raw JSON value (number, boolean, null,
		             object, or array constructed from raw JSON).
		%[2]s

		## Nested JSON syntax

		The %[1]skey=value%[1]s and %[1]skey:=raw%[1]s forms can be used to
		build complex nested structures. The keys of an object are sent in
		alphabetical order:

		%[2]s
		key[sub]=value
		    Sets a nested key inside an object. Multiple bracket segments
		    create deeply nested objects.
		    Example: user[name]=Alice user[age]:=30
		    Produces: {"user":{"age":30,"name":"Alice"}}

		key[]=value
		    Appends a value to an array. Multiple [] arguments add more
		    elements.
		    Example: apps[]=Terminal apps[]=Desktop
		    Produces: {"apps":["Terminal","Desktop"]}

		key[][sub]=value
		    Creates a new object, assigns the sub-key inside it, and appends
		    the whole object to the array. Further [sub] segments nest deeper.
		    Example: arr[][key]=value arr[][count]:=42
		    Produces: {"arr":[{"key":"value"},{"count":42}]}

		key[N]=value
		    Assigns at a specific numeric index in an array. Gaps are padded
		    with null. The maximum index is 10000.
		    Example: arr[0]=first arr[2]=third
		    Produces: {"arr":["first",null,"third"]}
		%[2]s

		## Top-level array syntax

		When the first argument starts with a bracket (e.g. %[1]s[0][key]=value%[1]s
		or %[1]s[]=value%[1]s), the root of the request body becomes a JSON
		array instead of an object.

		%[2]s
		[N][key]=value
		    Creates an array of objects and assigns at index N.
		    Example: [0][name]=web [0][port]:=80 [1][name]=api [1][port]:=90
		    Produces: [{"name":"web","port":80},{"name":"api","port":90}]

		[]=value
		    Appends raw values to the root array.
		    Example: []=a []=b []=c
		    Produces: ["a","b","c"]

		[]:=raw
		    Appends a raw JSON value (number, boolean, null) to the root array.
		    Example: []:=1 []:=2 []:=3
		    Produces: [1,2,3]
		%[2]s

		## Raw JSON values

		Use the %[1]s:=%[1]s operator (instead of %[1]s=%[1]s) to pass a raw
		JSON value:

		%[2]s
		count:=42           Number, not a string.
		active:=true        Boolean.
		data:=null          Null value.
		nested:={"a":1}     Nested JSON object.
		arr:=["x","y"]      Nested JSON array.
		%[2]s

		## Escaping

		Special characters in key names can be escaped with a backslash:

		%[2]s
		key\[sub\]=value    Literal bracket in key name.
		key\=value=test     Literal equals sign in key name.
		key[\\]=value       Literal backslash in key name.
		key[\1]=value       Force a numeric segment to be treated as a
		                    string map key instead of an array index.
		%[2]s

		## Multiple arguments

		Multiple body arguments can be passed on a single line or across
		multiple lines. Arguments are processed in order; later values
		overwrite earlier ones at the same path.

		## Whitespace in values

		If a value contains spaces, quote the entire argument:

		%[2]s
		unikraft api /v1/volumes "name=my test volume" stars:=54000
		%[2]s
	`, "`", "```")
}

func (APICmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "List instances in the default metro",
			Commands: []string{
				"unikraft api /v1/instances",
			},
		},
		{
			Description: "Get the current user's quotas",
			Commands: []string{
				"unikraft api /v1/users/quotas",
			},
		},
		{
			Description: "Inspect a specific instance by UUID",
			Commands: []string{
				"unikraft api /v1/instances/abc123-...-def456",
			},
		},
		{
			Description: "Create a new 256MB volume with raw JSON values",
			Commands: []string{
				`unikraft api /v1/volumes --metro=fra name=data size_mb:=256`,
			},
		},
		{
			Description: "Set nested object keys using bracket notation",
			Commands: []string{
				`unikraft api /v1/instances user[name]=Alice user[age]:=30 user[role]=admin`,
			},
		},
		{
			Description: "Append values to an array with key[]= syntax",
			Commands: []string{
				`unikraft api /v1/volumes tags[]=prod tags[]=critical`,
			},
		},
		{
			Description: "Append objects to an array using key[][sub]= syntax",
			Commands: []string{
				`unikraft api /v1/instances volumes[][id]=vol-abc volumes[][mount]=/data`,
			},
		},
		{
			Description: "Create a top-level JSON array with [N][key]=value syntax",
			Commands: []string{
				`unikraft api /v1/batch [0][type]=platform [0][name]=desktop [1][type]=platform [1][name]=web`,
			},
		},
		{
			Description: "Build a root array of raw values with []:= syntax",
			Commands: []string{
				`unikraft api /v1/items []:=42 []:=true []:=null`,
			},
		},
		{
			Description: "Assign at a specific array index with padding",
			Commands: []string{
				`unikraft api /v1/config errors[0]=first errors[2]=third`,
			},
		},
		{
			Description: "Merge an inline JSON object with key=value arguments",
			Commands: []string{
				`unikraft api /v1/volumes {"existing":"value"} name=new-volume`,
			},
		},
		{
			Description: "Pass a full JSON literal as the request body",
			Commands: []string{
				`unikraft api /v1/volumes '{"name":"test","size_mb":256}'`,
			},
		},
		{
			Description: "Create resources from a JSON file",
			Commands: []string{
				"unikraft api /v1/volumes @volume.json",
			},
		},
		{
			Description: "Pipe a request body in from stdin",
			Commands: []string{
				"unikraft api /v1/volumes @-",
			},
		},
		{
			Description: "Pass a value containing spaces (quote the argument)",
			Commands: []string{
				`unikraft api /v1/volumes "name=test volume" stars:=54000`,
			},
		},
		{
			Description: "Force a numeric segment as a string map key with backslash escaping",
			Commands: []string{
				`unikraft api /v1/config object[\1]=stringified object[\100]=same`,
			},
		},
		{
			Description: "Escape special characters in key names with backslash",
			Commands: []string{
				`unikraft api /v1/config "foo\[bar\]:=1" "baz[\[]:=2"`,
			},
		},
		{
			Description: "Send custom headers and skip TLS verification",
			Commands: []string{
				"unikraft api /v1/instances -H 'X-Debug: true' -k",
			},
		},
		{
			Description: "Delete an instance by UUID",
			Commands: []string{
				"unikraft api /v1/instances/abc123-...-def456 -X DELETE",
			},
		},
		{
			Description: "Check the health of the API",
			Commands: []string{
				"unikraft api /v1/healthz",
			},
		},
	}
}
