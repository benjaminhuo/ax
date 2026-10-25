# AX 适配 kagent Substrate 变更记录

本次改动让 AX 使用 kagent fork 的 Substrate `v0.6.0-alpha3` SDK，并提供复用预构建镜像、现有 Substrate、RustFS 和 WorkerPool 的部署配置。适配覆盖 Go 协议、任务模板、网络策略、暂停恢复、Helm chart 和测试工具。

记录范围为 **`7a92d7f251b691e1ed4f646e67a30e4e7722e214` 至 `bec8bd99398bfd2c46adc2bea22fd917b846fe01`，包含首尾提交**，共 7 个提交，时间为 2026-10-23 至 2026-10-25。末端固定为编写本文时的最新实现提交，方便后续继续追加记录。

## 提交索引

| 提交 | 主要改动 | 目的与效果 |
| --- | --- | --- |
| `7a92d7f` — Add image build scripts | 新增 `dist/` 静态部署清单、构建说明和 4 个构建脚本 | 提前发布 amd64/arm64 镜像，部署时直接引用已有镜像，避免每次进入 `ko apply` 构建流程 |
| `3a00136` — Add chart for ax | 新增 `charts/ax/`，包括 AX、Redis、认证、网络和离线渲染检查 | 将静态部署参数化，支持复用 kagent 已安装的 Substrate 和 RustFS |
| `069b1d3` — feat(substrate): target kagent Substrate v0.6.0-alpha3 | 固定 fork SDK，适配任务模板、资源、出站网络、快照、模板一致性和就绪检查 | 从部署配置扩展到实际 Go 客户端适配，并补充回归测试 |
| `4dabe35` — fix(cli): extend suspend and resume deadlines | 延长 CLI 生命周期 RPC 超时，支持环境变量覆盖 | 为工作区初始化和持久快照上传留出时间 |
| `c51b9d7` — feat(helm): configure AX for kagent Substrate alpha3 | 将 alpha3 运行参数接入 Helm 和静态清单，移除不适用的 Substrate values 项 | 使部署参数与适配后的服务端和目标版本一致 |
| `5b7f577` — test(substrate): align E2E setup with alpha3 and isolate local tests | 调整 Kind/E2E 安装配置，普通测试显式禁用集群 E2E | 对齐 fork 的测试环境，并避免常规测试因 `AX_E2E=true` 意外启用集群测试 |
| `bec8bd9` — docs: explain alpha3 deployment and compatibility requirements | 更新部署、兼容性、开发和 E2E 文档 | 说明新镜像、digest、权限、网络、持久化及验证边界 |

可以在仓库根目录查看包含起始提交的完整历史和差异：

```bash
git log --reverse --oneline 7a92d7f^..bec8bd9
git diff --stat 7a92d7f^ bec8bd9
```

下文按最终实现说明各项改动；早期提交中的配置以后续提交修订后的状态为准。

## 1. 提前构建多架构镜像，部署时复用

`7a92d7f` 新增 [dist/ax-server.yaml](../dist/ax-server.yaml)、[dist/redis.yaml](../dist/redis.yaml)、[dist/README.md](../dist/README.md) 和以下脚本：

| 脚本 | 具体行为 |
| --- | --- |
| [build-ax-server.sh](../dist/scripts/build-ax-server.sh) | 使用 `ko build --bare --platform=linux/amd64,linux/arm64` 构建并推送 server 镜像 |
| [build-ax-task-runner.sh](../dist/scripts/build-ax-task-runner.sh) | 用 Go 交叉编译两种架构的 runner，使用 `Dockerfile.task-runner` 分别构建、推送单架构镜像，再用 `docker buildx imagetools create` 合成索引 |
| [build-all.sh](../dist/scripts/build-all.sh) | 依次调用两个组件的构建脚本，失败时停止 |
| [common.sh](../dist/scripts/common.sh) | 统一仓库、标签、参数解析、工具检查和命令执行逻辑 |

默认镜像分别为 `docker.io/benjaminhuo/ax-server:latest` 和 `docker.io/benjaminhuo/ax-task-runner:latest`，目标平台均为 `linux/amd64`、`linux/arm64`。runner 额外发布 `latest-amd64`、`latest-arm64`，供合并顶层镜像索引使用。

