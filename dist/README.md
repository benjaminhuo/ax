# AX 预构建镜像与 Kubernetes 部署

需要 Helm 部署时使用 [`../charts/ax`](../charts/ax/README.md)，其中包含与 kagent 定制 Substrate 配合的配置、前提和安装步骤。

本目录基于源码提交 `0f085aa` 的 `Makefile`、`README.md` 和 `deploy/` 配置生成。
这里只准备了文件，没有执行镜像构建、推送或集群部署；下面的镜像标签需要先按说明发布。
下文示例命令在仓库根目录执行；构建脚本会自行定位仓库根目录，也可以从其他目录通过绝对路径调用。

## 文件与镜像

| 文件 | 用途 |
| --- | --- |
| `dist/ax-server.yaml` | Namespace、ServiceAccount、RBAC、ax-server Deployment 和 Service |
| `dist/redis.yaml` | Redis Deployment、Service 和 Namespace |
| `dist/README.md` | 构建、验证和部署说明 |
| `dist/scripts/build-all.sh` | 构建并推送两个 AX 组件的双架构镜像 |
| `dist/scripts/build-ax-server.sh` | 单独构建并推送 ax-server 双架构镜像 |
| `dist/scripts/build-ax-task-runner.sh` | 单独构建并推送 runner 双架构镜像 |
| `dist/scripts/common.sh` | 构建脚本共享配置和命令执行函数，无需单独运行 |

| 组件 | 镜像 | 目标平台 |
| --- | --- | --- |
| ax-server | `docker.io/benjaminhuo/ax-server:latest` | `linux/amd64`、`linux/arm64` |
| ax-task-runner | `docker.io/benjaminhuo/ax-task-runner:latest` | `linux/amd64`、`linux/arm64` |
| Redis | `redis:7-alpine` | 使用上游镜像，部署前可检查其平台列表 |

两个 AX 组件分别使用 Docker Hub 仓库 `benjaminhuo/ax-server` 和 `benjaminhuo/ax-task-runner`，最终标签均为 `latest`。
每个最终标签应指向包含两种架构的镜像索引，拉取方按运行平台选择镜像。
runner 的 `latest-amd64` 和 `latest-arm64` 是合并镜像索引时使用的单架构标签，部署使用最终的 `latest`。

## 构建环境

- Go：满足根目录 `go.mod` 的要求（当前为 `1.27.1`）。
- `ko`：构建 ax-server，无需运行 Docker daemon。
- Docker、Buildx 和运行中的 Docker daemon：构建 task-runner。
- Buildx builder 需支持 `linux/amd64` 与 `linux/arm64`，可使用原生节点或模拟执行。
- Docker Hub 的 `benjaminhuo/ax-server` 和 `benjaminhuo/ax-task-runner` 仓库推送权限。

先登录 Docker Hub，`ko` 和 Docker 使用已配置的仓库凭据：

```bash
docker login docker.io
```

若尚无合适的 Buildx builder，可以创建一个，并检查其支持的平台：

```bash
docker buildx create --name ax-multiarch --driver docker-container --use
docker buildx inspect --bootstrap
```

若已有支持所需平台的 builder，直接使用即可。

## 构建脚本入口

先预览全部构建命令，不需要安装构建工具，也不会创建二进制、构建镜像、推送镜像或访问集群：

```bash
./dist/scripts/build-all.sh --dry-run
```

准备好构建环境并登录仓库后，一次构建并推送两个组件：

```bash
./dist/scripts/build-all.sh
```

脚本默认发布本说明中的两个 `latest` 镜像，遇到失败即停止。
脚本不会安装依赖、创建 Buildx builder、修改部署清单或部署到集群。
所有入口支持 `--help` 和 `--dry-run`。

可通过环境变量修改仓库或标签，例如：

```bash
IMAGE_NAMESPACE=docker.io/benjaminhuo TAG=v1 ./dist/scripts/build-all.sh --dry-run
KO_DOCKER_REPO=docker.io/benjaminhuo/ax-server TAG=v1 ./dist/scripts/build-ax-server.sh --dry-run
TASK_RUNNER_REPO=docker.io/benjaminhuo/ax-task-runner TAG=v1 ./dist/scripts/build-ax-task-runner.sh --dry-run
```

`KO_DOCKER_REPO` 只覆盖 server 仓库，`TASK_RUNNER_REPO` 只覆盖 runner 仓库；
未指定时分别使用 `${IMAGE_NAMESPACE}/ax-server` 和 `${IMAGE_NAMESPACE}/ax-task-runner`。
默认 `IMAGE_NAMESPACE=docker.io/benjaminhuo`、`TAG=latest`。
使用其他仓库或标签发布时，需同步修改 `dist/ax-server.yaml` 的服务器镜像与 `AX_DEFAULT_TASK_IMAGE`。

