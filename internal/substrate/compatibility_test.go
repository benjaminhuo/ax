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
	"strings"
	"testing"
	"time"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const pinnedTestImage = "docker.io/example/runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type controlStub struct {
	ateapipb.ControlClient
	getAtespace    func(*ateapipb.GetAtespaceRequest) (*ateapipb.Atespace, error)
	createAtespace func(*ateapipb.CreateAtespaceRequest) (*ateapipb.Atespace, error)
	getPolicy      func(*ateapipb.GetActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error)
	createPolicy   func(*ateapipb.CreateActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error)
	updatePolicy   func(*ateapipb.UpdateActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error)
	suspend        func(*ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error)
	getActor       func(*ateapipb.GetActorRequest) (*ateapipb.Actor, error)
	createActor    func(*ateapipb.CreateActorRequest) (*ateapipb.Actor, error)
	revert         func(*ateapipb.RevertActorRequest) (*ateapipb.RevertActorResponse, error)
	listTemplates  func(*ateapipb.ListActorTemplatesRequest) (*ateapipb.ListActorTemplatesResponse, error)
}

func (s *controlStub) GetAtespace(_ context.Context, r *ateapipb.GetAtespaceRequest, _ ...grpc.CallOption) (*ateapipb.Atespace, error) {
	return s.getAtespace(r)
}
func (s *controlStub) CreateAtespace(_ context.Context, r *ateapipb.CreateAtespaceRequest, _ ...grpc.CallOption) (*ateapipb.Atespace, error) {
	return s.createAtespace(r)
}
func (s *controlStub) GetActorEgressPolicy(_ context.Context, r *ateapipb.GetActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	return s.getPolicy(r)
}
func (s *controlStub) CreateActorEgressPolicy(_ context.Context, r *ateapipb.CreateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	return s.createPolicy(r)
}
func (s *controlStub) UpdateActorEgressPolicy(_ context.Context, r *ateapipb.UpdateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	return s.updatePolicy(r)
}
func (s *controlStub) SuspendActor(_ context.Context, r *ateapipb.SuspendActorRequest, _ ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	return s.suspend(r)
}
func (s *controlStub) GetActor(_ context.Context, r *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	return s.getActor(r)
}
func (s *controlStub) CreateActor(_ context.Context, r *ateapipb.CreateActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	return s.createActor(r)
}
func (s *controlStub) RevertActor(_ context.Context, r *ateapipb.RevertActorRequest, _ ...grpc.CallOption) (*ateapipb.RevertActorResponse, error) {
	return s.revert(r)
}
func (s *controlStub) ListActorTemplates(_ context.Context, r *ateapipb.ListActorTemplatesRequest, _ ...grpc.CallOption) (*ateapipb.ListActorTemplatesResponse, error) {
	return s.listTemplates(r)
}

func TestEnsureAtespaceUsesScopedReadBeforeCreate(t *testing.T) {
	for _, code := range []codes.Code{codes.OK, codes.NotFound, codes.PermissionDenied} {
		t.Run(code.String(), func(t *testing.T) {
			creates := 0
			stub := &controlStub{
				getAtespace: func(*ateapipb.GetAtespaceRequest) (*ateapipb.Atespace, error) {
					if code == codes.OK {
						return &ateapipb.Atespace{}, nil
					}
					return nil, status.Error(code, "read")
				},
				createAtespace: func(*ateapipb.CreateAtespaceRequest) (*ateapipb.Atespace, error) {
					creates++
					return &ateapipb.Atespace{}, nil
				},
			}
			err := (&Client{control: stub}).EnsureAtespace(context.Background(), "team-a")
			if (err != nil) != (code == codes.PermissionDenied) {
				t.Fatalf("unexpected error: %v", err)
			}
			want := 0
			if code == codes.NotFound {
				want = 1
			}
			if creates != want {
				t.Fatalf("CreateAtespace calls = %d, want %d", creates, want)
			}
		})
	}
}

