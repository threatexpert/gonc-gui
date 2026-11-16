# VPN IPv6 待解决问题

更新日期：2026-11-16。排查基线：`v1.2.33`，提交 `b9996e1`。

本文记录 Windows、macOS、Android 的 IPv6 接管、路由清理和连接失败反馈问题。下方“问题总览”及详细根因保留修复前的排查基线；当前实施状态以本节为准。尚未完成实机验收的项目不能视为已经验证解决。

涉及 Gonc／gonetcat 或 Android AAR 构建来源仓库的变更需用户审批。用户已批准本轮移动封装及 AAR 更新，实施状态见下节；历史阶段中的“未修改 gonc/AAR”仅适用于当时。

## 本轮实施状态（2026-11-16）

### 最新：同步 tun2socks 修复提交

gonc 与 GUI 的顶层 replace 均固定到已推送的 `4b0120a38bf2ea724209882f0d7d5352dc75a436`（`v2.0.0-20261116123949-4b0120a38bf2`），同步 go.sum。本次包含 deferred HTTP/SOCKS4 握手超时及取消、RST 输出不持有全局准入锁、生命周期取消不进入失败缓存、IPv6 接收校验卸载标志兼容修复；原模式、2048 名额和 SOCKS5 30 秒握手默认值不变。gonc 的依赖更新已提交为 `ee76a9aa423e806f7cf43b63e87188b5c1da5ec2`，用户推送后 GUI 已同步固定到 `v2.6.13-0.20261116125136-ee76a9aa423e`。AAR 从该提交相同源码重新构建。构建保持 `GOWORK=off`，不依赖本地 tun2socks 源码或 workspace。下方依赖版本记录为历史阶段。

验证结果：新版依赖的 core/tunnel/SOCKS5 回归测试、gonc 公共包/移动封装测试、GUI 全量 Go 测试及 Android 140 项单元测试通过。Windows 已通过正常 build.bat 构建，构建信息确认 gonc 为 ee76a9a、tun2socks 为 4b0120a；新 AAR 原生库同样确认包含 4b0120a。Android release APK 版本名为 1.2.35，签名校验通过，APK 与 AAR 的 libgojni.so SHA-256 一致。产物为 build/bin/gonc-gui.exe、android/app/libs/mobilegonc.aar 和 android/app/build/outputs/apk/release/app-release.apk。实机 VPN 测试待用户验收。

### 历史：公共引擎固定到远程提交

GUI 的 go.mod/go.sum 现固定到已发布的 gonc 提交 `390e310f5e0f9d81e1be4d31ade14217236cad09`，对应 `v2.6.13-0.20261116103338-390e310f5e0f`，包含公共引擎及 v2.6.13 版本号更新；正常桌面构建使用 `GOWORK=off` 和普通 Wails 构建，不再依赖本地 workspace 或相邻源码。移除临时 workspace 文件和已注释的本地 replace 示例，保留版本化 tun2socks fork replace。本地及 GitHub 桌面脚本原本已禁用 workspace，无须增加构建步骤。Android 发布仍直接使用已提交的 AAR，不依赖相邻 gonc 仓库；本次只更新 Go 依赖引用，未重新生成 AAR。

验证：`GOWORK=off` 下模块校验、GUI 全量 Go 测试、VPN 后端 go vet 及正常 `build.bat` Windows/amd64 构建通过；macOS/amd64、macOS/arm64、Linux/amd64、Windows/arm64 VPN 后端交叉编译通过；与 GitHub 相同的 Android `assembleRelease --no-daemon` 命令通过。GitHub workflow 配置已检查，无本地模块替换，未触发会执行正式发布的远程 workflow；其他平台完整 GUI 构建仍由各平台 CI 验证。

### 历史：独立公共引擎包本地联调

gonc 新增独立 `tunengine` 包，配置结构不暴露 tun2socks 类型，统一延迟握手默认值、失败缓存、IPv6 拒绝，以及加锁的配置安装/启动/停止入口。同进程只允许一个运行引擎，重复启动返回错误；启动/停止错误向上传递，平台仍负责系统配置及失败回滚。桌面移除旧生命周期签名适配层，改为一次调用公共 Start；Android 保留原 Java API、fdsan/高位 FD 与旧调用兼容行为，仅把配置和引擎调用改到公共包。

用户选择先本地实现测试，不提交不推送。桌面测试使用忽略的 `build/bin/gonc-local.work` 联合本项目和相邻 gonetcat，通过 `wails build -m -nosyncgomod` 避免 unpublished 包被 tidy 从远程查找。正式 go.mod 中的 gonc 仍为 v2.6.12，必须等用户推送新包后固定新提交再进行 CI/正式发布；不将机器本地路径写入正式 replace。tun2socks 现在是间接依赖，顶层固定 fork 的 replace 仍保留。