构建入口支持 `--help` 和 `--dry-run`，并通过 `IMAGE_NAMESPACE`、`TAG`、`KO_DOCKER_REPO`、`TASK_RUNNER_REPO` 覆盖配置；其中 `KO_DOCKER_REPO` 只控制 server 仓库。脚本自行定位仓库根目录，预览模式不执行编译、镜像构建或推送。

server 使用 `ko` 发布 Go 程序。runner 继续使用 Docker Buildx，以保留现有 Dockerfile 中的 Python、`google-antigravity`、Git、SSH 等完整运行环境；只使用现有 `ko` runner 配置不能等价保留这些依赖。

这部分将镜像发布和 Kubernetes 部署分开：操作者发布一次镜像后，可使用静态清单或 Helm 重复部署。`imagePullPolicy: Always` 只影响镜像拉取，不会构建镜像；重新推送 `latest` 也不会自动更新已有 Pod 或任务。双架构构建流程已准备好，但镜像及其 Python/系统依赖在两个平台上的实际构建、运行结果仍需发布时验证。

## 2. 固定 fork SDK，匹配 alpha3 协议

[go.mod](../go.mod) 将原来的 `github.com/agent-substrate/substrate v0.5.0` 改为：

```go
require go.ate.dev/substrate v0.6.0-alpha3

replace go.ate.dev/substrate => github.com/kagent-dev/substrate v0.6.0-alpha3
```

对应 fork 标签的提交为 `e2fc0fa9f77ab5fc09eb01b43ccd74bd84c67c71`。kagent `v1.0.0-alpha17` 的 `go/go.mod` 使用同样的版本和 replace 方式。

生产代码、mock 服务和 E2E harness 的 protobuf 导入统一为 `go.ate.dev/substrate/pkg/proto/ateapipb`，更新 `go.sum`，并引入 `github.com/distribution/reference` 校验 OCI 镜像引用。AX 自身的 Go 模块路径及对外 Task/Workspace/Model protobuf 定义保持原有接口。

本次按 alpha3 实际协议适配：

| 项目 | alpha3 对应处理 |
| --- | --- |
| Control API | 继续使用 `ateapi.Control` 和 `ateapipb.ControlClient` |
| 模板调度 | 移除原来的 `SandboxConfig`、`sandboxclass.GVisor` 和 `gvisor-default` 硬编码，使用 `WorkerSelector.MatchLabels` 选择已有 WorkerPool |
| 资源类型 | 使用 `Resources{Limits: []*Limits{Name, Quantity}}`，保证有效配置同时含 CPU、内存 |
| 快照与探针 | 使用该版本的 `SnapshotConfig`、`GoldenSnapshotConfig` 和 `WakeupProbe.TimeoutSeconds` |
| Actor 删除 | 保留该版本支持的 `DeleteActorRequest.AnyState` |
| E2E WorkerPool | 继续使用 `ate.dev/v1alpha1`，worker 镜像入口改为 `ko://go.ate.dev/substrate/cmd/ateworker-gvisor` |

这里不再把 `sandboxClass` 的枚举或字符串转换作为 AX 模板适配入口，沙箱实现由选中的 WorkerPool 管理。alpha3 使用字符串 quantity 资源结构；此前针对其他 fork 版本讨论的 `ResourceRequirements`、`CpuMillicores`、`MemoryBytes` 结构不适用于本次目标。

这些改动让 AX 使用目标 fork 的类型和调用约定。部署时仍需从本分支重新生成镜像，旧 `ax-server` 二进制不会因为换了 chart 就获得新的 SDK 实现。

## 3. 完整构造和校验任务模板

新增 [internal/substrate/template.go](../internal/substrate/template.go)，集中管理 `TemplateOptions`、模板准备和有效配置校验；[client.go](../internal/substrate/client.go) 负责生成对应的 alpha3 ActorTemplate。

### 调度、资源与启动探针

| 配置 | 最终默认值 | 行为及效果 |
| --- | --- | --- |
| 任务 CPU | `1` | Task 未指定 CPU 时补齐默认值 |
| 任务内存 | `1Gi` | Task 未指定内存时补齐默认值；只指定其中一项也能形成完整资源配置 |
| WorkerPool 名称 | `kagent-default` | selector 使用 `kagent.dev/worker-pool-name` |
| WorkerPool 命名空间 | `kagent` | selector 使用 `kagent.dev/worker-pool-namespace`，限定目标池 |
| 唤醒探针 | HTTP `:80/readyz`，超时 `900` 秒 | 给仓库准备、bootstrap 和运行环境初始化留出时间；可配置 1–3600 秒 |
| 快照位置 | `s3://ate-snapshots/` | 每个模板显式设置 `snapshot_config.storage_location` |

