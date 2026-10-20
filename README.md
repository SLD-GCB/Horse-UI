# Horse-UI

一个自建的 **sing-box 单机代理面板**：在浏览器或 HorseBox 客户端里管理协议入站、用户、出站、证书、订阅与内核。

面板与 sing-box 跑在同一台机器上，sing-box 作为受管子进程运行，前端资源内嵌在单一二进制里，服务器不需要安装 Go / Node。

> **安装方法以发布包（Linux zip）里的 `README-install.txt` 为准**，那份文档和当次发布的二进制是配套的。
>
> 客户端请使用配套的 [HorseBox](https://github.com/SLD-GCB/HorseBox)：面板是自签 HTTPS，HorseBox 会按证书指纹校验，并在首次登录时提供临时加密连接。
>
> 为避免节点混乱，**一个用户只能绑定同一类节点**（同一个用户下面不会混装多种协议）。

## 功能

**协议与传输**

- VLESS（含 Reality）、VMess、Trojan、Shadowsocks（含 2022）、TUIC、Hysteria2、HTTP
- 传输层支持 TCP / WebSocket / gRPC / HTTPUpgrade / HTTP/2
- 入站/出站可视化配置，端口池自动分配，节点测速，批量新建与批量操作

**用户与订阅**

- 用户 UUID / 密码自动生成，一个用户可绑定多个入站（多节点）
- 流量配额、到期时间、订阅令牌可独立重置；超配额 / 过期 / 停用后订阅直接返回 403
- 订阅分发：通用（base64）、Clash(Mihomo)、sing-box 三种格式，各带二维码
- 订阅响应只下发配置，不附带流量头；客户端展示的额度由客户端本地记录（见「与 HorseBox 配合」）

**出站与路由**

- 11 种出站类型（含 WireGuard、链式转发），可视化路由规则，广告过滤

**证书**

- ACME 自动签发（Cloudflare / 阿里云 / DNSPod / HTTP-01）与自签证书
- 自动续签、HTTPS 热加载；证书表为空时启动自动生成自签证书
- 面板证书 SHA-256 指纹可直接复制，供客户端做固定校验

**内核**

- 一键安装 / 更新 / 启停，配置先校验再应用，日志查看

**安全**

- 面板 HTTPS 默认开启；除 Shadowsocks 外**所有协议强制 TLS 或 Reality**
- Bcrypt + JWT（改密即时吊销旧令牌）、登录限流与账号锁定
- CORS 白名单、面板 IP 白名单、审计日志、请求体上限

**弱网优化**

- 系统内核参数一键调优（UDP 缓冲 / BBR，可回滚）与全局 BBR 开关
- Hysteria2 拥塞控制三种模式引导、QUIC 调优字段、TCP Fast Open
- 行为抹除（全协议多路复用）、UDP 特征抹除、连接数诊断

**运维**

- 类 s-ui 的终端数字菜单：启动 / 停止 / 状态 / 查看改密 / 内核 / 日志 / 更新 / 卸载 / 系统调优
- 单文件二进制，前端已内嵌

## 协议支持

| 协议 | 加密 | 传输 | 备注 |
|---|---|---|---|
| VLESS | TLS / Reality | TCP / WS / gRPC / HTTPUpgrade / HTTP | 支持 `xtls-rprx-vision` |
| VMess | TLS | 同上 | 支持 `alterId` |
| Trojan | TLS / Reality | 同上 | |
| Shadowsocks | 协议自带 | TCP | 支持 2022 系列，无需 TLS |
| TUIC | QUIC + TLS | — | 可选 0-RTT、心跳、拥塞控制 |
| Hysteria2 | QUIC + TLS | — | 混淆（salamander / gecko）、带宽、Brutal |
| HTTP | TLS | — | HTTP 代理 |

## 快速开始

### 方式一：安装包（Linux）

解压后执行：

```bash
sudo bash service/install.sh
```

脚本会安装面板二进制、随包的 sing-box 内核与 systemd 服务，并做一次系统调优
（可用 `HORSEUI_TUNE=0` 跳过）。

默认监听 `0.0.0.0:8080`，面板 HTTPS 默认开启（自签证书），管理员账号 `admin`。
初始密码随机生成、只在首次启动时打印一次：

```bash
journalctl -u horseui --no-pager | grep -i 密码
```

如果检测到弱口令，启动时会**自动重置为随机密码**并打印新的密码（不会把面板锁死）。

### 方式二：Windows

双击 `horseui.exe` 进入终端管理菜单，选「启动」即可。首次启动同样会打印面板地址和管理员密码。

### 方式三：源码构建

```bash
# 1. 前端（产物写入 back-machine/internal/webui/dist，供 go:embed 使用）
cd front-UI
npm install
npm run build
cd ..

# 2. 后端
go build -trimpath -ldflags "-s -w" -o horseui ./back-machine

# 3. 运行（-run 跳过终端菜单，直接启动面板）
./horseui -run -data ./data
```

> 必须先构建前端再 `go build`，否则内嵌的是占位页。

## 首次登录

面板是自签 HTTPS，浏览器第一次打开会提示「不安全」。这时有两条路。

### 推荐：用 HorseBox 客户端登录（不经过浏览器）

1. 打开 HorseBox →「面板」→「首次登录」
2. 填 `https://服务器IP:端口`，点「临时连接」
3. 这条连接仍然是 TLS 加密的，只是第一次不校验证书；页面底部会有橙色横幅提示
4. 用终端打印的管理员密码登录
5. 建好入站和用户，复制订阅地址
6. 到面板「设置 → 客户端证书指纹」点「复制」
7. 回到 HorseBox「订阅」添加订阅：填订阅地址 + 指纹

从这以后，客户端每次 TLS 握手都会比对指纹，证书被替换会直接断开。

### 浏览器登录

直接打开 `https://IP:端口`，在证书警告页选择继续，然后用管理员密码登录。
浏览器不会做指纹固定，适合临时排障，不建议长期使用。

## 与 HorseBox 配合

HorseBox 是配套的双端客户端（Windows / Android），除代理之外还内置了下面这些能力，和面板是同一套证书与账号体系：

- **内嵌面板**：客户端里直接打开面板，走严格证书指纹校验；首次登录提供一次性临时加密连接
- **面板密码本**：在客户端里保存面板账号，登录页右上角一键填入；记录按面板去重，密码本地加密存储
- **SSH 终端**：「配置」页填 IP / 用户名 / 密码即可登录服务器，带 SFTP 文件传输和本地端口转发（`ssh -L`）
- **订阅用量条**：订阅卡片显示「已用 / 总额」。总额手填，已用是客户端本地累计（只算这台设备），面板侧仍然以数据库里的配额为准

面板这边不需要为客户端做任何额外配置：客户端只是消费者，账号、配额、到期、订阅令牌全部在面板里管理。

## 建节点与订阅

1. **入站**：新建入站，选择协议和端口。除 Shadowsocks 外，TLS/Reality 是强制项；自签证书可以直接选面板已有的那张。
2. **用户**：新建用户，UUID/密码自动生成；一个用户只绑定同一类节点，避免节点列表混装不同协议。
3. **订阅**：在用户详情里复制订阅地址，三种格式：

```
https://面板地址/sub/<令牌>                    # 通用（base64 链接列表）
https://面板地址/sub/<令牌>?target=clash       # Clash / Mihomo
https://面板地址/sub/<令牌>?target=singbox     # sing-box
```

订阅令牌可单独重置；禁用用户或配额/到期后会返回 403。

## 证书与指纹

- 证书表为空时，首次启动自动生成一张自签证书（SAN = 本机各网卡 IP + `localhost` + `127.0.0.1` + 主机名），有效期 10 年
- 「设置 → 客户端证书指纹」可以查看/复制 SHA-256，默认隐藏
- 续签或换证书后指纹会变，客户端需要重新复制
- 如果面板前面有反向代理终止 TLS，指纹页会提示：那里显示的是面板内部证书，客户端看到的证书以代理为准
- ACME 证书可以正常用于浏览器和公网域名；HorseBox 的指纹固定只对自签证书生效

## 安全建议

- 改掉默认端口，尽量不要长期把面板端口直接暴露在公网；需要时配合防火墙只放行自己的 IP
- 「设置 → 面板 IP 白名单」可以限制来源，默认关闭
- 管理员密码只存在终端输出里，登录后可以在面板里改；改密会让所有旧 token 立即失效
- 面板 HTTPS 证书加载失败时会回落明文并打印告警，保证不会把自己锁在门外；确认证书正常后尽快恢复
- 不要把 `data/` 目录提交到仓库或分享出去

## 数据与备份

运行数据默认在 `-data` 指定的目录（Linux 安装包为 `/etc/horseui`）：

- `horseui.db`：SQLite，用户、入站、订阅令牌、设置
- `certs/`：证书和私钥
- `config.json`：生成的 sing-box 配置
- `logs/`、`bin/`（内核）

备份只需要停面板后整目录复制；恢复时放回同一路径再启动。

## 目录结构

```
back-machine/          Go 后端
  internal/api/        HTTP 接口（入站 / 用户 / 出站 / 证书 / 订阅 / 统计 / 系统调优 …）
  internal/svc/        配置生成、订阅生成、内核管理、广告过滤
  internal/store/      SQLite 与配置项
  internal/panelmenu/  终端数字菜单
  internal/sysinfo/    端口池、网卡、系统调优
  internal/webui/      前端产物（go:embed）
  main.go              入口（-run / -menu / -tune）
core/                  sing-box 封装
  config/              sing-box 配置结构
  stats/               Clash API / V2Ray API 统计
  process/             内核进程管理
  cert/                证书工具
front-UI/              Vue 3 + TypeScript + Element Plus + Pinia + ECharts
service/               install.sh / systemd 单元 / 打包脚本
```

## 开发

- Go ≥ 1.27，Node ≥ 18
- 前端构建产物目录由 `front-UI/vite.config.ts` 的 `build.outDir` 指向 `back-machine/internal/webui/dist`
- 新增 sing-box 配置字段时，先用 `sing-box check` 实测再写进代码（内核对未知字段严格报错）
- 面板证书热加载的钩子在 `obtainCert` / `generateSelfSigned` 的 `db.Save` 之后，改证书流程时别绕开

## 常见问题

**启动后浏览器打不开面板**

看启动日志里的实际监听地址；端口被占用时面板会回退到启动参数里的地址，不会启动失败。

**忘了管理员密码**

终端菜单里有「重置密码」。

**客户端提示证书指纹不匹配**

面板换过证书。到「设置 → 客户端证书指纹」重新复制，更新到客户端订阅里。

**订阅返回 403 / 404**

403：用户被禁用、过期、超出配额，或 User-Agent 被白名单拦了。404：令牌不对或该用户没有可用入站。

**面板前面有 Nginx / Caddy**

可以让代理终止 TLS，但要在代理上把真实证书配好；面板指纹页会提示当前显示的不是客户端实际看到的证书。

## 许可

[AGPL-3.0](LICENSE)