新增 gonc CLI 依赖隔离回归测试（`go list -deps .`），并实际编译 CLI 检查构建信息，均未引入 tun2socks/gVisor。公共配置四种模式、自定义/非法值、默认值不修改输入、启停错误传递、重复停止、重连清除拒绝、并发启动和停止串行化均有测试。下方“已正式发布依赖”等表述是此前阶段状态，本轮新公共包尚未远程发布。

本轮验证：公共包及移动封装测试连续 10 轮通过，gonc 主包与 GUI 全量 Go 测试通过，公共包和桌面后端 go vet 通过；macOS/arm64、Linux/amd64 VPN 后端测试程序交叉编译通过。Windows 测试版为 `build/bin/gonc-gui-shared-tunengine-test.exe`。新 AAR 已打入 `android/app/build/outputs/apk/release/app-release.apk`，Android 140 项单元测试通过，APK 签名校验通过，APK/AAR 中原生库 SHA-256 一致。实际 VPN 连接、断线重连及设备稳定性仍待实机验收。

### 最新：Android 接入延迟 TCP 握手及本地 IPv6 拒绝

在 gonc 提交 `e8188775820c33ae44a4e1c2720144bdfdeb513d` 基础上更新移动封装，新增 `StartTun2SocksWithPolicy`，保留旧 `StartTun2Socks` 签名与默认关闭行为。Android 显式开启延迟 TCP 握手，握手等待 30 秒、失败缓存 30 秒/4096 条，连接代理保持底层固定 5 秒。引擎 Start 返回的错误现在会传回 Java，失败不再被当成已经启动。

仅当最终路由包含 IPv6 且探测失败时启用 `RejectIPv6`；IPv6 TCP 本地 RST、UDP（含 QUIC）本地 ICMPv6 拒绝，不进入 SOCKS5。路由保留，无 IPv6 路由时仍允许本地 IPv6。每个新隧道 Ready 代次探测一次并更新协议栈，包括 DNS 不变的情况；DNS 不变时复用 Java 持有的 TUN 接口，DNS 变化时沿用接口更新流程。保留原有 fdsan 清理、复制高位 fd、Java 关闭原 fd 的所有权约定。停止/过期探测结果会被检查，不增加周期复测。Android 实机抓包、断线恢复及长时间稳定性仍需用户验证。

验证结果：移动封装测试连续 10 轮通过，fork 的 core/SOCKS5/tunnel 测试连续 3 轮通过，Android release 单元测试 140 项全部通过。重建 `android/app/libs/mobilegonc.aar` 及 sources JAR，生成签名 APK `android/app/build/outputs/apk/release/app-release.apk`；apksigner 校验通过，APK 内 libgojni.so 与重建 AAR 内文件 SHA-256 完全一致，原生构建信息确认 tun2socks fork 为 `aab02800c33f`。旧接口和新策略接口均存在。构建有既有扫码依赖 Kotlin 元数据兼容警告但成功；go vet 的既有 `mobilegonc/source.go` 自定义 Seek 签名告警未在本轮修改。AAR 更新脚本禁用继承的 Go workspace，并用单次 Gradle daemon 执行 clean，避免后续构建缓存锁冲突。

### 最新：桌面正式引用远程 fork

`go.mod` 通过版本化 replace 固定到 `github.com/threatexpert/tun2socks/v2 v2.0.0-20261116085757-aab02800c33f`，对应提交 `aab02800c33f4ba5ad5a0496fd99b2e53cff9fa2`；`go.sum` 同步校验和，最低 Go 版本为 1.27.1。桌面默认启用延迟握手、30 秒/4096 条失败缓存、30 秒 SOCKS5 握手等待，以及按探测结果设置 IPv6 本地拒绝；连接代理固定 5 秒。

不再需要 `gonc_deferred_tcp` 标签或本地工作区。Windows/macOS/Linux 构建脚本及 GitHub 桌面工作流设置 `GOWORK=off`。项目、CLI 和 CI 统一升级为 Wails v2.17.1，恢复普通 `wails build`；删除临时 `prepare-wails-assets.cjs` 及 `-skipembedcreate` 绕过步骤。GitHub 从 go.mod 安装 Go；按用户要求，发布工作流不增加 VPN 策略测试步骤。Android APK 打包仓库内的 AAR，其更新方式与桌面依赖独立，见上一节。

用户已在 gonc 提交 `e8188775820c33ae44a4e1c2720144bdfdeb513d` 中固定同一 tun2socks fork 并调整 Go 版本；此提交未修改移动封装。gonc-gui 当前仍引用 gonc v2.6.12，顶层 tun2socks replace 必须保留（Go 不继承依赖模块的 replace），无须为此次桌面构建升级 gonc 或修改其源码。

Wails v2.17.1 本地验证：普通构建及移走 `frontend/dist` 后通过 `build.bat` 完整重新生成前端、绑定与 Windows 资源均成功，不再出现旧 CLI 的 `fmt without types` 错误。Go 全量测试、go vet 和前端 71 项测试通过。Windows 产物为 `build/bin/gonc-gui.exe`；GitHub 工作流配置已同步，但未触发远程发布运行。

