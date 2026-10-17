# Horse-UI

一个自建的 **sing-box 单机代理面板**：在浏览器里管理协议入站、用户、出站、证书、订阅与内核。

面板与 sing-box 跑在同一台机器上，sing-box 作为受管子进程运行，前端资源内嵌在单一二进制里。

---

## 功能

**协议与传输**

- VLESS（含 Reality）、VMess、Trojan、Shadowsocks（含 2022）、TUIC、Hysteria2、HTTP
- 传输层支持 TCP / WebSocket / gRPC / HTTPUpgrade / HTTP/2
- 入站/出站可视化配置，端口池自动分配，节点测速，批量新建与批量操作

**用户与订阅**

- 用户 UUID / 密码自动生成，一个用户可绑定多个入站（多节点）
- 流量配额、到期时间、订阅令牌可独立重置
- 订阅分发：通用（base64）、Clash(Mihomo)、sing-box 三种格式，各带二维码

**出站与路由**

- 11 种出站类型（含 WireGuard、链式转发），可视化路由规则，广告过滤

**证书**

- ACME 自动签发（Cloudflare / 阿里云 / DNSPod / HTTP-01）与自签证书
- 自动续签、HTTPS 热加载；证书表为空时启动自动生成自签证书

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
- 单文件二进制，前端已内嵌，服务器无需安装 Go / Node

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

### 方式一：安装包

Linux 服务器解压后执行：

```bash
sudo bash service/install.sh
```

脚本会安装面板二进制、随包的 sing-box 内核与 systemd 服务，并做一次系统调优
（可用 `HORSEUI_TUNE=0` 跳过）。

默认监听 `0.0.0.0:8080`，面板 HTTPS 默认开启（自签证书），管理员账号 `admin`，
初始密码随机生成、仅在首次启动时打印一次：

```bash
journalctl -u horseui --no-pager | grep -i 密码
```

### 方式二：源码构建

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

Windows 下双击构建出的 `horseui.exe` 会进入终端管理菜单。

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

## 数据与安全

- 运行数据默认在 `-data` 指定的目录（Linux 安装包为 `/etc/horseui`）：
  `horseui.db`（SQLite）、`certs/`（证书私钥）、`config.json`（生成的 sing-box 配置）、`logs/`、`bin/`（内核）
- **不要把 `data/` 目录提交到仓库**，里面有管理员密码哈希、用户凭据与订阅令牌
- 首次启动生成随机管理员密码；检测到弱口令会提示并强制修改
- 面板改监听端口失败会自动回退，不会把面板锁死

## 开发

- Go ≥ 1.27，Node ≥ 18
- 前端构建产物目录由 `front-UI/vite.config.ts` 的 `build.outDir` 指向 `back-machine/internal/webui/dist`
- 新增 sing-box 配置字段时，先用 `sing-box check` 实测再写进代码（内核对未知字段严格报错）

## 许可

[AGPL-3.0](LICENSE)
