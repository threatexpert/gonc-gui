# Gonc

面向桌面端和 Android 的点对点安全文件传输与远程组网工具。

Gonc 通过共享口令帮助两台设备找到彼此，在相同内网可快速发现彼此并直接
内网直连，不同网络则通过 NAT 穿透和打洞建立点对点直连。不需要账号，不
上传到云端，也没有文件大小限制。Gonc 还
包含远程组网模式，可在外面轻松地和家里网络或公司网络直连组网。不用
服务器，不用账号，设备之间直接建立加密连接。

![Gonc desktop file sender](ui-pc.gif)

<p>
  <img src="ui-android1.jpg" alt="Gonc Android file sender" width="360">
  <img src="ui-android2.jpg" alt="Gonc Android remote networking server" width="360">
</p>

## 功能特性

- **直接发送文件和文件夹** - 在桌面端和 Android 之间共享多个文件或整个
  目录。
- **无需账号** - 双方使用同一个共享口令即可连接。
- **附近设备** - 在同一网络中选择设备即可发送。首次核对一次六位数字完成配对，
  之后只需接收方确认。
- **端到端加密** - 流量受 TLS 1.3 保护。TLS 证书会从共享口令自动派生，
  并强制双向认证，因此不需要 CA 证书，也能防止中间人窃听或篡改。
- **连接成功后是真正的 P2P** - 成功传输时使用真实的点对点连接。Gonc
  默认不提供官方中继服务。
- **可靠的接收模式** - 可浏览远端文件列表，下载全部或指定路径，并通过
  BLAKE3 分块修复恢复中断下载。
- **Android 和桌面端界面** - Windows 和 Android 使用同一套工作流。
- **轻松远程组网** - 在多种设备架构上运行
  `gonc -p2p <passphrase> -linkagent`，即可从桌面端或 Android 客户端
  远程连接家里或公司的网络。
- **支持 IPv4 和 IPv6 场景** - 远程组网模式在 Windows 上支持 IPv6 路由检查和
  DNS 泄漏保护选项。

## 下载