以下测试版段落为历史过程记录，其中“正式依赖未改”“需要标签/本地工作区”已由本节取代。

### 历史：SOCKS5 分阶段超时测试版

当前产物：`build/bin/gonc-gui-tcp-timeouts-test.exe`，包含下方 IPv6 本地拒绝及延迟握手补丁。按最新要求，连接代理保持原版固定 5 秒，不增加此项参数；代理连接成功后，协商、认证和等待 CONNECT 结果另计默认 30 秒。两阶段不共享预算，提前失败立即结束，父上下文取消/更早截止时间仍优先，成功后清除读写 deadline。不增加界面选项或配置文件。

tun2socks `engine.Key` 仅新增 `TCPSOCKSHandshakeTimeout`，YAML/CLI 为 `tcp-socks-handshake-timeout`，零值使用 30s，负值启动前报错。仅延迟握手模式使用；原版 `DialContext` 和固定 5 秒拨号逻辑不改。嵌入调用通过 `proxy.TCPTimeouts` 与 `tunnel.NewDeferredTCPHandlerWithConfig` 配置，原构造器保留并使用默认值。非 SOCKS5 代理没有可分离的阶段，仍给 DialContext 原有固定 5 秒超时。

GUI 内部显式配置握手等待 30s，连接代理沿用固定 5s。Gonc/gonetcat、Android AAR、正式 go.mod/go.sum 未改。构建命令与下节相同，输出名改为 `gonc-gui-tcp-timeouts-test.exe`。新增测试覆盖两段预算不互相扣减、协商/认证/CONNECT 超时、父 deadline 和停止取消、成功后传输不被握手 deadline 截断、零值/负值、YAML 与 GUI 参数传递。

### 历史：本地 IPv6 TCP/UDP 拒绝测试版

当前产物：`build/bin/gonc-gui-ipv6-reject-test.exe`。本节取代下面历史条目中“探测只影响 DNS”“UDP 快速拒绝尚未实现”的描述。

- tun2socks 新增独立 `Config.RejectIPv6` / `Key.RejectIPv6`（YAML/CLI `reject-ipv6`），默认关闭，与 `DeferredTCP` 无依赖。可以只开拒绝、保持 IPv4 原版提前握手；本次 GUI 测试版继续显式开启已有延迟握手及 30 秒/4096 条失败缓存。
- 桌面端最终路由含 IPv6 且探测失败时，将 `RejectIPv6=true` 经 helper 配置传给协议栈。TCP 本地 RST，UDP 本地 ICMPv6 Type 1 / Code 1，不请求 SOCKS5。IPv4、ICMPv6 不被此策略拒绝。用户配置的路由不删除。
- 拒绝检查在扩展头解析、分片重组之后，创建转发端点之前执行。复用 gVisor 生成报文和 ICMP 限速；限速只减少回复，不允许被拒绝的数据继续转发。不修改 gVisor、系统防火墙，也不启用内部 iptables/conntrack。
- 同一次连接仅探测一次，无周期复测；重连就绪后重新探测并重建协议栈，成功则关闭拒绝，失败则开启。该配置在单个协议栈生命周期内不可变。探测误判会阻断本次连接中被接管的 IPv6 TCP/UDP，直到重连后新的结果改变策略。
- 本次没有更新 Android AAR 或将拒绝能力接入 Android；Gonc/gonetcat、正式 go.mod/go.sum 未改。仍需本地 GOWORK 和 `gonc_deferred_tcp` 标签。未带标签的构建遇到要求拒绝的配置会明确报错，不能静默忽略策略；正式发布前须发布 fork 并统一依赖/构建策略。
- 验证包含四种开关组合、仅拒绝时 IPv4 提前握手不变、TCP 不拨号即 RST、重复 SYN、UDP ICMP 校验和/原报文引用、IPv6 分片/扩展头、ICMP 限速、错误 UDP 校验和、重连两方向切换、helper JSON 传递。仍待 Windows/macOS 实机抓包验收；自动重连期间的完整 kill switch 仍不在本次范围内。

构建：设置 `GOWORK` 为 `build/bin/tun2socks-local.work` 的绝对路径，再运行 `wails build -platform windows/amd64 -skipembedcreate -m -nosyncgomod -tags gonc_deferred_tcp -o gonc-gui-ipv6-reject-test.exe`。

### 以下为此前阶段记录