## 1. 提前构建并推送 ax-server

```bash
./dist/scripts/build-ax-server.sh
```

脚本执行 `ko build --bare --platform=linux/amd64,linux/arm64 --tags=latest ./cmd/ax-server`。
`ko build` 默认会推送镜像，项目根目录的 `.ko.yaml` 提供基础镜像和 Go 编译配置。
`--bare` 使镜像直接发布到 `benjaminhuo/ax-server`，避免默认行为在仓库名后附加 Go 包路径摘要。
这里的变量名是 `KO_DOCKER_REPO`。

## 2. 提前构建并推送 ax-task-runner

runner 需要根目录 `Dockerfile.task-runner` 中的 Python、Antigravity 和系统工具。
不直接使用 `.ko.yaml` 中的 runner 配置，以保留完整运行环境。

运行：

```bash
./dist/scripts/build-ax-task-runner.sh
```

当前 Makefile 的 `build-task-runner` 固定编译 `linux/amd64`，脚本会先交叉编译
`bin/linux_amd64/ax-task-runner` 和 `bin/linux_arm64/ax-task-runner`。
然后使用根目录的 `Dockerfile.task-runner` 分别构建并推送 `latest-amd64`、`latest-arm64`，
最后执行 `docker buildx imagetools create`，将两种架构合并到 `latest` 镜像索引。
Dockerfile 中存在 `ARG TARGETARCH=amd64`，脚本为每个平台显式传入 `TARGETARCH`，确保复制对应架构的二进制。

根目录 `.dockerignore` 已允许上述两种架构的 runner 二进制进入构建上下文。
Dockerfile 中的 `google-antigravity` 及其依赖也需要支持目标平台；实际构建结果需在发布时确认。

## 3. 检查发布结果

```bash
docker buildx imagetools inspect docker.io/benjaminhuo/ax-server:latest
docker buildx imagetools inspect docker.io/benjaminhuo/ax-task-runner:latest
docker buildx imagetools inspect docker.io/library/redis:7-alpine
```

确认每个镜像索引中同时包含 `linux/amd64` 和 `linux/arm64`。
Buildx 可能额外生成证明元数据条目；以实际运行平台条目为准。
AX 使用 Agent Substrate 执行任务，还需要相应 WorkerPool、运行时与节点支持目标架构。

ax-server 的 Pod 镜像可选择固定为 `docker.io/benjaminhuo/ax-server@sha256:...`。
kagent 定制 Substrate 要求 runner 固定 digest，部署前需将 `AX_DEFAULT_TASK_IMAGE` 改为
`docker.io/benjaminhuo/ax-task-runner:latest@sha256:<实际索引摘要>`，详见下文兼容性前提。
应使用包含两种架构的顶层索引 digest，避免固定成单个平台的 digest。

## 4. 使用预构建镜像部署

部署前需已安装 Agent Substrate。当前清单按 kagent 安装的定制 Substrate 配置，
Control API 地址为 `dns:///api.ate-system.svc:443`，TLS 服务名为 `api.ate-system.svc`。
使用原有的 ServiceDNS ClusterTrustBundle 验证服务端证书，
并使用 audience 为 `api.ate-system.svc` 的投射 ServiceAccount token 认证。
这是 kagent 安装器配置的 Kubernetes JWT provider 支持的方式；
AX 当前客户端不支持 kagent controller 使用的 Pod 客户端证书，无需额外挂载无法使用的客户端证书。
命名空间的 `ate.dev/api-access: "true"` 标签用于通过 Substrate 的 API NetworkPolicy，不能替代 API 授权。
镜像仓库需要能被 Kubernetes 节点及 Substrate 的镜像拉取路径访问；私有仓库按各自运行环境配置拉取凭据。

### Substrate v0.6.0-alpha3 运行配置

本分支固定 kagent fork 的 Substrate v0.6.0-alpha3（配套 kagent v1.0.0-alpha17）。
源码已迁移到 `go.ate.dev/substrate/pkg/proto/ateapipb`，资源仍使用字符串数量的 `Resources{Limits: ...}`。
CPU、内存必须同时存在；未指定的项由 AX 的 `--default-task-cpu=1`、`--default-task-memory=1Gi` 补齐。
YAML 中 server Pod 的 resources 不会替代任务沙箱资源。

快照位置由每个 ActorTemplate 的 `snapshot_config.storage_location` 指定；清单使用 `AX_SNAPSHOTS_BUCKET=s3://ate-snapshots/`。
alpha3 没有 Substrate Chart 的 `ateApi.snapshotLocation` 配置，也没有 `PerTemplateSnapshotLocation` 开关。
RustFS endpoint、AWS 区域、path style 和凭据由 Substrate 的 control/node objectstore 插件管理，AX 不直接访问 RustFS。
URI 的 bucket 必须已经创建；不要把 S3 URI 替换成 RustFS 的 HTTP endpoint。

