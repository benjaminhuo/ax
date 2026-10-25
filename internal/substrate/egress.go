// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package substrate

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// EgressRules compiles operator-provided origins. HTTPS is intercepted by
// Substrate; tls:// selects TLS passthrough. Empty input explicitly denies all.
func EgressRules(origins []string) ([]*ateapipb.EgressRule, error) {
	rules := map[string]*ateapipb.EgressRule{}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "tls") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("egress origin %q must be http://host[:port], https://host[:port], or tls://host[:port]", origin)
		}
		host := strings.ToLower(u.Hostname())
		if net.ParseIP(host) != nil || len(host) > 253 {
			return nil, fmt.Errorf("egress origin %q requires a DNS hostname", origin)
		}
		labels := strings.Split(strings.TrimPrefix(host, "*."), ".")
		if host != "*" {
			for _, label := range labels {
				if len(label) > 63 || !dnsLabel.MatchString(label) {
					return nil, fmt.Errorf("egress origin %q has an invalid hostname", origin)
				}
			}
		}
		port := int64(443)
		if u.Scheme == "http" {
			port = 80
		}
		if p := u.Port(); p != "" {
			port, err = strconv.ParseInt(p, 10, 32)
			if err != nil || port < 1 || port > 65535 {
				return nil, fmt.Errorf("egress origin %q has an invalid port", origin)
			}
		}
		ports := &ateapipb.Ports{Numbers: []int32{int32(port)}}
		rule := &ateapipb.EgressRule{}
		switch u.Scheme {
		case "http":
			rule.Http = &ateapipb.HttpRule{Hostnames: []string{host}, Ports: ports}
		case "https":
			rule.Https = &ateapipb.HttpsRule{Hostnames: []string{host}, Ports: ports}
		case "tls":
			rule.TlsPassthrough = &ateapipb.TlsPassthroughRule{Hostnames: []string{host}, Ports: ports}
		}
		key := fmt.Sprintf("%s:%d", host, port)
		if previous, exists := rules[key]; exists && !proto.Equal(previous, rule) {
			return nil, fmt.Errorf("egress origin %q conflicts with another protocol on %s", origin, key)
		}
		rules[key] = rule
	}
	if len(rules) > 256 {
		return nil, fmt.Errorf("Substrate permits at most 256 egress rules")
	}
	keys := make([]string, 0, len(rules))
	for key := range rules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*ateapipb.EgressRule, 0, len(keys))
	for _, key := range keys {
		result = append(result, rules[key])
	}
	return result, nil
}

// EnsureActorEgressPolicy applies the allowlist before an Actor is resumed.
// Updating an existing policy uses its UID and version to protect concurrent work.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, actorName string) error {
	actor := &ateapipb.ObjectRef{Atespace: atespace, Name: actorName}
	return retryOnCodes(ctx, func() error {
		want := &ateapipb.EgressPolicy{
			Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"},
			Rules:    c.egressRules,
		}
		current, err := c.control.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor, Name: "default"})
		if status.Code(err) == codes.NotFound {
			_, err = c.control.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{Actor: actor, EgressPolicy: want})
			return err
		}
		if err != nil {
			return err
		}
		if proto.Equal(&ateapipb.EgressPolicy{Rules: current.GetRules()}, &ateapipb.EgressPolicy{Rules: want.GetRules()}) {
			return nil
		}
		want.Metadata.Uid = current.GetMetadata().GetUid()
		want.Metadata.Version = current.GetMetadata().GetVersion()
		_, err = c.control.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: want})
		return err
	}, codes.Unavailable, codes.Aborted, codes.AlreadyExists)
}