- **V6-01、V6-10：已实现，待三端实机验收。** 删除桌面及安卓 IPv6 开关；以最终路由决定 IPv6 接口配置、接管和探测，探测失败不再过滤路由。旧配置的布尔开关不再参与决策；导出时保留派生的兼容字段供旧版读取。空路由仍采用 `0.0.0.0/1`、`128.0.0.0/1`、`::/0`。安卓没有 IPv6 路由时明确 `allowFamily(AF_INET6)`。
- **V6-02～V6-07：已实现，待 Windows/macOS 实机验收。** 桌面全局 `/0` 展开为两个 `/1`，避免与本地默认路由竞争；macOS 使用内核 RIB 核验前缀、接口（含索引）和旁路网关，建立本次路由归属账本，清理与失败回滚只操作匹配的自建路由；IPv6 对端也建立旁路。Windows IPv6 明确 `store=active`，安装后查询活动路由表，不再删除未曾添加的旁路路由。核验失败不发布系统 VPN 就绪。核验的是安装结果，不代表持续审计或覆盖其他更具体的路由。
- **V6-11：已实现，待出口能力变化实测。** 仅过滤 IPv6 DNS **服务器地址**，不改 AAAA 答案。全部 DNS 被过滤时明确回退 `8.8.8.8` 并记录警告；Windows 继续走已有 SOCKS5 DNS 代理，代理初始化失败则报错，不静默使用物理网络 DNS。macOS/Android 通过 TUN 添加 DNS 主机路由，无新增 DNS 代理。无效路由/DNS 配置直接失败，不再静默跳过。
- **探测策略已按 2026-11-16 最新要求调整：取消周期复测。** 同一次连接只使用首次 IPv6 探测结果；隧道重新连接就绪后再探测一次。失败状态文案恢复“不可用 / Unavailable”，不再承诺自动定时复测。探测结果仍只影响 DNS 选择，不删除用户配置的 IPv6 接管路由；安卓重连探测导致 DNS 列表变化时仍可能切换接口，需要实机压力测试。停止会等待启动/重连处理退出，避免旧线程重新开启 VPN。最新 Windows 测试版为 `build/bin/gonc-gui-ipv6-once-test.exe`（仍启用本地 tun2socks 配置驱动补丁）。
- **V6-08 仍未解决：** 桌面真实隧道中断仍沿用暂停 helper、随后重新安装的生命周期，自动重连全程持续接管/断网保护不属于本轮实现。不能把 DNS 原地更新等同于完整 kill switch。
- **V6-09 TCP 本地测试补丁已实现（2026-11-16），待实机验收：** 本地 tun2socks 克隆基于 `9c66760ef5887e7c2613983885cb41a07bc2e85a`，先等待远端代理 CONNECT 成功再发送本地 SYN-ACK；拨号失败/超时在握手前返回 RST。SOCKS5 greeting、认证和 CONNECT 响应纳入已有 5 秒连接预算，停止时取消拨号并释放连接。没有按出口探测结果直接封禁 IPv6，仍尝试实际目标；UDP／QUIC 快速拒绝尚未实现。应用是否回退仍由应用决定。

本轮 TCP 测试版：`build/bin/gonc-gui-tcp-handshake-test.exe`。通过被忽略的 `build/bin/tun2socks-local.work` 引入相邻的本地克隆，正式 `go.mod`/`go.sum`、Gonc 和 Android AAR 均未修改。复现构建：PowerShell 设置 `$env:GOWORK=(Resolve-Path build/bin/tun2socks-local.work).Path`，再执行 `wails build -platform windows/amd64 -skipembedcreate -m -nosyncgomod -o gonc-gui-tcp-handshake-test.exe`。普通构建不设置该工作区，仍使用发布版 tun2socks，不包含补丁。补丁通过双方完整 Go 测试、前端测试、macOS arm64/amd64 和 Linux amd64 VPN 后端交叉编译；原始 TCP 包测试及 SOCKS5 超时/取消测试连续 10 轮通过。未执行真实 VPN 抓包或 `-race`（本机没有可用 C 编译器）。

本地验证：Go 全量测试通过；前端 71 项测试通过；Android 136 项单元测试与签名 release 构建通过；macOS arm64/amd64、Linux amd64 的 VPN 后端交叉编译通过；Windows Wails 构建通过。当前环境未启用 CGO 且没有可用 GCC，`-race` 未执行。macOS/Android 双栈网络与抓包验收尚未执行，下方实机清单保持未勾选。

构建环境说明：本机升级 Go 1.27 后，Wails v2.12.0 内置旧版 Go 包分析器在 embed 占位扫描报 `package ... without types`；本轮在前端产物已生成的情况下使用 `wails build -platform windows/amd64 -skipembedcreate -m -nosyncgomod`，仍生成绑定及 Windows 图标/manifest。未修改系统 Wails 工具、构建脚本或项目依赖。

### TCP 失败 SYN 缓存测试版（2026-11-16）

针对 Windows 收到 RST 后仍重发相同 SYN 的实测，增加每个协议栈独立的失败缓存：四元组＋SYN 原始序列号为键，失败后固定保留 30 秒，IPv4/IPv6 合计最多 4096 条。命中直接返回 RST，不重新拨 SOCKS5，也不续期；新序列号正常拨号。访问时先清理过期项，满时淘汰最早插入项；停止清空并禁止迟到的拨号结果重新填充，重建协议栈使用新缓存。闲置时不运行清理定时器，最多保留容量上限内的记录直到下次访问或停止。

