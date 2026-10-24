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

### RustFS 快照配置

`AX_SNAPSHOTS_BUCKET=s3://ate-snapshots/` 使用 S3 URI，指向 Substrate Helm chart 默认创建的 RustFS bucket。
Substrate Chart 默认生成 `gs://ate-snapshots/`，需要在已有 release 的 Helm values 中显式设置：

```yaml
ateApi:
  snapshotLocation: s3://ate-snapshots/
```

使用与已安装 release 一致的 chart 版本合并该配置并升级，使
`ate-system/ate-api-server-envvars` 中的 `ATE_API_SNAPSHOT_LOCATION` 同步为 `s3://ate-snapshots/`。
快照 URI 与对象存储服务端点是两个独立配置，不能将快照 URI 替换成 RustFS 的 HTTP 地址。
`http://rustfs.ate-system.svc:9000`、AWS 区域及凭据由 Substrate 的 control/node objectstore 插件配置，
AX 仅通过 Control API 提交快照存储位置，不直接读写 RustFS，也不需要 RustFS 的账号或密码。
如果安装时修改了 bucket，请同步核对 `ate-system/ate-api-server-envvars` 的 `ATE_API_SNAPSHOT_LOCATION`。
未开启 `PerTemplateSnapshotLocation` 预览特性时，服务端使用自身的集群级快照位置；
开启后才使用 AX 提交的模板级位置，因此需要同步设置服务端与 AX 的配置。

### Substrate API 授权

kagent 安装器默认只把 `spiffe://cluster.local/ns/kagent/sa/kagent-controller`
设为 bootstrap owner，AX 的 JWT principal 是 `system:serviceaccount:ax-system:ax-server`。
清单中的 Kubernetes ClusterRole 只允许读取模型 Secret，不会授予 Substrate API 权限。
AX 每次协调任务都会调用 `CreateAtespace`，因此仅授予某个 atespace 的 editor 权限还不够。

在 Substrate release 的现有 Helm values 中保留全部已有 owner，并加入 AX principal，例如默认安装为：

```yaml
ateApi:
  authorization:
    bootstrapOwners:
      - spiffe://cluster.local/ns/kagent/sa/kagent-controller
      - system:serviceaccount:ax-system:ax-server
```

使用与已安装 release 一致的 chart 版本合并这些 values，再升级 release；
或者由现有 owner 通过 Substrate GlobalAccessPolicy 授予 AX global owner。
这两种授权都覆盖所有 atespaces，与 AX 当前自动创建 atespace 的实现有关。
本目录不会自动更改已安装的 Substrate release 或授权策略，也不会借用 kagent controller 的身份。

### 当前源码的 API 兼容性前提

只修改部署清单不能保证当前 AX 与工作区的定制 Substrate 完整兼容：

- AX `go.mod` 使用 `github.com/agent-substrate/substrate v0.5.0`，
  当前定制仓库模块为 `go.ate.dev/substrate`。AX 的 `internal/substrate/client.go`
  仍构造旧的 `Resources{Limits: []*Limits{Name, Quantity}}`；
  定制服务端要求 `ResourceRequirements{Limits: Resources{cpu_millicores, memory_bytes}}`，且 CPU、内存均必填。
  需要适配客户端 protobuf/资源转换并重新发布 ax-server，YAML 中的 Pod resources 不能替代 ActorTemplate resources。
- 定制服务端校验 ActorTemplate 容器镜像必须带 digest。
  runner 仍可发布 `latest` 标签，但部署前必须将 `AX_DEFAULT_TASK_IMAGE` 改为
  `docker.io/benjaminhuo/ax-task-runner:latest@sha256:<实际双架构索引摘要>`。
  显式填写的 `Task.spec.image`（包括 `examples/task.yaml`）同样需要有效 digest。
  ax-server 自身是 Kubernetes Pod，其 `latest` 标签不受此项 ActorTemplate 校验影响。
- kagent 已安装 `kagent/kagent-default` WorkerPool；AX 的模板当前没有 workerSelector，
  不会按 kagent 的池标签显式限定到该池。如需专用或固定 WorkerPool，需要在 AX 模板构建逻辑中设置 selector。
  当前 AX 会为任务自行创建模板，修改 `--template-atespace` 不会为这些模板增加 selector。

以上结论基于本地 `~/src/ai/kagent/kagent` 与 `~/src/ai/kagent/substrate` 源码；
实际安装版本还需核对。尚未执行构建、集群部署或端到端任务验证。

满足上述授权、镜像摘要和 API 兼容性前提，且镜像构建、推送完成后执行：

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