func TestPrepareActorTemplatePersistsStateAndEnforcesLimits(t *testing.T) {
	c := &Client{template: TemplateOptions{DefaultCPU: "500m", DefaultMemory: "2Gi"}}
	tmpl, err := c.PrepareActorTemplate("team-a", pinnedTestImage, map[string]string{"AX_TASK_YAML": "task"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.GetResources().GetLimits()[0].GetQuantity() != "500m" || tmpl.GetResources().GetLimits()[1].GetQuantity() != "2Gi" {
		t.Fatalf("missing defaults: %v", tmpl.GetResources())
	}
	mounts := map[string]string{}
	for _, m := range tmpl.Containers[0].VolumeMounts {
		mounts[m.Name] = m.MountPath
	}
	if mounts["workspace"] != "/workspace" || mounts["ax-state"] != "/ax" || mounts["egress-trust"] != egressTrustMount {
		t.Fatalf("missing persistent state or trust mount: %v", mounts)
	}
	custom, err := c.PrepareActorTemplate("team-a", pinnedTestImage, nil, nil, "/project")
	if err != nil || custom.GetContainers()[0].GetVolumeMounts()[0].GetMountPath() != "/project" {
		t.Fatalf("custom workspace is not durable: %v", err)
	}
	trust := tmpl.Volumes[2].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
	if len(trust.GetNames()) != 2 {
		t.Fatalf("public and MITM roots must both be projected: %v", trust)
	}
	for _, env := range []map[string]string{
		{"SSL_CERT_FILE": "/untrusted"}, {"AX_TASK_YAML": strings.Repeat("x", 32769)},
	} {
		if _, err := c.PrepareActorTemplate("team-a", pinnedTestImage, env, nil); err == nil {
			t.Fatalf("accepted invalid environment %v", env)
		}
	}
	if _, err := c.PrepareActorTemplate("team-a", "runner:latest", nil, nil); err == nil {
		t.Fatal("accepted unpinned image")
	}
}

func TestEgressRulesAreCanonicalAndBounded(t *testing.T) {
	a, err := EgressRules([]string{"https://github.com", "http://collector:4318", "https://github.com:443/", "tls://*.example.com:8443"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := EgressRules([]string{"tls://*.example.com:8443", "https://github.com", "http://collector:4318"})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&ateapipb.EgressPolicy{Rules: a}, &ateapipb.EgressPolicy{Rules: b}) || len(a) != 3 {
		t.Fatal("origins must be deduplicated and sorted")
	}
	for _, bad := range []string{"https://127.0.0.1", "https://user:pass@example.com", "https://example.com/path", "https://example.com?query", "ftp://example.com", "https://foo..example.com", "https://example.com:70000"} {
		if _, err := EgressRules([]string{bad}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := EgressRules([]string{"https://example.com", "tls://example.com"}); err == nil {
		t.Fatal("accepted ambiguous protocols")
	}
	if rules, err := EgressRules(nil); err != nil || len(rules) != 0 {
		t.Fatal("empty allowlist must deny all")
	}
}

func TestEnsureEgressPolicyUpdatesWithPreconditionsAndCanRevoke(t *testing.T) {
	rules, err := EgressRules([]string{"https://github.com"})
	if err != nil {
		t.Fatal(err)
	}
	var stored *ateapipb.EgressPolicy
	creates, updates := 0, 0
	stub := &controlStub{
		getPolicy: func(r *ateapipb.GetActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error) {
			if r.GetName() != "default" || r.GetActor().GetAtespace() != "team-a" {
				t.Fatal("wrong policy reference")
			}
			if stored == nil {
				return nil, status.Error(codes.NotFound, "missing")
			}
			return proto.Clone(stored).(*ateapipb.EgressPolicy), nil
		},
		createPolicy: func(r *ateapipb.CreateActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error) {
			creates++
			stored = proto.Clone(r.GetEgressPolicy()).(*ateapipb.EgressPolicy)
			stored.Metadata.Uid = "00000000-0000-0000-0000-000000000001"
			stored.Metadata.Version = 3
			return stored, nil
		},
		updatePolicy: func(r *ateapipb.UpdateActorEgressPolicyRequest) (*ateapipb.EgressPolicy, error) {
			updates++
			if r.GetEgressPolicy().GetMetadata().GetUid() != stored.Metadata.Uid || r.GetEgressPolicy().GetMetadata().GetVersion() != 3 {
				t.Fatal("missing update preconditions")
			}
			stored = proto.Clone(r.GetEgressPolicy()).(*ateapipb.EgressPolicy)
			return stored, nil
		},
	}
	c := &Client{control: stub, egressRules: rules}
	if err := c.EnsureActorEgressPolicy(context.Background(), "team-a", "task"); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureActorEgressPolicy(context.Background(), "team-a", "task"); err != nil {
		t.Fatal(err)
	}
	c.egressRules = nil
	if err := c.EnsureActorEgressPolicy(context.Background(), "team-a", "task"); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || updates != 1 || len(stored.GetRules()) != 0 {
		t.Fatalf("policy revocation was not applied: creates=%d updates=%d", creates, updates)
	}
}

func snapshotActor(generation int32, storageStatus ateapipb.SnapshotStorageStatus) *ateapipb.Actor {
	return &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Uid: "actor-uid"},
		Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, Snapshots: []*ateapipb.Snapshot{{
			Generation:      generation,
			DurableSnapshot: &ateapipb.SnapshotStorage{Status: storageStatus, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://ate-snapshots/task/snapshot"}},
		}}},
	}
}

func TestSuspendWaitsForLatestDurableSnapshot(t *testing.T) {
	old := defaultRetryInterval
	defaultRetryInterval = time.Millisecond
	defer func() { defaultRetryInterval = old }()
	pending := snapshotActor(2, ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING)
	pending.Status.Snapshots = append(pending.Status.Snapshots, snapshotActor(1, ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED).Status.Snapshots[0])
	reads := 0
	stub := &controlStub{
		suspend: func(r *ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error) {
			if r.GetOptions().GetMode() != ateapipb.DurabilityMode_DURABILITY_MODE_SYNCHRONOUS {
				t.Fatal("suspend must request synchronous durability")
			}
			return &ateapipb.SuspendActorResponse{Actor: pending}, nil
		},
		getActor: func(*ateapipb.GetActorRequest) (*ateapipb.Actor, error) {
			reads++
			return snapshotActor(2, ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED), nil
		},
	}
	if err := (&Client{control: stub}).SuspendActor(context.Background(), "team-a", "task"); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("accepted an old completed generation, reads=%d", reads)
	}
	stub.getActor = func(*ateapipb.GetActorRequest) (*ateapipb.Actor, error) {
		actor := snapshotActor(2, ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED)
		actor.Metadata.Uid = "replacement"
		return actor, nil
	}
	if err := (&Client{control: stub}).SuspendActor(context.Background(), "team-a", "task"); err == nil {
		t.Fatal("accepted replacement Actor while waiting")
	}
}

func TestSuspendInitialActorAndCancellation(t *testing.T) {
	actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED}}
	stub := &controlStub{suspend: func(*ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error) {
		return &ateapipb.SuspendActorResponse{Actor: actor}, nil
	}}
	c := &Client{control: stub}
	if err := c.SuspendActor(context.Background(), "team-a", "task"); err != nil {
		t.Fatal(err)
	}
	actor = snapshotActor(1, ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SuspendActor(ctx, "team-a", "task"); err == nil {
		t.Fatal("ignored canceled upload wait")
	}
}

func TestCrashedActorIsPreservedWhenRevertFails(t *testing.T) {
	stub := &controlStub{
		createActor: func(*ateapipb.CreateActorRequest) (*ateapipb.Actor, error) {
			return nil, status.Error(codes.AlreadyExists, "exists")
		},
		getActor: func(*ateapipb.GetActorRequest) (*ateapipb.Actor, error) {
			return &ateapipb.Actor{Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_CRASHED}}, nil
		},
		revert: func(*ateapipb.RevertActorRequest) (*ateapipb.RevertActorResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "denied")
		},
	}
	// No DeleteActor implementation: a destructive fallback would panic here.
	_, err := (&Client{control: stub}).EnsureActor(context.Background(), "team-a", "task", "team-a", "template")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unexpected revert result: %v", err)
	}
}

