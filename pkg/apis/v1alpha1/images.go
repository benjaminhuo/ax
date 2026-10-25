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

import (
	_ "crypto/sha256" // Register SHA-256 for OCI digest validation.
	"fmt"

	"github.com/distribution/reference"
)

// ValidateTaskImage requires an immutable OCI image reference, as required by
// Substrate v0.6.0-alpha3. For multi-platform images, use the image index digest.
func ValidateTaskImage(image string) error {
	if len(image) > 512 {
		return fmt.Errorf("image reference exceeds 512 characters")
	}
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", image, err)
	}
	if _, ok := ref.(reference.Digested); !ok {
		return fmt.Errorf("image %q must be pinned by digest (repository@sha256:...)", image)
	}
	return nil
}
