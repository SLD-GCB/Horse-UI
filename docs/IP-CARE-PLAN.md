# IP 养护集成方案

把 IP-Sentinel 的“送中检测 / 纠偏”能力并入 Horse-UI 与 HorseBox。

- 面板：执行器
- HorseBox：监视器 + 控制器
- 默认手动，自动由用户自己开
- 不引入 Telegram、独立 Master/Agent、OTA 集群

---

## 1. 目标

### 做什么

```
检测服务器出口 IP 在 Google 的归属区域
判断是否送中（CN）
支持手动/自动检测
支持手动/自动纠偏
HorseBox 显示服务器状态
HorseBox 可远程触发
历史记录
```

### 不做什么

```
不执行任意 Shell
不管理多台远程 Agent
不做 Telegram 控制
不默认自动纠偏
不强迫用户做选择
```

---

## 2. 用户体验

### 2.1 面板页面

侧边栏新增：

```
IP 养护
```

页面结构：

```
状态卡片
  出口 IP
  Google 区域
  目标区域
  是否送中
  信誉分
  最后检测
  最后纠偏

操作
  立即检测
  立即纠偏
  自动检测开关
  自动纠偏开关

历史
  时间 / 类型 / 区域 / 结果 / 耗时 / 错误

设置
  目标区域
  检测周期
  纠偏模式
  通知
```

状态：

```
正常（绿）
送中（黄）
检测中（蓝）
失败（红）
未知（灰）
```

### 2.2 HorseBox 页面

位置：

```
面板配置 / 工具页 → IP 养护
```

显示：

```
服务器 IP
Google 区域
是否送中
最后检测
最后纠偏
当前任务
```

按钮：

```
检测
纠偏
刷新
```

设置：

```
检测模式：手动 / 每天 / 每周
纠偏模式：手动 / 自动
送中通知
失败通知
```

默认：

```
手动
通知开
自动纠偏关
```

### 2.3 自动与手动

```
手动：用户点按钮才执行
自动检测：按用户设置的周期执行
自动纠偏：只有检测到送中且用户开启时才执行
```

不限制：

```
纠偏次数
失败重试次数
```

但必须记录：

```
每次时间
结果
错误
```

---

## 3. 总体架构

```
HorseBox
  ├─ 拉取状态
  ├─ 手动检测/纠偏
  ├─ 本地定时器
  └─ 通知
        │ HTTPS + JWT
        ▼
Horse-UI
  ├─ IP 养护 API
  ├─ 任务队列
  ├─ 调度器
  ├─ 检测器
  ├─ 纠偏器
  └─ SQLite 历史
        │ 本机 HTTP
        ▼
  Google / YouTube / 信誉接口
```

---

## 4. 面板后端

### 4.1 目录

```
back-machine/internal/ipcare/
  api.go
  service.go
  checker.go
  warmer.go
  scheduler.go
  store.go
  types.go
```

### 4.2 数据表

```sql
CREATE TABLE ip_care_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  target_region TEXT NOT NULL DEFAULT 'US',
  detect_mode TEXT NOT NULL DEFAULT 'manual',
  detect_interval_seconds INTEGER NOT NULL DEFAULT 86400,
  warm_mode TEXT NOT NULL DEFAULT 'manual',
  notify_cn INTEGER NOT NULL DEFAULT 1,
  notify_fail INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);

CREATE TABLE ip_care_checks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ip TEXT NOT NULL,
  google_region TEXT NOT NULL DEFAULT '',
  target_region TEXT NOT NULL DEFAULT '',
  is_cn INTEGER NOT NULL DEFAULT 0,
  trust_score INTEGER NOT NULL DEFAULT -1,
  latency_ms INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE ip_care_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  finished_at INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_ip_care_checks_created ON ip_care_checks(created_at DESC);
```

### 4.3 接口

```
GET  /api/ip-care/status
GET  /api/ip-care/history?limit=50
POST /api/ip-care/check
POST /api/ip-care/warm
GET  /api/ip-care/settings
PUT  /api/ip-care/settings
```

