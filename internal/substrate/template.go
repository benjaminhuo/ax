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
	"fmt"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/ax/pkg/apis/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultCPU                  = "1"
	DefaultMemory               = "1Gi"
	DefaultWakeupTimeoutSeconds = 900
	egressTrustMount            = "/run/ax/egress"
)

var trustEnvironment = []string{
	"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "AWS_CA_BUNDLE",
	"NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO",
}

// TemplateOptions are operator-owned settings shared by AX task templates.
// They deliberately stay out of the public Task API.
type TemplateOptions struct {
	DefaultCPU           string
	DefaultMemory        string
	WorkerSelector       map[string]string
	SnapshotsBucket      string
	WakeupTimeoutSeconds int32
	// WorkspacePath is derived from the Task, not an operator setting.
	WorkspacePath string
}

func (o TemplateOptions) withDefaults() TemplateOptions {
	if o.DefaultCPU == "" {
		o.DefaultCPU = DefaultCPU
	}
	if o.DefaultMemory == "" {
		o.DefaultMemory = DefaultMemory
	}
	if o.WorkerSelector == nil {
		o.WorkerSelector = map[string]string{
			"kagent.dev/worker-pool-name":      "kagent-default",
			"kagent.dev/worker-pool-namespace": "kagent",
		}
	}
	if o.SnapshotsBucket == "" {
		o.SnapshotsBucket = os.Getenv("AX_SNAPSHOTS_BUCKET")
	}
	if o.SnapshotsBucket == "" {
		o.SnapshotsBucket = DefaultSnapshotsBucket
	}
	if o.WakeupTimeoutSeconds == 0 {
		o.WakeupTimeoutSeconds = DefaultWakeupTimeoutSeconds
	}
	if o.WorkspacePath == "" {
		o.WorkspacePath = "/workspace"
	}
	o.WorkspacePath = path.Clean(o.WorkspacePath)
	return o
}

func (o TemplateOptions) Validate() error {
	o = o.withDefaults()
	if err := v1alpha1.ValidateWorkspacePath(o.WorkspacePath); err != nil {
		return err
	}
	if err := v1alpha1.ValidateResources(&v1alpha1.ResourcesSpec{Limits: &v1alpha1.ResourceList{Cpu: o.DefaultCPU, Memory: o.DefaultMemory}}); err != nil {
		return fmt.Errorf("default task resources: %w", err)
	}
	if o.WakeupTimeoutSeconds < 1 || o.WakeupTimeoutSeconds > 3600 {
		return fmt.Errorf("wakeup timeout must be between 1 and 3600 seconds")
	}
	u, err := url.Parse(o.SnapshotsBucket)
	if err != nil || u.Scheme != "s3" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || len(o.SnapshotsBucket) > 1024 {
		return fmt.Errorf("snapshot location must be an s3://bucket/prefix/ URI")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if strings.Contains(segment, "\\") || (segment != "" && strings.Trim(segment, ". ") == "") {
			return fmt.Errorf("snapshot location contains an invalid path segment")
		}
	}
	if len(o.WorkerSelector) == 0 {
		return fmt.Errorf("worker selector must select the intended kagent WorkerPool")
	}
	for _, key := range []string{"kagent.dev/worker-pool-name", "kagent.dev/worker-pool-namespace"} {
		if err := v1alpha1.ValidateName(o.WorkerSelector[key]); err != nil {
			return fmt.Errorf("worker selector %s: %w", key, err)
		}
	}
	return nil
}

// PrepareActorTemplate builds the complete effective template before its name
// is hashed, so changes to operator defaults cannot silently reuse old templates.
func (c *Client) PrepareActorTemplate(atespace, image string, env map[string]string, resources *v1alpha1.ResourcesSpec, workspacePath ...string) (*ateapipb.ActorTemplate, error) {
	if err := v1alpha1.ValidateTaskImage(image); err != nil {
		return nil, err
	}
	if err := v1alpha1.ValidateResources(resources); err != nil {
		return nil, err
	}
	for _, key := range append(append([]string{}, trustEnvironment...), "SSL_CERT_DIR") {
		if _, exists := env[key]; exists {
			return nil, fmt.Errorf("environment variable %s is reserved for Substrate egress trust", key)
		}
	}
	opts := c.template.withDefaults()
	if len(workspacePath) > 0 && workspacePath[0] != "" {
		opts.WorkspacePath = workspacePath[0]
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	tmpl := BuildActorTemplate(atespace, "", image, env, nil, "", ResourceLimits(resources, opts), opts)
	if err := validateTemplate(tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func validateTemplate(tmpl *ateapipb.ActorTemplate) error {
	if len(tmpl.GetContainers()) != 1 {
		return fmt.Errorf("AX templates require one container")
	}
	ctr := tmpl.GetContainers()[0]
	if err := v1alpha1.ValidateTaskImage(ctr.GetImage()); err != nil {
		return err
	}
	if len(ctr.GetEnv()) > 32 {
		return fmt.Errorf("template has %d environment variables; Substrate permits at most 32", len(ctr.GetEnv()))
	}
	for _, variable := range ctr.GetEnv() {
		if !utf8.ValidString(variable.GetValue()) || utf8.RuneCountInString(variable.GetValue()) > 32768 {
			return fmt.Errorf("environment variable %s must be valid UTF-8 and at most 32768 characters", variable.GetName())
		}
		name := variable.GetName()
		if name == "" || len(name) > 256 {
			return fmt.Errorf("invalid environment variable name %q", name)
		}
		for _, ch := range name {
			if ch < 32 || ch > 126 || ch == '=' {
				return fmt.Errorf("invalid environment variable name %q", name)
			}
		}
	}
	return nil
}

// ActorTemplateSpec strips server-owned fields for immutable-content checks.
func ActorTemplateSpec(tmpl *ateapipb.ActorTemplate) *ateapipb.ActorTemplate {
	out := proto.Clone(tmpl).(*ateapipb.ActorTemplate)
	out.Metadata = &ateapipb.ResourceMetadata{Atespace: tmpl.GetMetadata().GetAtespace(), Name: tmpl.GetMetadata().GetName()}
	out.Status = nil
	return out
}

func sortedEnv(env map[string]string) []*ateapipb.EnvVar {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ateapipb.EnvVar, 0, len(keys))
	for _, key := range keys {
		out = append(out, &ateapipb.EnvVar{Name: key, Value: env[key]})
	}
	return out
}
