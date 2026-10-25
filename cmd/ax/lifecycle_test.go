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

package main

import (
	"testing"
	"time"
)

func TestLifecycleTimeoutCanCoverBootstrapAndDurableUpload(t *testing.T) {
	t.Setenv("AX_LIFECYCLE_TIMEOUT", "70m")
	ctx, cancel, err := lifecycleContext()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < 69*time.Minute {
		t.Fatal("configured lifecycle timeout not applied")
	}
	for _, raw := range []string{"invalid", "0s", "-1m"} {
		t.Setenv("AX_LIFECYCLE_TIMEOUT", raw)
		if _, _, err := lifecycleContext(); err == nil {
			t.Errorf("accepted invalid timeout %q", raw)
		}
	}
}
