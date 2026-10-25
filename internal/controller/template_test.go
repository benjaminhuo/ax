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

package controller

import (
	"strings"
	"testing"

	"github.com/google/ax/internal/substrate"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
)

func TestTemplateNameIncludesEffectiveOperatorConfiguration(t *testing.T) {
	base := substrate.BuildActorTemplate("team-a", "", "runner", map[string]string{"B": "2", "A": "1"}, nil, "", nil)
	name, err := taskTemplateName("task", base)
	if err != nil {
		t.Fatal(err)
	}
	reordered := substrate.BuildActorTemplate("team-a", "", "runner", map[string]string{"A": "1", "B": "2"}, nil, "", nil)
	same, err := taskTemplateName("task", reordered)
	if err != nil {
		t.Fatal(err)
	}
	if same != name {
		t.Fatal("environment map order changed template identity")
	}
	for _, change := range []func(*ateapipb.ActorTemplate){
		func(tmpl *ateapipb.ActorTemplate) { tmpl.Resources.Limits[0].Quantity = "2" },
		func(tmpl *ateapipb.ActorTemplate) {
			tmpl.WorkerSelector.MatchLabels["kagent.dev/worker-pool-name"] = "other"
		},
		func(tmpl *ateapipb.ActorTemplate) { tmpl.SnapshotConfig.StorageLocation = "s3://other/" },
		func(tmpl *ateapipb.ActorTemplate) { tmpl.Containers[0].WakeupProbe.TimeoutSeconds++ },
		func(tmpl *ateapipb.ActorTemplate) { tmpl.Containers[0].VolumeMounts[0].MountPath = "/project" },
	} {
		tmpl := proto.Clone(base).(*ateapipb.ActorTemplate)
		change(tmpl)
		changed, err := taskTemplateName("task", tmpl)
		if err != nil {
			t.Fatal(err)
		}
		if changed == name {
			t.Fatal("effective template change did not change identity")
		}
	}
}

func TestLongTaskTemplateNamesAndCleanupRemainIsolated(t *testing.T) {
	tmpl := substrate.BuildActorTemplate("team-a", "", "runner", nil, nil, "", nil)
	a, b := strings.Repeat("a", 62)+"b", strings.Repeat("a", 62)+"c"
	nameA, err := taskTemplateName(a, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	nameB, err := taskTemplateName(b, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if len(nameA) > 63 || len(nameB) > 63 || nameA == nameB {
		t.Fatalf("invalid or colliding names: %q %q", nameA, nameB)
	}
	if !taskTemplatePattern(a).MatchString(nameA) || taskTemplatePattern(a).MatchString(nameB) {
		t.Fatal("cleanup could target a different task")
	}
}
