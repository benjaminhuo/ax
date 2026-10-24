#!/usr/bin/env ruby
# Offline rendering checks. The synthetic digest is never pulled or published.

require "minitest/autorun"
require "open3"
require "yaml"

class ChartRenderTest < Minitest::Test
  CHART = File.expand_path("..", __dir__)
  DIGEST = "sha256:" + "a" * 64

  def helm(*args)
    Open3.capture3("helm", *args)
  end

  def render(*overrides, namespace: "ax-system", release: "ax")
    stdout, stderr, status = helm(
      "template", release, CHART, "--namespace", namespace,
      "--set-string", "taskRunner.image.digest=#{DIGEST}", *overrides
    )
    assert status.success?, stderr
    YAML.load_stream(stdout).compact
  end

  def server(resources)
    resources.find do |resource|
      resource["kind"] == "Deployment" &&
        resource.dig("spec", "template", "spec", "containers", 0, "name") == "ax-server"
    end
  end

  def container(resources)
    server(resources).dig("spec", "template", "spec", "containers", 0)
  end

  def env(resources)
    container(resources).fetch("env").to_h { |entry| [entry.fetch("name"), entry] }
  end

  def redis(resources)
    resources.find do |resource|
      resource["kind"] == "Deployment" &&
        resource.dig("spec", "template", "spec", "containers", 0, "name") == "redis"
    end
  end

  def test_default_connectivity_and_persistent_state
    resources = render
    args = container(resources).fetch("args")
    assert_includes args, "--substrate-endpoint=dns:///api.ate-system.svc:443"
    assert_includes args, "--substrate-authority=api.ate-system.svc"
    assert_includes args, "--redis-addr=ax-redis.ax-system.svc:6379"
    refute args.any? { |arg| arg.include?("insecure-tls") || arg.include?("plaintext") }
    assert_equal "s3://ate-snapshots/", env(resources).fetch("AX_SNAPSHOTS_BUCKET")["value"]
    assert_equal "docker.io/benjaminhuo/ax-task-runner:latest@#{DIGEST}", env(resources).fetch("AX_DEFAULT_TASK_IMAGE")["value"]
    refute env(resources).keys.any? { |name| name.start_with?("AWS_", "RUSTFS_") }

    pod = server(resources).dig("spec", "template", "spec")
    assert_equal true, pod["automountServiceAccountToken"]
    volumes = pod.fetch("volumes").to_h { |volume| [volume.fetch("name"), volume] }
    jwt = volumes.fetch("ate-token").dig("projected", "sources", 0, "serviceAccountToken")
    assert_equal "api.ate-system.svc", jwt["audience"]
    assert_equal 7200, jwt["expirationSeconds"]
    assert_equal "servicedns.podcert.ate.dev/identity", volumes.fetch("servicedns-ca").dig("projected", "sources", 0, "clusterTrustBundle", "signerName")
    container(resources).fetch("volumeMounts").each { |mount| assert volumes.key?(mount.fetch("name")) }

    pvc = resources.find { |resource| resource["kind"] == "PersistentVolumeClaim" }
    assert_equal "keep", pvc.dig("metadata", "annotations", "helm.sh/resource-policy")
    assert_equal "Recreate", redis(resources).dig("spec", "strategy", "type")
    assert_equal ["--appendonly", "yes", "--dir", "/data"], redis(resources).dig("spec", "template", "spec", "containers", 0, "args")
    refute resources.any? { |resource| resource["kind"] == "Namespace" }
  end

  def test_missing_and_malformed_runner_digests_are_rejected
    _, stderr, status = helm("template", "ax", CHART)
    refute status.success?
    assert_includes stderr, "taskRunner.image.digest"
    _, stderr, status = helm("template", "ax", CHART, "--set-string", "taskRunner.image.digest=latest")
    refute status.success?
    assert_includes stderr, "digest"
  end

  def test_api_ingress_is_limited_to_this_releases_server_pods
    resources = render(namespace: "agents", release: "staging")
    policy = resources.find { |resource| resource["kind"] == "NetworkPolicy" }
    assert_equal "ate-system", policy.dig("metadata", "namespace")
    assert_equal({"app" => "ate-api-server"}, policy.dig("spec", "podSelector", "matchLabels"))
    rule = policy.dig("spec", "ingress", 0)
    assert_equal [{"protocol" => "TCP", "port" => 443}], rule["ports"]
    assert_equal 1, rule.fetch("from").length
    peer = rule.fetch("from").first
    assert_equal({"kubernetes.io/metadata.name" => "agents"}, peer.dig("namespaceSelector", "matchLabels"))
    assert_equal server(resources).dig("spec", "selector", "matchLabels"), peer.dig("podSelector", "matchLabels")
    assert_equal "server", peer.dig("podSelector", "matchLabels", "app.kubernetes.io/component")
  end

  def test_external_redis_and_password_secret
    resources = render(
      "--set", "redis.enabled=false",
      "--set-string", "redis.external.address=shared.db.svc:6379",
      "--set-string", "redis.external.passwordSecret.name=redis-auth"
    )
    assert_nil redis(resources)
    refute resources.any? { |resource| resource["kind"] == "PersistentVolumeClaim" }
    assert_includes container(resources).fetch("args"), "--redis-addr=shared.db.svc:6379"
    assert_equal({"name" => "redis-auth", "key" => "password"}, env(resources).fetch("REDIS_PASSWORD").dig("valueFrom", "secretKeyRef"))
  end

  def test_missing_external_redis_address_is_rejected
    _, stderr, status = helm(
      "template", "ax", CHART,
      "--set-string", "taskRunner.image.digest=#{DIGEST}", "--set", "redis.enabled=false"
    )
    refute status.success?
    assert_includes stderr, "redis.external.address"
  end

  def test_substrate_names_and_ca_configmap_can_be_overridden
    resources = render(
      "--set-string", "substrate.namespace=runtime",
      "--set-string", "substrate.apiService=control",
      "--set-string", "substrate.auth.audience=custom-audience",
      "--set-string", "substrate.tls.caConfigMap=substrate-ca",
      "--set-string", "substrate.tls.caKey=ca.pem"
    )
    args = container(resources).fetch("args")
    assert_includes args, "--substrate-endpoint=dns:///control.runtime.svc:443"
    assert_includes args, "--substrate-authority=control.runtime.svc"
    volumes = server(resources).dig("spec", "template", "spec", "volumes")
    ca = volumes.find { |volume| volume["name"] == "servicedns-ca" }
    assert_equal({"name" => "substrate-ca", "items" => [{"key" => "ca.pem", "path" => "trust-bundle.pem"}]}, ca["configMap"])
    refute ca.key?("projected")
    jwt = volumes.find { |volume| volume["name"] == "ate-token" }
    assert_equal "custom-audience", jwt.dig("projected", "sources", 0, "serviceAccountToken", "audience")
    assert_equal "runtime", resources.find { |resource| resource["kind"] == "NetworkPolicy" }.dig("metadata", "namespace")
    assert_equal "atenet-router.runtime.svc:80", env(resources).fetch("ATENET_ROUTER_ADDR")["value"]
  end

  def test_releases_in_different_namespaces_do_not_share_cluster_rbac_or_api_policy
    first = render(namespace: "team-a")
    second = render(namespace: "team-b")
    %w[ClusterRole ClusterRoleBinding NetworkPolicy].each do |kind|
      first_name = first.find { |resource| resource["kind"] == kind }.dig("metadata", "name")
      second_name = second.find { |resource| resource["kind"] == kind }.dig("metadata", "name")
      refute_equal first_name, second_name
    end
    binding = second.find { |resource| resource["kind"] == "ClusterRoleBinding" }
    assert_equal "team-b", binding.dig("subjects", 0, "namespace")
    role = second.find { |resource| resource["kind"] == "ClusterRole" }
    assert_equal role.dig("metadata", "name"), binding.dig("roleRef", "name")
  end

  def test_existing_and_ephemeral_redis_storage
    resources = render("--set-string", "redis.persistence.existingClaim=existing-data")
    refute resources.any? { |resource| resource["kind"] == "PersistentVolumeClaim" }
    assert_equal "existing-data", redis(resources).dig("spec", "template", "spec", "volumes", 0, "persistentVolumeClaim", "claimName")

    resources = render("--set", "redis.persistence.enabled=false")
    refute resources.any? { |resource| resource["kind"] == "PersistentVolumeClaim" }
    assert_nil redis(resources).dig("spec", "template", "spec", "volumes")
    assert_nil redis(resources).dig("spec", "template", "spec", "containers", 0, "volumeMounts")
  end

  def test_preprovisioned_identity_and_network_policy
    resources = render(
      "--set", "server.serviceAccount.create=false,server.rbac.create=false,substrate.networkPolicy.create=false",
      "--set-string", "server.serviceAccount.name=custom-ax"
    )
    refute resources.any? { |resource| %w[ServiceAccount ClusterRole ClusterRoleBinding NetworkPolicy].include?(resource["kind"]) }
    assert_equal "custom-ax", server(resources).dig("spec", "template", "spec", "serviceAccountName")
  end

  def test_resource_names_remain_within_kubernetes_limits
    resources = render("--set-string", "fullnameOverride=#{"x" * 100}")
    names = resources.map { |resource| resource.dig("metadata", "name") }
    assert names.all? { |name| name.length <= 63 }, names.inspect
    identities = resources.map do |resource|
      [resource["apiVersion"], resource["kind"], resource.dig("metadata", "namespace"), resource.dig("metadata", "name")]
    end
    assert_equal identities.uniq, identities

    first = render(release: "ax-#{"x" * 47}a")
    second = render(release: "ax-#{"x" * 47}b")
    refute_equal server(first).dig("metadata", "name"), server(second).dig("metadata", "name")
  end

  def test_invalid_connection_and_auth_values_are_rejected
    ["substrate.snapshotLocation=http://rustfs:9000/bucket/", "substrate.auth.expirationSeconds=1", "server.replicas=0"].each do |setting|
      _, stderr, status = helm(
        "template", "ax", CHART, "--set-string", "taskRunner.image.digest=#{DIGEST}", "--set", setting
      )
      refute status.success?, setting
      assert_includes stderr, setting.split("=").first.split(".").last
    end
  end
end
