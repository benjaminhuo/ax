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
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/ax/internal/controller"
	"github.com/google/ax/internal/lock"
	"github.com/google/ax/internal/server"
	"github.com/google/ax/internal/store/redis"
	"github.com/google/ax/internal/substrate"
	"github.com/google/ax/pkg/apis/v1alpha1"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	var (
		listenAddr              string
		redisAddr               string
		redisPassword           string
		substrateEndpoint       string
		substrateAuthority      string
		substrateTokenFile      string
		substrateCAFile         string
		substrateInsecureTLS    bool
		substratePlaintext      bool
		defaultTemplate         string
		defaultTemplateAtespace string
		defaultTaskImage        string
		defaultTaskCPU          string
		defaultTaskMemory       string
		workerPoolName          string
		workerPoolNamespace     string
		wakeupTimeoutSeconds    int
		egressOrigins           string
	)

	flag.StringVar(&listenAddr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis server address")
	flag.StringVar(&redisPassword, "redis-password", "", "Redis password")
	flag.StringVar(&substrateEndpoint, "substrate-endpoint", "api.ate-system.svc.cluster.local:443", "Agent Substrate Control API endpoint")
	flag.StringVar(&substrateAuthority, "substrate-authority", "api.ate-system.svc", "Authority / TLS ServerName for Substrate endpoint")
	flag.StringVar(&substrateTokenFile, "substrate-token-file", "", "Path to bearer token file for Substrate auth")
	flag.StringVar(&substrateCAFile, "substrate-ca-file", "", "Path to CA PEM file for Substrate TLS")
	flag.BoolVar(&substrateInsecureTLS, "substrate-insecure-tls", false, "Skip Substrate TLS verification")
	flag.BoolVar(&substratePlaintext, "substrate-plaintext", false, "Use insecure plaintext gRPC connection to Substrate")
	flag.StringVar(&defaultTemplate, "template", "default-template", "Default Substrate ActorTemplate name")
	flag.StringVar(&defaultTemplateAtespace, "template-atespace", v1alpha1.DefaultSystemNamespace, "Default Substrate ActorTemplate atespace")
	flag.StringVar(&defaultTaskImage, "default-task-image", v1alpha1.DefaultTaskImage, "Container image for tasks that do not set spec.image (env: AX_DEFAULT_TASK_IMAGE)")
	flag.StringVar(&defaultTaskCPU, "default-task-cpu", substrate.DefaultCPU, "CPU limit for tasks that omit it")
	flag.StringVar(&defaultTaskMemory, "default-task-memory", substrate.DefaultMemory, "Memory limit for tasks that omit it")
	flag.StringVar(&workerPoolName, "worker-pool-name", "kagent-default", "kagent WorkerPool name")
	flag.StringVar(&workerPoolNamespace, "worker-pool-namespace", "kagent", "kagent WorkerPool namespace")
	flag.IntVar(&wakeupTimeoutSeconds, "task-wakeup-timeout-seconds", substrate.DefaultWakeupTimeoutSeconds, "Seconds allowed for workspace bootstrap (1-3600)")
	flag.StringVar(&egressOrigins, "egress-allow-origins", "", "Comma-separated HTTP(S) or tls:// origins allowed for task egress; empty denies all")
	flag.Parse()

	if envAddr := os.Getenv("ADDR"); envAddr != "" {
		listenAddr = envAddr
	}
	if envRedis := os.Getenv("REDIS_ADDR"); envRedis != "" {
		redisAddr = envRedis
	}
	if envPass := os.Getenv("REDIS_PASSWORD"); envPass != "" {
		redisPassword = envPass
	}
	if envImage := os.Getenv("AX_DEFAULT_TASK_IMAGE"); envImage != "" {
		defaultTaskImage = envImage
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := v1alpha1.ValidateTaskImage(defaultTaskImage); err != nil {
		slog.Error("configure --default-task-image or AX_DEFAULT_TASK_IMAGE with a published multi-platform digest", "error", err)
		os.Exit(1)
	}
	if wakeupTimeoutSeconds < 1 || wakeupTimeoutSeconds > 3600 {
		slog.Error("task-wakeup-timeout-seconds must be between 1 and 3600")
		os.Exit(1)
	}
	var origins []string
	for _, origin := range strings.Split(egressOrigins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}

	slog.Info("starting ax-server",
		"listenAddr", listenAddr,
		"redisAddr", redisAddr,
		"substrateEndpoint", substrateEndpoint,
		"template", defaultTemplate,
		"defaultTaskImage", defaultTaskImage,
	)

	rClient := goredis.NewClient(&goredis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
	})
	defer rClient.Close()

	rStore := redis.NewStore(rClient, redis.Options{})
	rLocker := lock.NewRedisLocker(rClient, lock.RedisLockerOptions{})

	var reconciler server.Reconciler
	subClient, err := substrate.NewClientWithOptions(substrate.ClientOptions{
		Target:      substrateEndpoint,
		Authority:   substrateAuthority,
		TokenFile:   substrateTokenFile,
		CAFile:      substrateCAFile,
		InsecureTLS: substrateInsecureTLS,
		Plaintext:   substratePlaintext,
		Template: substrate.TemplateOptions{
			DefaultCPU:    defaultTaskCPU,
			DefaultMemory: defaultTaskMemory,
			WorkerSelector: map[string]string{
				"kagent.dev/worker-pool-name":      workerPoolName,
				"kagent.dev/worker-pool-namespace": workerPoolNamespace,
			},
			WakeupTimeoutSeconds: int32(wakeupTimeoutSeconds),
		},
		EgressOrigins: origins,
	})
	if err != nil {
		slog.Error("could not initialize substrate client", "error", err)
		os.Exit(1)
	} else {
		defer subClient.Close()
		reconciler = controller.NewTaskReconciler(subClient, defaultTemplate, defaultTemplateAtespace, defaultTaskImage)
	}

	srv := server.NewServer(rStore, server.Options{
		Locker:     rLocker,
		Reconciler: reconciler,
		Readiness: func(ctx context.Context) error {
			if err := rClient.Ping(ctx).Err(); err != nil {
				return err
			}
			return subClient.Ready(ctx)
		},
	})

	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: srv.Handler(),
	}
	httpServer.Protocols = new(http.Protocols)
	httpServer.Protocols.SetHTTP1(true)
	httpServer.Protocols.SetUnencryptedHTTP2(true)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down ax-server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