func TestListTemplatesDrainsPages(t *testing.T) {
	reads := 0
	stub := &controlStub{listTemplates: func(r *ateapipb.ListActorTemplatesRequest) (*ateapipb.ListActorTemplatesResponse, error) {
		reads++
		if r.GetOptions().GetPageToken() == "" {
			return &ateapipb.ListActorTemplatesResponse{ActorTemplates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Name: "first"}}}, NextPageToken: "next"}, nil
		}
		return &ateapipb.ListActorTemplatesResponse{ActorTemplates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Name: "second"}}}}, nil
	}}
	all, err := (&Client{control: stub}).ListActorTemplates(context.Background(), "team-a")
	if err != nil || len(all) != 2 || reads != 2 {
		t.Fatalf("templates=%v reads=%d err=%v", all, reads, err)
	}
}

func TestExistingTemplateChecksImmutableContent(t *testing.T) {
	want := BuildActorTemplate("team-a", "template", pinnedTestImage, nil, nil, "", nil)
	stored := proto.Clone(want).(*ateapipb.ActorTemplate)
	stored.Metadata.Uid = "server-owned"
	stored.Metadata.Version = 5
	stored.Status = &ateapipb.ActorTemplateStatus{}
	mock := &mockControlClient{templates: map[string]*ateapipb.ActorTemplate{"team-a/template": stored}}
	c := &Client{control: mock}
	if _, err := c.EnsureActorTemplate(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	stored.Resources.Limits[0].Quantity = "4"
	if _, err := c.EnsureActorTemplate(context.Background(), want); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reused different template: %v", err)
	}
	if len(mock.createdTemplates) != 0 {
		t.Fatal("must not recreate an existing immutable template")
	}
}
