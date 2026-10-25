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

package controller_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/ax/internal/controller"
	"github.com/google/ax/internal/substrate"
	"github.com/google/ax/pkg/apis/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func configuredReconciler(t *testing.T, mock *mockControlServer) *controller.TaskReconciler {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	ateapipb.RegisterControlServer(srv, mock)
	go srv.Serve(lis)
	t.Cleanup(func() { srv.Stop(); lis.Close() })
	c, err := substrate.NewClientWithOptions(substrate.ClientOptions{
		Target: lis.Addr().String(), Plaintext: true,
		Template:      substrate.TemplateOptions{DefaultCPU: "500m", DefaultMemory: "2Gi"},
		EgressOrigins: []string{"https://github.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	r := controller.NewTaskReconciler(c, "", "", testDefaultImage)
	r.SecretResolver = noSecrets
	r.WorkspaceReadyTimeout = time.Millisecond
	return r
}

func TestConfiguredRuntimeIsAppliedBeforeResume(t *testing.T) {
	mock := &mockControlServer{}
	r := configuredReconciler(t, mock)
	task := &v1alpha1.Task{Metadata: &v1alpha1.ObjectMeta{Name: "configured", Atespace: "team-a"}, Spec: &v1alpha1.TaskSpec{Resources: &v1alpha1.ResourcesSpec{Limits: &v1alpha1.ResourceList{Cpu: "2"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := r.Reconcile(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	limits := mock.createdTemplates[0].GetResources().GetLimits()
	if limits[0].GetQuantity() != "2" || limits[1].GetQuantity() != "2Gi" {
		t.Fatalf("effective resources lost: %v", limits)
	}
	if len(mock.resumedActors) != 0 {
		t.Fatal("initial suspended task must not run")
	}
	task.Status.Phase = "Running"
	if _, err := r.Reconcile(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	if len(mock.createdTemplates) != 1 || len(mock.createdAtespaces) != 1 {
		t.Fatal("reconcile must reuse identical templates and scoped atespaces")
	}
	if len(mock.resumedActors) != 1 {
		t.Fatal("task was not resumed after policy installation")
	}
	if len(mock.egressPolicies["configured"].GetRules()) != 1 {
		t.Fatal("configured policy was not installed before execution")
	}
}

func TestPolicyFailureDoesNotPreventSuspend(t *testing.T) {
	mock := &mockControlServer{egressErr: status.Error(codes.PermissionDenied, "policy denied")}
	r := configuredReconciler(t, mock)
	task := &v1alpha1.Task{Metadata: &v1alpha1.ObjectMeta{Name: "stop", Atespace: "team-a"}, Status: &v1alpha1.TaskStatus{Phase: "Suspended"}}
	if _, err := r.Reconcile(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	if len(mock.suspendedActors) != 1 || len(mock.resumedActors) != 0 {
		t.Fatal("policy permissions must not block stopping execution")
	}
}

func TestEgressPolicyFailurePreventsResume(t *testing.T) {
	mock := &mockControlServer{egressErr: status.Error(codes.PermissionDenied, "policy denied")}
	r := configuredReconciler(t, mock)
	task := &v1alpha1.Task{Metadata: &v1alpha1.ObjectMeta{Name: "denied", Atespace: "team-a"}, Status: &v1alpha1.TaskStatus{Phase: "Running"}}
	if _, err := r.Reconcile(context.Background(), task, nil); err == nil {
		t.Fatal("policy failure was ignored")
	}
	if task.GetStatus().GetPhase() != "Failed" || len(mock.resumedActors) != 0 {
		t.Fatal("task must not resume without its configured policy")
	}
}