清单通过 `--worker-pool-name=kagent-default` 和 `--worker-pool-namespace=kagent` 选择现有池。
模板不再设置 SandboxConfig；运行时由所选 WorkerPool 决定。
`--egress-allow-origins` 是出站允许列表，按实际 Git、模型服务和依赖下载主机调整；空列表拒绝所有出站访问。
HTTP(S) 目标仅填写 origin，允许 `https://*.example.com` 形式的单标签通配符；`tls://host:port` 可选择 TLS 透传。
AX 在 Resume 前创建或更新 Actor 策略，配置 HTTPS 拦截所需的动态 CA，不将集群 CA 烘焙进镜像。

模板关闭 golden snapshot，以避免预热运行真实 Task；工作区和 `/ax` 各有持久卷。
`--task-wakeup-timeout-seconds=900` 为最长十分钟的工作区 bootstrap 留出时间。
VOLUMES 快照保留持久卷内容，根文件系统和进程不保证恢复；工具链应预装进 runner 或安装到持久目录。
暂停成功表示最新快照已完成上传；初始未运行的暂停任务除外。

### Substrate API 授权

AX 默认 JWT principal 是 `system:serviceaccount:ax-system:ax-server`。
Kubernetes ClusterRole 仅用于读取模型 Secret，不授予 Substrate API 权限。
AX 先 GetAtespace，仅在 NotFound 时 Create；预先创建的 atespace 可以按 scoped editor 授权，自动创建需要全局创建权限。

如果使用 bootstrap owner 配置方式，将以下配置合并到**已有 Substrate release**，保留所有原有 owner：

```yaml
ateApi:
  authorization:
    bootstrapOwners:
      - spiffe://cluster.local/ns/kagent/sa/kagent-controller
      - system:serviceaccount:ax-system:ax-server
```

也可以由现有 owner 通过 AccessPolicy 授权，避免扩大 bootstrap owner 列表。
部署前必须使用本分支重新构建的 ax-server；尚未验证任何已发布镜像或集群运行兼容性。
AX 在启动时校验默认 runner 的 digest，因此必须先把清单中的标签替换为真实的多架构 index digest。
`/readyz` 检查 Redis/Substrate 连接，`/healthz` 只检查进程。

满足上述授权、镜像摘要和运行配置前提，且镜像构建、推送完成后执行：

```bash
kubectl apply -f dist/redis.yaml
kubectl apply -f dist/ax-server.yaml

kubectl -n ax-system rollout status deployment/ax-redis
kubectl -n ax-system rollout status deployment/ax-server
```

以后重复执行这组部署命令即可复用已发布镜像，部署端无需 Go、ko 或构建源码。
清单中 ax-server 使用 `imagePullPolicy: Always`，每次启动 Pod 时检查 `latest` 当前指向的镜像，并可复用已有镜像层；不会构建镜像。
仅重新推送 `latest` 并执行 `kubectl apply` 不会自动重启已有 Pod，更新服务器镜像后执行：

```bash
kubectl -n ax-system rollout restart deployment/ax-server
kubectl -n ax-system rollout status deployment/ax-server
```

更新 runner 镜像不会自动替换已运行的任务；其拉取与缓存行为由 Agent Substrate 的运行环境决定。

Deployment 中保留了 runner 的发布标签，部署前按上述要求补充实际索引 digest：

```yaml
env:
  - name: AX_DEFAULT_TASK_IMAGE
    value: "docker.io/benjaminhuo/ax-task-runner:latest"
```

只有未设置 `spec.image` 的 Task 才使用这个默认 runner。
`examples/task.yaml` 已显式指定官方镜像；使用自建 runner 时，请修改其 `spec.image` 为
发布后查询到的 `docker.io/benjaminhuo/ax-task-runner:latest@sha256:<实际索引摘要>`，
或删除该字段以使用已固定 digest 的服务器默认值。
Task、Workspace、Model 仍通过 `ax apply` 提交，不作为 Kubernetes 资源执行 `kubectl apply`。

## 与现有 Makefile 的区别

- `make deploy` 调用 `ko apply`，会进入构建流程；复用预构建镜像时使用本目录的清单和 `kubectl apply`。
- `make build` 只构建本地 Go 二进制。
- `make push` 目前只依赖 `push-task-runner`，不会构建 ax-server。
- `make push-task-runner` 会重新进入 runner 构建流程，只处理 amd64，并调用 GCR 专用的 `gcloud` 摘要查询；本说明使用 Docker Hub 和独立的双架构构建命令。
