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

package v1alpha1

import "testing"

func TestValidateTaskImage(t *testing.T) {
	for image, valid := range map[string]bool{
		"docker.io/benjaminhuo/ax-task-runner:latest@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": true,
		"docker.io/benjaminhuo/ax-task-runner:latest": false,
		"runner@sha256:abc":                           false,
		"https://example.com/runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": false,
	} {
		if err := ValidateTaskImage(image); (err == nil) != valid {
			t.Errorf("image %q: %v", image, err)
		}
	}
}

func TestWorkspaceMountsDoNotOverlapPrivateState(t *testing.T) {
	for _, p := range []string{"/", "/ax", "/ax/cache", "/run", "/run/ax/egress", "/workspace/../ax"} {
		if err := ValidateWorkspacePath(p); err == nil {
			t.Errorf("accepted overlapping mount %q", p)
		}
	}
	for _, p := range []string{"", "/workspace", "/workspace/project", "/project"} {
		if err := ValidateWorkspacePath(p); err != nil {
			t.Errorf("rejected %q: %v", p, err)
		}
	}
}