新产物为 `build/bin/gonc-gui-tcp-syn-cache-test.exe`，保留此前握手测试版用于对照。仍使用同一本地 Go 工作区，构建参数中的 `-o` 改为该文件名。原始报文回放测试覆盖相同 SYN 仅一次拨号、新 ISN 不拦截及 IPv4/IPv6 RST 的原始序列号、ACK 与校验和；缓存测试覆盖 30 秒到期、不续期、4096 上限、最早淘汰和并发停止。

### 默认关闭与公开接口兼容重构（2026-11-16）

最新测试版：`build/bin/gonc-gui-tcp-optin-test.exe`。tun2socks 的 `engine.Start()`、`core.CreateStack()`、默认 Tunnel 和 SOCKS5 `DialContext()` 恢复上游行为；`core.Config`、`engine.Key` 字段布局与现有函数签名不变。新行为仅通过 `engine.StartWithDeferredTCP()` 或 `core.CreateStackWithDeferredTCP(ctx, cfg)` 显式开启，不根据 handler 实现的接口自动开启。默认路径不分配失败缓存、SYN 跟踪或新取消上下文。

gonc-gui 的内部 starter 通过 `gonc_deferred_tcp` 构建标签选择新入口，无用户界面开关。构建本地测试版需设置前述 `GOWORK`，并使用 `wails build -platform windows/amd64 -skipembedcreate -m -nosyncgomod -tags gonc_deferred_tcp -o gonc-gui-tcp-optin-test.exe`。不带标签的构建仍走原入口，即使使用本地 fork 也不会自动开启；正式依赖仍未切换，原有发布依赖构建无需知道新增符号。后续发布 fork 并固定依赖版本时再统一正式构建策略。

新增默认模式 TCP 提前握手回归、默认 SOCKS5 取消语义、旧结构体位置初始化及公开函数签名兼容测试；开启模式继续覆盖远端优先握手、缓存及 RST 报文。新旧两种模式分别验证，不把低风险等同于绝对无风险。

### 配置驱动与可调缓存参数（2026-11-16，当前实现）

最新测试版：`build/bin/gonc-gui-tcp-config-test.exe`。按确认的新要求，允许新增结构体字段，不再维护位置初始化兼容。删除专用启动入口及 `createStack(cfg, tcpHandler)` 包装，统一使用 `engine.Start()` 和 `core.CreateStack(cfg)`：前者由 `Key.TCPDeferredHandshake` 启用，后者由非 nil 的 `Config.DeferredTCP` 启用；默认仍关闭。此前一节的独立入口方案已被本节替代。

`Key.TCPFailureCacheTTL` / `TCPFailureCacheCapacity` 可配置缓存；core 对应 `DeferredTCPConfig.FailureCacheTTL` / `FailureCacheCapacity`。未填写或零值使用 30 秒、4096 条，负数报错；重复 SYN 固定不续期，IPv4/IPv6 共用容量上限。tun2socks YAML / CLI 对应 `tcp-deferred-handshake`、`tcp-failure-cache-ttl`、`tcp-failure-cache-capacity`。gonc-gui 本地 fork 构建仍通过 `gonc_deferred_tcp` 标签内部明确配置为开启、30 秒、4096 条，无新增界面选项。

本地编译参数：设置前述 GOWORK 后执行 `wails build -platform windows/amd64 -skipembedcreate -m -nosyncgomod -tags gonc_deferred_tcp -o gonc-gui-tcp-config-test.exe`。新增自定义有效期、容量与 YAML 解析／非法值测试，保留默认模式和开启模式回归。

## 问题总览

优先级：P0 为流量绕过预期 VPN 或破坏本地网络；P1 为路由生命周期及状态可靠性；P2 为配置一致性和失败体验。编号用于后续跟踪，不表示独立提交顺序。

| 编号 | 优先级 | 平台 | 问题 | 证据状态 |
| --- | --- | --- | --- | --- |
| V6-01 | P0 | Windows、macOS | 出口探测失败后过滤 IPv6 路由，流量本地直出 | 用户复现，代码确认 |
| V6-02 | P0 | macOS | 本地已有 IPv6 默认路由时，VPN 的 `::/0` 未正常接管 | 用户复现，路由表确认；具体添加错误未捕获 |
| V6-03 | P0 | macOS | 断开时按配置删除路由，误删本地 IPv6 默认路由 | 用户对照复现，清理代码存在缺陷 |
| V6-04 | P1 | 桌面端 | 缺少路由实际生效核验，界面可能错误显示就绪 | 代码确认；macOS 有现场证据 |
| V6-05 | P1 | Windows | IPv6 路由未明确使用临时存储 | 代码确认，残留影响待实测 |
| V6-06 | P1 | Windows、macOS | 旁路路由添加与清理缺少可靠归属管理 | 代码确认，误删影响待实测 |
| V6-07 | P1 | macOS | 旁路路由只处理 IPv4，IPv6 隧道对端存在被接管风险 | 代码确认，IPv6 底层连接待实测 |
| V6-08 | P0 | 桌面端 | 自动重连暂停路由期间缺少持续接管保障 | 生命周期代码确认，泄露窗口待抓包验证 |
| V6-09 | P2 | 共用 tun2socks 的平台 | 本地 TCP 提前握手成功，影响 IPv4 回退 | 源码及上游讨论确认，项目应用行为待专项测试 |
| V6-10 | P2 | 全平台 | IPv6 开关与路由含义重复，安卓与桌面行为不同 | 代码确认，统一方案待实施 |
| V6-11 | P1 | 全平台 | DNS 过滤兜底、出口探测失败与恢复策略需明确 | 各平台现状不同，需逐项测试 |

