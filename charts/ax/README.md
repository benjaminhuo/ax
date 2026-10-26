# AX Helm chart

部署 AX server、模型 Secret 读取权限和 Redis，复用 kagent 已安装的 Substrate、RustFS、WorkerPool。
本分支适配 **kagent Substrate v0.6.0-alpha3**（与 kagent v1.0.0-alpha17 的默认依赖一致）。
必须使用本分支重新发布的 AX 镜像；尚未执行集群集成验证。Chart 不执行镜像构建。

## 安装 CLI

在 AX 仓库的适配分支上执行：

```bash
cd ~/src/ai/kagent/ax
git switch kagent-substrate-v0.6.0-alpha3
make install
```

`make install` 调用 `go install`，自动编译并安装本分支的 `ax` CLI，**无需先运行 `make build`**。
`make build` 用于生成仓库内的 `bin/ax` 和 `bin/ax-server`；仅安装 CLI 时不需要这一步。
安装目录为 `GOBIN`，未设置时默认为 `$(go env GOPATH)/bin`。使用默认目录时，可执行：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
command -v ax
```

若自定义了 `GOBIN`，则将该目录加入 `PATH`，并确认 `command -v ax` 指向本次安装的二进制。
使用本分支 CLI 可保持与 ax-server 的 API 一致，并支持下文的生命周期超时配置。
`go install github.com/google/ax/cmd/ax@latest` 安装的是官方发布版，不能保证与本分支的 API 兼容。

## 准备镜像与运行配置

使用 `../../dist/scripts` 中的脚本发布 amd64/arm64 镜像，并取得 runner 的多架构 **index digest**。
`ax-server` 可使用 `latest`，Substrate 的任务镜像必须固定 digest；AX 在提交任务和构造模板时校验。

```bash
docker buildx imagetools inspect docker.io/benjaminhuo/ax-task-runner:latest
```

创建 AX values 文件，填入真实摘要和任务需要访问的目标：

```yaml
taskRunner:
  image:
    repository: docker.io/benjaminhuo/ax-task-runner
    tag: latest
    digest: sha256:<真实的双架构索引摘要>
  defaultResources:
    cpu: "1"
    memory: 1Gi
  wakeupTimeoutSeconds: 900
substrate:
  snapshotLocation: s3://ate-snapshots/ax/
  workerPool:
    name: kagent-default
    namespace: kagent
  egress:
    allowedOrigins:
      - https://github.com
      - https://*.githubusercontent.com
      - https://generativelanguage.googleapis.com
      # 仅在 bootstrap 需要时增加软件包源及其重定向目标，例如：
      # - https://pypi.org
      # - https://files.pythonhosted.org
```

`allowedOrigins` 默认空列表，拒绝所有出站访问和相关 DNS 查询。
`https://` 使用 Substrate 的 HTTPS 拦截；AX 会投射网关 CA 和公共根证书，并设置 Git/Python/curl 等信任配置。
`tls://api.example.com:8443` 表示不解密的 TLS 透传。规则支持左侧单标签通配符，HTTP(S) URL 不能带路径、查询或凭证。
不要从未受信任的 Task 自动扩展允许列表。访问被重定向到其他主机时，需要同时配置目标主机。

资源默认值属于任务沙箱，与 `server.resources` 分开。Task 只设置 CPU 或内存时，另一项继承上述默认值。
模板显式关闭 golden snapshot，防止预热阶段执行真实任务；`/workspace`（或 Task 自定义的工作区路径）和 `/ax` 使用独立持久卷。
使用 VOLUMES 快照时，根文件系统及进程不保证恢复，bootstrap 安装的工具应位于持久目录或预先构建进 runner 镜像。

## 对接已有 Substrate

- Control API 默认 `dns:///api.ate-system.svc:443`，TLS authority/JWT audience 默认 `api.ate-system.svc`。
- 保留现有 ServiceAccount JWT 和服务端 CA 投射；可通过 `substrate.tls.caConfigMap` 提供位于 AX 命名空间的 CA ConfigMap。
- Substrate 的授权系统必须允许 AX principal，例如 `system:serviceaccount:ax-system:ax-server`。
  AX 先查询 atespace，仅在不存在时创建；预创建的 atespace 可使用 scoped editor 权限，自动创建则需要全局创建权限。
- Chart 中 Kubernetes ClusterRole 只用于模型 Secret 读取，不授予 Substrate API 权限。
- `examples/substrate-values.yaml` 是已有 Substrate release 的授权覆盖示例，合并时保留所有原有 owner。
  它不能作为 AX Chart 的 values 文件使用，也不能将权限更改自动应用到已有 release。

alpha3 的快照位置直接来自 **ActorTemplate.snapshot_config.storage_location**。
AX Chart 的 `substrate.snapshotLocation` 有效；alpha3 的 Substrate Chart 没有 `ateApi.snapshotLocation`，也没有 `PerTemplateSnapshotLocation` 开关。
RustFS endpoint、区域、凭证和 path style 由 Substrate 的 control/node objectstore 插件配置。
AX 不直接访问 RustFS；应确保 URI 中的 bucket 已存在，不能把 `s3://bucket/prefix/` 换成 HTTP endpoint。

## 安装

以下命令由操作者在完成镜像发布和授权后执行：

```bash
helm upgrade --install ax ./charts/ax \
  --namespace ax-system --create-namespace \
  --values /path/to/ax-values.yaml
```

默认 Redis 使用持久卷，需要可用 StorageClass；可配置 `redis.persistence.storageClass`、`existingClaim` 或使用外部 Redis。
`substrate.defaultTemplate` / `templateAtespace` 保留旧参数兼容性；AX 为每个任务创建自己的完整模板，WorkerPool 由上述 selector 决定。
修改镜像、资源、挂载或其他启动配置后，应创建新 Task；AX 会拒绝静默复用引用旧模板的 Actor。
出站策略可以更新，并在下一次协调任务时应用；将允许列表清空会撤销已有允许规则。

`/healthz` 检查进程存活，`/readyz` 检查 Redis 和 Substrate 连接。暂停操作等待最新快照上传完成后才报告成功；从未运行的初始暂停任务可以没有快照。
CLI 的 suspend/resume 默认超时为 20 分钟，可用 `AX_LIFECYCLE_TIMEOUT=70m` 延长以匹配更长的探针超时或大快照上传。

## 离线检查

```bash
ruby charts/ax/tests/render_test.rb
helm lint ./charts/ax --set-string taskRunner.image.digest=sha256:<真实摘要>
```

渲染测试使用合成摘要，不拉取镜像、不连接集群。集群 E2E 需要后续单独执行。
