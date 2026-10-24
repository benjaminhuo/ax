# AX Helm chart

基于 `../../dist/ax-server.yaml` 和 `../../dist/redis.yaml`，部署 AX server、ServiceAccount、
读取模型 Secret 所需的 RBAC，以及 Redis。复用 kagent 已安装的 Substrate、RustFS 和 WorkerPool。
Chart 不包含镜像构建步骤。

## 与定制 Substrate 配合使用的前提

本 chart 处理 Kubernetes 部署配置。当前工作区的 AX 源码仍使用
`github.com/agent-substrate/substrate v0.5.0`，而相邻的定制仓库模块为 `go.ate.dev/substrate`。
两者的 ActorTemplate 资源字段不同：AX 构造 `Resources{Limits: []*Limits{Name, Quantity}}`，
定制服务端要求 `ResourceRequirements{Limits: Resources{CpuMillicores, MemoryBytes}}`，且两项限额均必填。
**提交任务前，必须使用已适配该 fork 的 ax-server 镜像；chart 无法修复 Go SDK/protobuf 不兼容。**
本次没有迁移 AX 的 Go 依赖，也没有验证任何已发布镜像的运行兼容性。

此外需要：

- kagent 安装的 Substrate 已运行，Kubernetes JWT provider 接受 audience `api.ate-system.svc`。
- Substrate API 已授予 AX 的 JWT principal global owner。AX 每次协调任务都会调用 `CreateAtespace`，
  仅给某个 atespace 的 editor 权限不足。默认 principal 为 `system:serviceaccount:ax-system:ax-server`。
  Chart 的 Kubernetes ClusterRole 只用于读取 Secret，不授予 Substrate API 权限。
- 已发布 runner 的 amd64、arm64 镜像索引，并获得真实 digest。定制服务端拒绝未固定 digest 的 ActorTemplate 镜像。
- 集群支持 ServiceAccount token projection 和现有 ServiceDNS ClusterTrustBundle projection；
  也可通过 `substrate.tls.caConfigMap` 提供同一服务端 CA，ConfigMap 必须位于 AX release 命名空间。
- 使用默认 Redis 持久化配置时，有默认 StorageClass，或指定 `redis.persistence.storageClass` / `existingClaim`。

AX 会为任务创建自己的 ActorTemplate。`substrate.defaultTemplate` 和 `templateAtespace` 保留现有启动参数，
不会将 kagent 的 Harness 模板转换为 AX 模板，也不会将任务强制绑定到 `kagent-default` WorkerPool。
如需限定 WorkerPool，需在 AX 模板构建逻辑中设置相应 worker selector。

## 1. 配置已有 Substrate release

`examples/substrate-values.yaml` 是 **Substrate chart** 的覆盖文件，不能作为 AX chart 的 values 使用。
它为默认安装补充以下配置：

```yaml
ateApi:
  snapshotLocation: s3://ate-snapshots/
  authorization:
    bootstrapOwners:
      - spiffe://cluster.local/ns/kagent/sa/kagent-controller
      - system:serviceaccount:ax-system:ax-server
```

请合并到现有 Substrate release 的配置，保留所有已有 owner，使用当前安装的 chart 版本升级。
Helm 会整体替换数组，`--reuse-values` 也不会自动合并 `bootstrapOwners`。
如果 AX 使用其他命名空间或 ServiceAccount，请同步调整 JWT principal。
也可由现有 owner 通过 Substrate GlobalAccessPolicy 授予 AX global owner，从而不修改 bootstrap owner 列表。
Chart 不会自动修改现有 Substrate release 或授权策略。

对象存储由 Substrate 的 control/node objectstore 插件访问：

```yaml
ATE_STORAGE_BACKEND: s3
AWS_ENDPOINT_URL: http://rustfs.ate-system.svc:9000
AWS_S3_USE_PATH_STYLE: "true"
```

RustFS 的访问凭据也配置在这些插件上。AX 不直接访问 RustFS，不需要安装额外对象存储，
也不接收 RustFS 密钥。`s3://ate-snapshots/` 是 bucket/路径 URI，不是 HTTP 服务端点。
Substrate Chart 默认可能仍生成 `gs://ate-snapshots/`；上述覆盖将服务端和 AX 的快照位置统一为 S3 URI。
未开启 `PerTemplateSnapshotLocation` 时，服务端采用集群级位置，AX 的模板级位置不会覆盖它。

## 2. 安装 AX

以下命令在 AX 仓库根目录执行，默认 release 名为 `ax`、命名空间为 `ax-system`。
先查看 runner 镜像索引，将输出顶部的真实 `Digest:` 值填入 `RUNNER_DIGEST`：

```bash
docker buildx imagetools inspect docker.io/benjaminhuo/ax-task-runner:latest
```

该 digest 应对应同时包含 `linux/amd64` 和 `linux/arm64` 的顶层索引。
单个平台的 digest 会限制任务可运行的架构。