## macOS 已复现的完整故障链

用户使用支持 IPv6 的远端，开启 IPv6，配置 `::/0`。本地 Wi-Fi 同时具有 IPv6 访问能力。

现场证据：

- 状态卡片显示远端 IPv6 出口可用、路由包含 `::/0`。
- Gonc 接口为 `utun4`，具有 `10.60.173.33` 和 `fd60:173:33::2`。
- 系统路由表有指向 `en0` 的 IPv6 默认路由，但没有指向 `utun4` 的默认路由。
- `route -n get -inet6 2606:4700:4700::1111` 选中 `en0`。
- 首次连接时 IPv6 不走 VPN；断开后本地 IPv6 也失效；再次连接则可以经 VPN 使用 IPv6。
- 重连 Wi-Fi 恢复本地 IPv6 后，上述问题再次出现。
- 改为 `::/1` 和 `8000::/1` 后，首次连接、断开恢复和再次连接均符合预期。

与证据吻合的故障链：已有系统默认路由使 VPN 的 `::/0` 添加未生效；程序未识别失败；断开时却删除 `::/0`，影响系统原有路由；再次连接时冲突条件消失，因此 VPN 路由才生效。是否具体返回 `File exists` 尚无现场命令输出，不应把该错误文本写成已证实事实。

### V6-02 默认路由安装方式

位置：`internal/winvpn/winvpn_darwin.go` 的 `configureRoutes()`。

当前直接执行 `route -n add -inet6 <CIDR> -interface <utun>`。建议在 macOS 安装层将规范化后的 `::/0` 展开为 `::/1`、`8000::/1`，去重后安装；保留用户配置的逻辑含义，不要求用户掌握平台差异，也不删除系统原有默认路由。

两条 `/1` 覆盖整个 IPv6 地址空间，比 `/0` 更具体，但更具体的直连或例外路由仍可能优先。该方案不等同于完整的系统级断网保护。WireGuard 的 macOS 脚本采用相同的默认路由展开方式，见文末参考。

### V6-03 路由误删及失败回滚

位置：同文件的 `cleanupRoutes()`、`Start()`、`Stop()`。

当前清理遍历 `config.Routes`，执行不限定接口的 IPv6 路由删除；既不记录添加是否成功，也不确认删除对象属于本程序。配置中途失败同样会调用 `Stop()`，因此失败回滚也存在误删风险。

建议：

- 记录本次实际创建的路由及其地址族、前缀、接口、网关等信息，区分预先存在的路由。
- 删除前确认当前路由仍匹配归属，并使用足够精确的删除条件；不得只凭目标前缀删除。
- 部分配置失败时只回滚本次创建的项目；添加后核验失败也必须能安全处理已产生的路由。
- 清理应幂等，路由已消失时不得误删替代路由；记录清理错误，不静默吞掉。
- 保护系统及其他 VPN 的路由，不以覆盖或删除它们来解决冲突。

### V6-04 配置成功的误判

位置：`internal/winvpn/winvpn_darwin.go` 的 `run()`；`internal/goncrunner/runner.go` 的 `applied` 回调；`internal/vpnhelper/server.go`。

当前只凭子进程退出码判断命令是否成功。Apple 的 `route` 源码存在操作失败、打印错误后仍以零退出的路径。退出码成功不能证明路由已安装。

此外，状态卡片的路由来自准备应用的配置，而非系统路由表核验结果。建议结合命令输出与系统路由查询核验前缀、接口／网关；区分配置路由与实际安装路由。任何关键路由未正确应用时，不得宣告系统 VPN 已就绪。Windows 也缺少实际路由选择核验，应补测试，不能直接假设具有 Apple 命令相同的退出码问题。

## 桌面端共有的接管缺陷

### V6-01 出口探测失败导致本地直出

位置：`internal/goncrunner/runner.go` 的 `resolveEffectiveIPv6()`、`routeLines()` 和配置构造。

当前 IPv6 开关关闭或出口探测失败，都会使有效 IPv6 标志为 false；`routeLines()` 随即过滤所有 IPv6 路由。用户即使填写了两条 `/1`，也无法绕过这种过滤。Windows 和 macOS 没有因此自动阻断本地 IPv6，用户已在两端复现直出。