`GET /status`：

```json
{
  "ip": "103.247.28.18",
  "google_region": "US",
  "target_region": "US",
  "is_cn": false,
  "trust_score": 82,
  "last_check": 1794000000,
  "last_warm": 1794000000,
  "busy": false,
  "last_error": ""
}
```

`POST /check`：

```
同步执行，最长 20 秒
返回本次结果
同一时间只允许一个任务
```

`POST /warm`：

```
异步执行
立即返回 job_id
客户端轮询 status
```

### 4.4 鉴权

```
复用现有 JWT
只允许管理员
频率限制：每分钟 6 次
任务去重：同一类型正在跑时返回 busy
```

---

## 5. 检测器

### 5.1 主检测

```
GET https://www.youtube.com/premium
GET https://music.youtube.com/
UA：干净 Windows Chrome
超时：12 秒
```

从 HTML 提取：

```
contentRegion
countryCode
INNERTUBE_CONTEXT_GL
GL
```

判断：

```
CN 或跳转 google.cn → 送中
目标区域一致 → 正常
无结果 → 未知
```

### 5.2 辅助检测

```
ipinfo.io/country
ipapi.co/country
```

只做辅助，不替代 Google。

### 5.3 信誉

Phase 1 可选：

```
Scamalytics
AbuseIPDB
```

没有 API key 时跳过，显示“未检测”。

---

## 6. 纠偏器

### 6.1 行为

```
取目标区域关键词
随机 UA
访问 Google 搜索 / News / Maps
带 Referer
3–5 次请求
随机间隔
```

### 6.2 数据

```
data/ipcare/keywords/<region>.txt
data/ipcare/user_agents.txt
```

可从 IP-Sentinel 复制，保留来源和许可证。

### 6.3 约束

```
只访问 Google 域名
不访问用户输入 URL
不并发轰炸
单次任务最多 5 个请求
```

---

## 7. 调度器

```
面板启动时加载 settings
detect_mode=daily/weekly 时启动 timer
到点创建 check job
warm_mode=auto 且检测为 CN 时创建 warm job
```

防重复：

```
同一时间只跑一个 check
同一时间只跑一个 warm
```

HorseBox 的本地定时器只负责“提醒/触发”，  
最终以面板任务状态为准。

---

## 8. HorseBox 端

### 8.1 文件

```
UI/lib/ipcare/
  api.dart
  model.dart
  page.dart
  scheduler.dart
```

### 8.2 页面

```
状态卡片
检测按钮
纠偏按钮
历史列表
设置
```

### 8.3 定时

```
Android：WorkManager
Windows：Timer
```

### 8.4 通知

```
送中
检测失败
纠偏完成
```

### 8.5 离线

```
面板不可达：
  显示上次结果
  标记“服务器不可达”
  不自动纠偏
```

---

## 9. 安全

```
只允许管理员
JWT 鉴权
固定目标域名
不执行任意命令
任务队列限并发
日志脱敏
```

---

## 10. 风险

```
Google 可能触发验证码
频繁纠偏可能加重 IP 风险
ToS 风险
关键词/UA 数据会过期
误报
```

处理：

```
默认手动
显示风险提示
记录每次结果
失败不隐藏
```

---

## 11. 实施阶段

### Phase 1

```
面板后端：settings/check/history
面板页面：状态 + 手动检测
```

验收：

```
打开页面能看到出口 IP 和 Google 区域
点检测 20 秒内返回
送中显示黄色
```

### Phase 2

```
纠偏器
手动纠偏
历史记录
```

验收：

```
点纠偏能执行
历史有记录
失败有错误
```

### Phase 3

```
HorseBox 状态页
手动检测/纠偏
通知
```

验收：

```
App 能看到面板状态
能触发检测
能收到通知
```

### Phase 4

```
自动检测
自动纠偏
设置同步
```

验收：

```
用户开启后才自动
关闭后不执行
不限制次数
```

---

## 12. 结论

```
面板执行
App 监视和遥控
默认手动
自动由用户决定
不设次数上限
只记录和提示
```