```bash
export RUNNER_DIGEST='sha256:<实际双架构索引摘要>'

helm upgrade --install ax ./charts/ax \
  --namespace ax-system --create-namespace \
  --set-string taskRunner.image.digest="${RUNNER_DIGEST}" \
  --wait --timeout 5m
```

占位符不是可用 digest，需替换成实际的 64 位十六进制摘要。Chart 在未提供 digest 时会拒绝渲染。
Chart 不根据部署机器的 CPU 限制 Pod 架构，server 和 runner 均可使用预构建的双架构镜像。
显式设置的 `Task.spec.image` 也必须是有效的带 digest 镜像地址；它会覆盖默认 runner。
`imagePullSecrets` 只用于 Kubernetes Pod，任务镜像拉取凭据仍由 Substrate 运行环境配置。

默认渲染：

- `ax-server:latest`，默认 runner 为 `ax-task-runner:latest@sha256:...`，仓库均为 `docker.io/benjaminhuo`。
- `dns:///api.ate-system.svc:443`，TLS authority 和 token audience 为 `api.ate-system.svc`。
- Router 地址 `atenet-router.ate-system.svc:80`，快照 URI `s3://ate-snapshots/`。
- 单副本 Redis，开启 AOF 和 1Gi PVC；替换 Pod 时保留 AX 的 task/workspace/model 数据。
- 在 `ate-system` 创建一条额外的 API NetworkPolicy，仅允许本 release 的 server Pod 访问 API 的 TCP 443。
  该规则不会修改 Substrate 自有的 NetworkPolicy，也不需要给整个 AX 命名空间开放 API 访问。
  安装者需要在 Substrate 命名空间创建 NetworkPolicy 的权限。

已有网络放行规则时可设置 `substrate.networkPolicy.create=false`，例如沿用
`ate.dev/api-access=true` 命名空间标签。关闭 chart 的规则后，需要操作者负责已有规则确实允许 AX。
Router 数据端口由 Substrate 已有 NetworkPolicy 放行。

## 常用覆盖

```yaml
server:
  image:
    repository: docker.io/benjaminhuo/ax-server
    tag: latest
  serviceAccount:
    name: ax-server

taskRunner:
  image:
    repository: docker.io/benjaminhuo/ax-task-runner
    tag: latest
    digest: sha256:<实际双架构索引摘要>

substrate:
  namespace: ate-system
  snapshotLocation: s3://ate-snapshots/

redis:
  persistence:
    enabled: true
    size: 1Gi
    storageClass: ""
```

将覆盖内容保存到独立 values 文件后，用 `-f` 传给安装命令。更换 Substrate namespace/service 时，
默认 endpoint、authority、audience、router 地址会同步派生，也可逐项覆盖。
外部 API 地址与集群内 API 不同时，可设置 `substrate.endpoint` 和 `substrate.authority`。
`substrate.auth.audience` 必须与服务端 JWT provider 的 audience 一致。
网络策略默认选择 `app: ate-api-server`；定制服务端 Pod 标签时同步修改 `substrate.networkPolicy.apiPodSelector.matchLabels`。

复用外部 Redis：

```yaml
redis:
  enabled: false
  external:
    address: redis.example.svc:6379
    passwordSecret:
      name: ax-redis-password
      key: password
```

Secret 必须位于 AX release 命名空间；不使用密码时将 `name` 留空。外部 Redis 需兼容当前 AX 的普通 TCP Redis 客户端。
内置 Redis 使用单实例模式。Chart 创建的 PVC 带 `helm.sh/resource-policy: keep`，卸载时保留，
重新安装前请确认卷的归属与恢复方式，或通过 `existingClaim` 显式复用。
`redis.persistence.enabled=false` 可关闭持久化，但重建 Redis Pod 会丢失 AX 控制平面状态。

## 验证和连接

```bash
helm lint ./charts/ax --set-string taskRunner.image.digest="${RUNNER_DIGEST}"
helm template ax ./charts/ax --namespace ax-system \
  --set-string taskRunner.image.digest="${RUNNER_DIGEST}"

kubectl -n ax-system rollout status deployment/ax-server
kubectl -n ax-system rollout status deployment/ax-redis
ax -n ax-system get tasks
```

默认 release `ax` 生成 `ax-server` Service，与 AX CLI 的自动隧道逻辑一致。
如果修改 release/fullname 或 Service 端口，请使用安装后 NOTES 中的 port-forward 和 `ax --server=...` 命令。
`/healthz` 就绪只表示 AX HTTP 服务可访问，不能证明 Substrate 授权、任务运行、快照上传与恢复均正常。

离线运行渲染回归检查（需要 Helm、Ruby 自带的 YAML 和 Minitest，不构建镜像、不访问集群）：

```bash
ruby ./charts/ax/tests/render_test.rb
```

当前工作仅做静态 Helm lint、渲染和回归检查；没有安装 release、修改集群、构建或推送镜像。