建议将路由接管与出口能力解耦：IPv6 接口地址和用户路由由最终路由规则决定，探测失败不能删除或过滤接管路由。快速拒绝属于 V6-09 的额外工作；仅修复接管时，远端不通仍可能让应用等待失败，不能承诺立即回退 IPv4。

### V6-08 自动重连期间持续接管

位置：`internal/goncrunner/vpn_lifecycle.go` 的 `runVPNRouteLifecycle()`，以及 `runner.go` 传入的 `stop: helper.Stop`。

当前真实隧道中断会停止系统 VPN 配置，后续 Ready 再重新应用。自动重连期间可能暴露本地直出窗口，需要以抓包验证其时序及范围；不能把此前修复的“重连后路由能恢复”视为“重连全程不泄露”。

建议将停止转发与撤销接管拆开，自动重连期间保留接口及接管，暂时拒绝或限制转发。需评估隧道自身旁路、SOCKS5 端口变化与资源生命周期。用户主动断开则恢复普通网络。程序崩溃、接口消失后仍强制断网属于额外的系统级保护，不在现有方案保证范围内。

## Windows 与旁路路由风险

### V6-05 IPv6 路由存储范围

位置：`internal/winvpn/winvpn_windows.go` 的 `configureRoutes()`。

IPv4 添加命令指定 `store=active`，IPv6 没有指定。微软文档说明相关命令的默认存储为 persistent，可能在异常退出后留下配置。建议明确使用 active，测试正常退出、配置失败和异常终止；处理旧残留前须确认归属，不能批量删除用户路由。

Windows 的普通 VPN 路由添加和删除均限定 `interface=Gonc`，因此不同于 macOS 无接口约束地删除系统 `::/0`。但同前缀的实际选择仍受路由和接口 metric 等影响，需核验。

### V6-06 旁路路由清理

位置：两平台的 `configureBypassRoutes()`、`cleanupRoutes()`；Windows 的 `configure()`。

- Windows 当前明确跳过旁路路由添加，断开时却仍遍历 `BypassIPs` 尝试删除主机路由，缺少可靠归属限制。
- macOS 会添加 IPv4 旁路路由，但清理也只按目标删除，没有记录本次是否创建成功。

建议纳入同一套路由归属记录和精确清理机制。没有创建的路由不能在断开时删除；已有同目标路由不得视作本程序资产。

### V6-07 macOS 的 IPv6 隧道对端旁路

macOS `configureBypassRoutes()` 只接受 IPv4。全局接管 IPv6 后，若底层隧道实际使用 IPv6 对端，可能将隧道自身流量导回 utun。需补充并验证 IPv6 对端旁路、网关／接口作用域以及网络变化处理。此次现场底层为 `tcp4`，此项不是本次故障原因。

## 连接失败反馈与跨平台语义

### V6-09 tun2socks 提前完成本地 TCP 握手

当前依赖 `github.com/xjasonlyu/tun2socks/v2 v2.7.0`。其 `core/tcp.go` 先调用 `CreateEndpoint()` 完成本地握手，随后 `tunnel/tcp.go` 才拨号到代理。远端失败时关闭已建立连接，可能使应用错过 IPv4 回退；不同应用行为需要实测。

gVisor 有 `ForwarderRequest.Complete(true)` 返回 RST 的能力，但 tun2socks 当前接入接口没有暴露握手前策略回调。不能把握手后关闭连接当成等价修复。

待确定实现范围：

- 针对性方案：根据出口探测策略，在握手前拒绝 IPv6。不能解决探测成功但个别目标不可达的问题。
- 通用方案：远端连接成功后才完成本地握手，需要重构拨号时序、超时、并发及资源释放。
- UDP／QUIC 需另外设计适当的 ICMPv6 不可达反馈，不能用静默丢包冒充快速失败，也不能误伤必要控制报文。

上游 #83 仍开放；PR #235 关闭且未合并；维护者在 #320 中表示当时无实施计划。这些事实截至本记录日期。若采用 fork，建议保持通用小补丁、固定提交版本，并让桌面与 Android AAR 构建链使用一致版本；不要直接修改 Go 模块缓存。依赖库中的 `replace` 不会自动传递给主模块。

### V6-10 IPv6 开关与安卓行为

位置：`android/app/src/main/java/cn/threatexpert/gonc/GoncVpnService.java` 的 `resolveEffectiveIpv6()`、`establishInterface()`、`addRoutes()`、`addDnsServers()`。

安卓目前在有效 IPv6 为 false 时不添加 IPv6 接口地址、路由和 DNS，也不调用 `allowFamily(AF_INET6)`。Android 对受该 VPN 管理的应用默认阻断整个 IPv6 地址族；这是流量阻断，不是禁止创建 IPv6 socket。Gonc 自身被排除在 VPN 外，且仅 SOCKS5 模式不建立该 VPN 接管，不能概括为整机 IPv6 永久阻断。