上述资源配置属于任务沙箱，与 `ax-server` Pod 自身的 requests/limits 分开。CPU、内存数量校验增加 32 字符长度限制；原有正数、CPU 上限和不支持 requests 的检查继续生效。配置错误会在 AX 校验阶段暴露，减少创建模板后才被 Substrate 拒绝的情况。

### 持久目录和快照

工作区继续使用 `DurableDir`，默认挂载 `/workspace`；Task 自定义工作区路径时，模板也使用该路径。新增独立的 `/ax` 持久目录，保存 runner 私有状态，使暂停恢复同时覆盖工作文件和 AX 私有状态。

新增路径校验，要求自定义工作区为绝对路径，且不能与 `/ax`、`/run/ax/egress` 相同、互相嵌套或覆盖根目录，避免 alpha3 不允许的挂载冲突。

快照保真度继续为 `VOLUMES`，显式将 golden snapshot 模式设为 `DISABLED`，避免模板预热阶段提前执行真实任务。卷快照用于持久目录恢复，不承诺恢复根文件系统、进程或内存；bootstrap 安装的工具应放在持久目录或提前加入 runner 镜像。

### 镜像与环境变量

新增 [images.go](../pkg/apis/v1alpha1/images.go)，在 Task 显式镜像、模板准备和 server 默认镜像配置处校验 digest。默认仓库改为 `docker.io/benjaminhuo/ax-task-runner`，部署时需提供类似以下引用：

```text
docker.io/benjaminhuo/ax-task-runner:latest@sha256:<真实的双架构索引摘要>
```

runner 应使用同时包含 amd64、arm64 的顶层 index digest，避免单架构 digest 限定运行平台。server 镜像可以继续使用 `latest`，也支持固定 digest。AX 只校验镜像引用和 digest 格式，不查询远端仓库确认镜像存在或包含哪些架构。

Helm 在缺少 runner digest 时拒绝渲染；静态 YAML 保留发布标签及替换说明，未填真实 digest 时 server 会启动失败。这样可尽早发现不满足 fork 镜像要求的配置。

模板环境变量按名称排序，并检查最多 32 项、变量名合法性及值的 UTF-8/长度限制。出站信任相关变量由 AX 管理，拒绝 Task 覆盖，以保持信任配置一致。

## 4. 在运行前配置出站网络及 HTTPS 信任

新增 [internal/substrate/egress.go](../internal/substrate/egress.go)，将操作者配置的 origin 列表转换为每个 Actor 的 `default` EgressPolicy：

| origin 类型 | 对应行为 |
| --- | --- |
| `http://host[:port]` | 生成 HTTP 规则，默认端口 80 |
| `https://host[:port]` | 生成 HTTPS 拦截规则，默认端口 443 |
| `tls://host[:port]` | 生成 TLS 透传规则，默认端口 443 |

规则校验 DNS 主机名、通配符、端口和 URL 组成，去重、排序，拒绝同一主机端口上的协议冲突，并限制最多 256 条。允许列表来自 server 参数或 Helm values，不根据 Task 内容自动扩展。

策略不存在时创建，存在且规则变化时携带 UID/version 更新，对并发冲突进行有限重试。清空允许列表会在下一次准备运行时撤销已有允许规则。默认空列表拒绝所有出站访问和相关 DNS；Git 仓库、模型 API、软件包源以及重定向后的主机需要操作者明确放行。静态清单和 E2E 提供 GitHub/Gemini 示例，Helm 默认仍为空列表。

模板新增 `/run/ax/egress` 系统信息卷，投射 `egress-mitm.ate.dev` 和 `system-roots.ate.dev` 信任包，并设置 Git、curl、Python Requests、Node、AWS 等常见客户端的 CA 环境变量，使采用这些信任设置的客户端可以访问 HTTPS 拦截代理。

控制器在 resume 前协调策略：失败则阻止执行并记录 `EgressPolicyFailed`。暂停路径不依赖策略查询或更新，避免出站策略权限或服务故障阻止停止任务。

## 5. 暂停恢复、模板一致性与故障处理

### 暂停成功需要确认最新持久快照