从 [GitHub Releases](https://github.com/threatexpert/gonc-gui/releases)
下载最新版本。

发布包命名类似：

| 平台 | 包名 |
| --- | --- |
| Windows x64 | `gonc-gui-<version>-windows-amd64.zip` |
| Windows arm64 | `gonc-gui-<version>-windows-arm64.zip` |
| Linux x64 | `gonc-gui-<version>-linux-amd64.tar.gz` |
| Linux arm64 | `gonc-gui-<version>-linux-arm64.tar.gz` |
| Android arm64 | `gonc-gui-<version>-android-arm64.apk` |

在 Windows 上使用远程组网功能时，请将 `gonc-gui.exe` 和 `wintun.dll` 放在同一
目录。

## 工作原理

1. 发送方选择文件或文件夹并开始共享。
2. Gonc 生成或接受一个共享口令。
3. 接收方输入或扫描同一个共享口令。
4. 双方通过公共 MQTT 信令服务器交换加密后的连接信息。
5. Gonc 尝试 NAT 穿透，并在建立真实 P2P 连接后开始传输。

信令服务器只用于帮助双方相遇。它无法看到共享口令，也无法解密交换的网络
信息。

如果双方都位于限制较强的 IPv4 NAT 后面，NAT 穿透可能失败。这种情况下可
以使用 SOCKS5 代理服务器作为中继路径。

### NAT 穿透支持情况

如果双方都有 IPv6 连接能力，通常是最容易建立直连的情况。

对于 IPv4，NAT 穿透表现取决于双方各自的 NAT 类型：

| 类型 | NAT 类型 | P2P 穿透难度 |
| --- | --- | --- |
| 1 | Full Cone NAT，完全锥形 NAT | 最容易 |
| 2 | Restricted Cone NAT，地址受限锥形 NAT | 容易 |
| 3 | Port Restricted Cone NAT，端口受限锥形 NAT | 中等 |
| 4 | Symmetric NAT，对称型 NAT | 最难 |

Gonc 预期的 IPv4 穿透能力如下：

| A 端 NAT | B 端 NAT | 预期结果 |
| --- | --- | --- |
| 类型 1 | 类型 1、2、3 或 4 | TCP 和 UDP 可用 |
| 类型 2 | 类型 2、3 或 4 | TCP 和 UDP 可用 |
| 类型 3 | 类型 3 或 4 | UDP 可用 |
| 类型 4 | 类型 4 | 无法直接连接；需要用户自备支持 UDP ASSOCIATE 的 SOCKS5 代理服务器作为中继路径 |

## 附近设备（同一网络）

**附近设备**页面会列出同一局域网中的 Gonc 设备，无需口令。

1. 在两台设备上打开**附近设备**（也可在**发送文件**页点击**发送到附近设备**，或在
   **接收文件**页点击**从附近设备接收**）。只有打开附近设备页面时，本机才能被发现；
   页面顶部会显示其他设备看到的本机名称。
2. 添加文件、文件夹、文本或剪贴板内容。
3. 在对方设备卡片上选择发送方式：
   - **配对并发送**：两台设备会显示相同的六位数字，确认一致后在两边点击确认。
     两台设备会保持配对，之后向已配对设备发送时会跳过这一步。
   - **发送**（仅未配对设备）：无需核对数字直接发送。适合可以让网络中任何人看到
     的文件：此方式无法证明对方设备的身份，两台设备也不会因此配对。
4. 接收方会看到发送方名称和文件列表，点击**接收**。来自未配对设备的请求会标注
   **未配对**。文件保存到**本机**区域显示的文件夹；已存在的文件不会被覆盖（副本
   保存为 `名称 (1).扩展名`）。如只想接收已配对设备的文件，可在**本机**区域关闭
   **接收未配对设备发送的文件**。

附近设备传输在两台设备之间直接通过 TLS 1.3 进行，不会连接 MQTT 或 STUN 服务器，
因此在无法访问互联网的网络中也能使用。设备通过 UDP 19731 端口的组播互相发现，
并在 TCP 19732 端口接受连接。只有打开附近设备页面时 Gonc 才会发送发现报文，在其他
页面不会在网络中广播，正在进行的传输不受影响。即使某台设备的防火墙阻止入站连接也能使用：每次广播
都会请求附近设备回复；直连无响应时，两台设备会像口令模式的局域网发现一样，互相
组播并同时向对方发起连接。右下角的**活动日志**会显示实际使用的连接方式。
如果对方设备没有出现（部分访客或办公 Wi-Fi 会阻止组播，或两台设备不在同一网络），
请改用**发送文件**和**接收文件**通过口令传输。

广播内容使用会定期变化的值而非固定设备 ID，因此无法跨网络追踪设备；同一网络中的
其他人仍可看到设备名称。关闭**允许附近设备发现本机**即可停止广播。配对密钥在
Windows 上使用 DPAPI 加密；在 macOS 和 Linux 上目前保存在仅当前用户可读的文件中。

## 发送文件

桌面端发送内容能力：

| 内容来源 | Windows 桌面端 | macOS/Linux 桌面端 |
| --- | --- | --- |
| 文件 | 支持 | 支持 |
| 文件夹 | 支持 | 支持 |
| 手动输入文本 | 支持 | 支持 |
| 剪贴板文本 | 支持 | 支持 |
| 剪贴板原生文件 | 支持 | 不支持；剪贴板中没有可读取的文本时，应用会显示明确的不支持提示 |
| 剪贴板原生图片 | 支持（保存为生成的 PNG） | 不支持；剪贴板中没有可读取的文本时，应用会显示明确的不支持提示 |

界面只有一个 **Add** 按钮，点击后可在 **File**、**Folder**、**Text** 和
**Clipboard** 四个选项中选择。文件和文件夹也可通过拖放添加。每一行都有单独
移除按钮，**Clear all** 可清空整个列表。发送会话运行期间的列表变化会同步给
接收方，包括清空列表；接收方刷新后会看到空列表。

1. 打开 **Send Files**。
2. 添加要共享的内容。
3. 使用生成的口令，或输入你自己的强口令。
4. 通过可信渠道将口令或二维码分享给接收方。
5. 保持发送方运行，直到接收方完成下载。

## 接收文件

1. 打开 **Receive Files**。
2. 输入或扫描发送方口令。
3. 选择保存目录。
4. 连接后浏览远端目录，然后下载选中的文件或当前文件夹。

断点续传模式会使用 BLAKE3 清单校验本地分块，再决定是否复用。对于中断的
下载，Gonc 会从最后一个校验通过的完整分块继续，而不是盲目信任本地文件
大小。

收到的文件存在、可读、是常规文件，并且大小与当前远端列表一致时，接收列表
会将它标记为本地可用。这只是便捷性检查，不代表内容完整性验证。桌面端的
“定位”按钮只会在资源管理器、Finder 或当前平台的文件管理器中显示文件；
Gonc 桌面端不会直接打开收到的文件。

Android 端可在当前接收会话中打开、选择其他应用打开、分享或查看本地可用
文件的信息；这些动作不会作为下载历史跨应用启动保存。打开 APK 时，Gonc
只会将文件交给 Android 系统软件包安装器，由系统处理未知来源授权和安装
确认；Gonc 不会静默安装软件包。

## 远程组网

Gonc 也可以在设备之间建立加密组网隧道。人在外面时，可以轻松和家里网络或
公司网络直连组网；不需要自建中心服务器，不需要账号，双方通过共享口令和
P2P 穿透建立连接。服务端部署方式很简单：在家里、公司或远端设备上运行：

```text
gonc -p2p <passphrase> -linkagent
```

然后从桌面端或 Android 客户端连接它。

- **组网服务端** 运行 `linkagent` 端点，并可将这台设备作为流量出口。
- **组网客户端** 连接到服务端，并可启动系统网络接口。
- 配置可保存，也可通过二维码分享。
- 高级选项包括 DNS 服务器、路由 CIDR、MTU、路由 metric、上游代理、
  仅隧道模式，以及额外的 `gonc` 参数。

在 Windows 上，组网客户端模式可能会请求管理员权限，以便配置路由和 DNS
保护。

## 隐私与安全

- Gonc 不需要用户账号。
- 文件数据只会在 TLS 1.3 安全连接建立后传输。即使使用 SOCKS5 服务器作为
  中继路径，它也只能看到 TLS 加密后的流量，无法读取或篡改文件内容。
- 共享口令就是连接密钥。任何拥有口令的人都可以连接并接收你共享的文件，
  因此请每次使用随机生成的高强度口令，不要复用已经分享过的口令。
- Gonc 从口令派生 TLS 证书并要求双向认证，因此不需要 CA 证书。
- Gonc 使用公共第三方 STUN 服务器发现 NAT 地址，并使用公共 MQTT 服务器
  进行信令。双方会使用从口令派生的哈希在 MQTT 上相遇，网络地址则使用从
  口令派生的数据通过 AES-GCM 加密后交换。MQTT 服务器无法看到口令，也
  无法解密交换的地址。
- 文件修复使用 BLAKE3 分块哈希，避免信任过期的本地数据。

用于 STUN 和信令的公共服务器：

```text
STUN:
tcp://turn.cloudflare.com:80
udp://turn.cloudflare.com:53
udp://stun.l.google.com:19302
stun.gonc.cc:3478
global.turn.twilio.com:3478
stun.nextcloud.com:443

MQTT:
tcp://broker.hivemq.com:1883
tcp://broker.emqx.io:1883
tcp://test.mosquitto.org:1883
tcp://mqtt.gonc.cc:1883
```

## 故障排查

### Windows 应用无法打开

Gonc 通过 Wails 使用 Microsoft Edge WebView2。如果应用无法打开，请安装或
修复
[Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。

### Android 组网连接在后台停止

Android 可能会限制长时间运行的后台任务。请允许 Gonc 忽略电池优化，或将
应用电池模式设置为不受限制。

### 无法连接传输

- 确认双方使用完全相同的口令。
- 如果双方都位于类型 4 / 对称型 NAT 后面，请配置你自己的、支持
  UDP ASSOCIATE 的 SOCKS5 代理服务器，让 Gonc 将其作为中继路径。

## 开发

桌面端应用使用 Wails 构建，并嵌入 `gonetcat` Go 引擎。Android 的
`mobilegonc.aar` 由本仓库的 `android/mobilegonc` 和 `android/nearbygo`
通过 gomobile 生成，使用 `go.mod` 锁定的 gonc 版本。

Android 绑定或其 Go 依赖变动后，在仓库根目录重新生成 AAR：

```text
android\update-mobilegonc-aar.bat
```

### 在 Windows 上构建

前置条件（与 GitHub 发布工作流使用的工具一致）：

- Go，版本见 `go.mod`
- Node.js 及 npm（发布工作流使用 Node.js 24）
- Wails CLI v2.12.0
- Microsoft Edge WebView2 Runtime（运行应用时需要）

安装 Wails CLI，并确保 `GOBIN`（默认为 `GOPATH\bin`）已加入 `PATH`：

```text
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
```

在项目根目录执行构建：

```text
.\build.bat
```

该脚本会设置 `GOPROXY=https://goproxy.cn,direct`，安装前端依赖，执行
`wails build -platform windows/amd64`，并复制所需的 Wintun DLL。传入
`arm64` 参数可构建 Windows arm64 版本。输出文件：

```text
build\bin\gonc-gui.exe
build\bin\wintun.dll
```

分发时请将 `wintun.dll` 与 `gonc-gui.exe` 放在同一目录。

直接执行 `go build` 与上述构建并不等价：它不会执行 Wails 的前端构建，也不会
生成 Windows 资源。`wails build` 过程中，Wails 会临时生成包含应用图标和
Windows 清单的 `gonc-gui-res.syso`，链接后再将其删除，因此仓库中找不到
`.syso` 文件属于正常现象，也不需要另外安装 `rsrc` 或 `windres`。直接
`go build`（还需要 `desktop,production` 构建标签和已构建好的
`frontend\dist`）生成的 exe 没有图标和清单，可用于诊断，但不能作为发布构建。

创建发布包（在 `dist\` 中生成 Windows amd64、arm64 压缩包及 Android APK）：

```text
release.bat
```

## 项目结构

```text
gonc-gui/
  app.go                    Wails 后端方法，暴露给前端
  frontend/                 桌面端 React UI
  internal/goncrunner/      内嵌的 gonc 会话运行器
  internal/httpdownload/    桌面端 HTTP 接收下载器
  internal/nearby/          附近设备发现、配对和传输协议
  internal/nearbyapp/       桌面与 Android 共用的附近设备服务
  internal/tunengine/       桌面与 Android VPN 共用的 tun2socks 引擎
  android/                  Android 应用
  android/mobilegonc/       口令传输和 VPN 的 Go 绑定
  android/nearbygo/         Android 附近设备功能的 Go 绑定
  android/internal/mobilefilesource/
                            共用的 Android 文件源适配器
  android/update-mobilegonc-aar.bat
                            重建 Android mobilegonc.aar
```

## 许可证

参见 [LICENSE](LICENSE)。