讨论形成的统一建议尚未实施：

- 删除 IPv6 开关；以规范化、展开默认值后的有效路由决定接管及是否探测。
- 不含 IPv6 路由时不接管 IPv6；安卓需明确允许该地址族按底层网络正常访问。
- 含 IPv6 路由时始终安装相应接口配置和路由，不受探测结果过滤。
- 部分网段只接管相应范围；`::/0` 表示全局 IPv6 接管。空路由默认值及界面提示需保持一致。
- 旧配置和二维码中的 IPv6 开关字段需要兼容迁移，不能仅隐藏 UI 后继续读取旧布尔值。

早期讨论过“安卓只删开关、继续按探测过滤路由”的最小方案；它与上述统一语义不同，不应混合实施。

### V6-11 DNS 与探测恢复策略

- 区分 IPv6 DNS 服务器地址与 DNS 返回的 AAAA 记录；当前讨论允许按出口能力过滤前者，不计划通过过滤 AAAA 解决回退。
- 用户仅填写 IPv6 DNS、过滤后为空时，需检查各平台的实际行为，明确选择经 VPN 可达的 IPv4 DNS 兜底或报 DNS 不可用；不能悄悄依赖本地 DNS。
- Windows 已有 DNS 防泄露代理，本次讨论不授权给其他平台新增 DNS 代理。
- 公网探测失败不证明所有 IPv6 目标不可达。当前需求不考虑远端内网 IPv6，但仍应区分探测中、可用、不可用，避免一次暂时失败永久封禁。
- 新 Ready 的现有探测与失败后的周期复测是两回事；需明确复测间隔、重连重测、策略更新及 DNS 生效时机。

## 建议实施顺序与验收

1. 独立修复 macOS 默认路由展开、安装核验、归属清理和失败回滚，不依赖 tun2socks 改造。
2. 修复 Windows 临时路由和旁路清理风险，补充路由核验。
3. 实施路由与探测解耦、三端配置语义及 DNS 兜底，同时处理重连期间的接管保障。
4. 单独评估并实现握手前失败反馈，覆盖 TCP 和 UDP 的区别。

验收清单：

- [ ] macOS 本地双栈、远端支持 IPv6，填写 `::/0` 首次即正确走 VPN。
- [ ] macOS 断开后本地 IPv6 立即可用，无需重连 Wi-Fi；反复连接不损坏原有路由。
- [ ] macOS 两条 `/1` 显式配置与自动展开行为一致，重复前缀正确去重。
- [ ] 本地 IPv4-only、远端支持 IPv6 时，经 VPN 的 IPv6 访问正常。
- [ ] 出口探测失败时，用户配置接管的 IPv6 目标不从物理接口直出；未接管范围保持预期行为。
- [ ] 路由添加冲突、部分成功、命令错误但零退出码等场景不会错误显示就绪，也不会误删已有路由。
- [ ] Windows 正常断开与异常退出后的活动／持久路由状态符合设计，旁路清理不影响非本程序路由。
- [ ] IPv6 底层隧道对端不会被自身接管，网络切换后旁路仍正确。
- [ ] 自动重连、重新探测及 DNS 更新期间抓包验证接管范围内没有意外直出窗口。
- [ ] 安卓无 IPv6 路由、部分路由、全局路由及仅 SOCKS5 模式分别测试。
- [ ] IPv6-only DNS 配置、全部 DNS 被过滤、出口能力变化分别测试。
- [ ] 若修改 tun2socks，验证探测策略预先拒绝时不拨号 SOCKS5；若采用通用方案，验证远端失败时不完成本地握手。两种方案均测试允许路径、并发、超时、停止及 IPv4 回退。

## 参考资料

- [Apple route 命令源码](https://raw.githubusercontent.com/apple-oss-distributions/network_cmds/main/route.tproj/route.c)：`newroute()` 失败输出与主流程退出码。
- [WireGuard macOS wg-quick](https://raw.githubusercontent.com/WireGuard/wireguard-tools/master/src/wg-quick/darwin.bash)：`add_route()` 将 IPv6 默认路由展开为两条 `/1`。
- [微软 IPv6 路由配置](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/install-configure-ip-version-6)：路由存储参数。
- [微软 IPv6 metric](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-tcpip-interfaces-interface-ipv6settings-metric)：接口与路由 metric。
- [Android VpnService.Builder.allowFamily](https://developer.android.com/reference/android/net/VpnService.Builder#allowFamily(int))：地址族默认阻断及允许行为。
- [tun2socks issue 83](https://github.com/xjasonlyu/tun2socks/issues/83)、[PR 235](https://github.com/xjasonlyu/tun2socks/pull/235)、[讨论 222](https://github.com/xjasonlyu/tun2socks/discussions/222)、[issue 320](https://github.com/xjasonlyu/tun2socks/issues/320)：双栈回退及握手时序。