`SuspendActor` 显式请求 `DURABILITY_MODE_SYNCHRONOUS`，随后按 generation 找到最新快照，等待其 durable 状态为 `COMPLETED` 且对象 URI 非空。等待期间核对 Actor UID、暂停状态和快照 generation，支持调用方取消，不能使用旧 generation 的完成快照代替本次快照。

从未运行、初始即暂停的 Actor 可以没有快照。其他暂停失败会将 Task 的 Ready 条件设为 False，原因为 `ActorSuspendFailed`。这让暂停成功的含义包含持久化确认，而不只表示暂停 RPC 已返回。

Actor 崩溃时仍尝试 Revert 到已有快照；Revert 失败后返回错误并保留 Actor，不再自动删除重建，保留后续恢复状态。

CLI suspend/resume 的默认 RPC 超时从 5 分钟改为 20 分钟，可通过 `AX_LIFECYCLE_TIMEOUT` 覆盖，例如 `70m`；非法、零或负时长会返回错误。这为探针等待和快照上传提供更合理的调用时间。

### 模板名称反映完整有效配置

[控制器](../internal/controller/reconciler.go) 先生成完整有效模板，再确定性序列化并计算名称摘要。镜像、环境变量、资源默认值、WorkerPool、快照位置、持久目录、信任配置和探针等启动参数均参与计算，避免只修改 server 默认值却继续复用旧模板。

对已有同名模板比较有效内容，忽略服务端 UID/version/status 等字段；并发创建返回 AlreadyExists 后也重新核对内容。Actor 已绑定其他不可变模板时明确报错，要求为新的启动配置创建新 Task，避免静默继续使用旧配置。

长任务名生成模板时截断并加入任务名摘要，满足资源名称长度要求，同时保持不同长名称任务的清理范围隔离。模板列表遍历所有分页，检测重复 page token，防止清理遗漏或陷入循环。

### 启动与依赖就绪

server 在默认镜像、模板参数、出站配置或客户端初始化错误时退出，不再以缺少 Substrate reconciler 的状态继续服务。

保留 `/healthz` 作为进程存活检查，新增 `/readyz` 检查 Redis PING 和 Substrate gRPC 连接就绪。检查总超时为 2 秒，部署 readiness probe 超时设为 3 秒。Substrate 检查只等待传输连接 Ready，不调用需要全局权限的 API，也不代表已验证所有任务权限或沙箱运行能力。

## 6. Helm chart 与现有 kagent 部署对接

新增 [charts/ax](../charts/ax/README.md)，部署 AX Deployment/Service、ServiceAccount、模型 Secret 读取 RBAC，以及可选的 Redis Deployment/Service/PVC。Chart 复用已有 Substrate、RustFS 和 WorkerPool，不安装这些组件，也不执行镜像构建。

Chart 默认连接参数与 kagent 的安装约定对齐：

| 配置 | 默认值或处理 |
| --- | --- |
| Control API | `dns:///api.ate-system.svc:443`，使用 gRPC DNS 解析 |
| TLS authority / JWT audience | `api.ate-system.svc`，均可覆盖 |
| Router | `atenet-router.ate-system.svc:80`，提供给 `ATENET_ROUTER_ADDR`，不带 HTTP scheme |
| API token | 投射 ServiceAccount JWT，默认有效期 7200 秒 |
| API CA | 默认投射 `servicedns.podcert.ate.dev/identity` 的 ClusterTrustBundle；支持使用 AX 命名空间中的 CA ConfigMap |
| API 网络访问 | 在 Substrate 命名空间新增放行规则，来源限定为该 release 的 AX server Pod；与现有 NetworkPolicy 叠加，可关闭以复用已有规则 |
| Redis | 默认启用 PVC 和 AOF，使用 Recreate 策略，PVC 设置 `helm.sh/resource-policy: keep`；支持已有 PVC、StorageClass、临时存储或外部 Redis/password Secret |

Kubernetes Secret 读取 RBAC 与 Substrate 自身授权分别配置。Chart 创建的 ClusterRole 只授予 Kubernetes Secret 的 get/list/watch，不会授予 Substrate API 权限。[substrate-values.yaml](../charts/ax/examples/substrate-values.yaml) 提供将 AX principal 加入已有 Substrate release 的授权示例，需合并并保留现有 owner。

AX 的 `EnsureAtespace` 改为先 Get，仅在 NotFound 时 Create；权限错误直接返回。这样预创建的 atespace 可使用 scoped editor 权限，自动创建仍需要全局创建权限。

Chart 通过 values、JSON Schema 和模板暴露镜像、资源、WorkerPool、探针、出站网络、认证和连接配置，渲染时检查缺少 digest、缺少外部 Redis 地址及非法参数。资源命名考虑长 release 名和跨命名空间安装，避免 ClusterRole、Binding 和 API NetworkPolicy 名称冲突。

`--template`、`--template-atespace` 保留参数兼容性；正常任务协调会构造自己的完整 ActorTemplate，无需手工维护一个可复制的基础模板。

## 7. RustFS/S3 配置及部署文档修正

将 AX 默认快照位置、`dist/ax-server.yaml`、`deploy/ax-server.yaml` 和 E2E 配置中的上游 `gs://` 示例改为 `s3://ate-snapshots/` 或其前缀。AX 的 `AX_SNAPSHOTS_BUCKET`、Helm 的 `substrate.snapshotLocation` 最终写入每个 ActorTemplate 的 `snapshot_config.storage_location`。

快照位置校验要求合法的 S3 bucket URI。RustFS 的 HTTP endpoint、区域、凭证和 path style 由 Substrate 的 control/node objectstore 插件配置，AX 不直接访问 RustFS，不向 AX Pod 注入对象存储凭证。bucket 需要预先存在，`s3://bucket/prefix/` 不能替换成 RustFS 的 HTTP endpoint。

alpha3 的 Substrate chart 没有 `ateApi.snapshotLocation`，也不依赖 `PerTemplateSnapshotLocation` 开关，因此移除初版授权示例中的无效配置，改为说明模板级存储位置。

[README](../README.md) 指向目标 fork 版本，并使用 `go install ./cmd/ax` 安装本分支 CLI；[dist 说明](../dist/README.md)、[Helm 说明](../charts/ax/README.md)、[manifest 说明](manifests.md) 和 [E2E 说明](../test/e2e/README.md) 补充 digest、权限、网络、持久化和超时要求。Task、Workspace、Model 仍由 AX API/CLI 管理，不作为 Kubernetes CRD 提交。

## 8. 测试与验证范围

新增和调整的本地测试覆盖镜像与挂载路径校验、资源补齐、有效模板哈希、长名称隔离、Atespace 权限路径、策略创建/更新/撤销及并发前提、运行前策略顺序、策略失败时仍可暂停、最新快照等待/取消、Revert 失败保留 Actor、模板内容比较、分页和依赖 readiness。

`make test` 和 Go CI 显式设置 `AX_E2E=false`。Kind 脚本默认仓库改为 kagent fork，默认版本从 `go.mod` 读取；测试部署使用 S3、独立 `ax-workers` 池和可配置出站 origin，并为专用测试安装配置 AX ServiceAccount 的 bootstrap owner。

E2E 断言改为读取 alpha3 已知类型字段，不再通过“存在未知 protobuf 字段”放宽兼容判断；增加双资源限制、WorkerPool、golden 禁用和最新 S3 快照完成检查，并校验 `/workspace` 与 `/ax` 两个目录的恢复。

本次适配阶段已完成以下本地验证；本次编写 changelog 未重新执行这些构建和测试：

| 验证 | 结果及范围 |
| --- | --- |
| `AX_E2E=false go test -mod=readonly -count=1 ./...` | 通过；真实集群 E2E 跳过 |
| `internal/substrate`、`internal/controller`、`internal/server` race 检查 | 通过，使用本地 mock/loopback 服务 |
| `go build ./cmd/...` | 通过 |
| ax-server、ax-task-runner 的 Linux amd64/arm64 交叉编译 | 通过；仅编译 Go 二进制 |
| SDK proto/生成代码与 alpha3 标签比对 | 一致 |
| Helm lint、离线渲染测试 | 通过；12 个测试、121 项断言 |
| Shell 语法、JSON/YAML 解析、差异空白检查 | 通过 |

**尚未执行镜像构建/推送、Helm 安装或任何连接 Kubernetes 集群的测试，也未验证现有已发布镜像的运行兼容性。** 后续仍需发布本分支镜像并检查双架构索引，在授权的集群环境验证 JWT/CA、Substrate 权限、WorkerPool 调度、Git/模型网络访问，以及 RustFS 上的暂停恢复与目录持久化。上述代码和本地测试通过不等同于这些集群链路已经验证。
