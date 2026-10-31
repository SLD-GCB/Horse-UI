<template>
  <div class="page">
    <el-row :gutter="12">
      <el-col :span="14">
        <el-card shadow="never">
          <template #header>面板与内核配置</template>
          <el-form :model="settings" label-width="140px">
            <el-form-item label="面板监听地址">
              <el-input v-model="settings.panel_listen" placeholder="留空使用启动参数（如 0.0.0.0:8080）" />
              <div class="hint">
                修改后需重启面板生效。想让域名直接走 443，填 <span class="mono">0.0.0.0:443</span>（需先启用 HTTPS）。
                若填错导致无法监听，会自动回退到启动参数，不会把面板锁死。
              </div>
            </el-form-item>
            <el-form-item label="面板域名">
              <el-input v-model="settings.panel_domain" placeholder="如 panel.example.com（可带端口）" />
              <div class="hint">仅用于展示面板访问地址，不影响面板实际监听（监听地址在启动参数里）。</div>
            </el-form-item>
            <el-form-item label="订阅对外地址">
              <el-input v-model="settings.sub_host" placeholder="https://sub.example.com 或 1.2.3.4" />
              <div class="hint">分享链接中使用的服务器地址；留空则回退到当前访问域名。</div>
            </el-form-item>
            <el-form-item label="订阅路径前缀">
              <el-input v-model="settings.sub_path" placeholder="/sub" />
            </el-form-item>
            <el-form-item label="端口池区间">
              <el-input v-model="settings.port_pool_start" style="width: 120px" placeholder="10000" />
              <span style="margin: 0 8px">—</span>
              <el-input v-model="settings.port_pool_end" style="width: 120px" placeholder="60000" />
              <div class="hint">
                新建入站时从该区间自动分配可用端口；建议避开 1-1024 与常用服务端口。
              </div>
            </el-form-item>
            <el-form-item label="Clash API 监听">
              <el-input v-model="settings.clash_listen" placeholder="127.0.0.1:9090" />
            </el-form-item>
            <el-form-item label="Clash API 密钥">
              <el-input v-model="settings.clash_secret" />
            </el-form-item>
            <el-form-item label="统计方式">
              <el-select v-model="settings.stats_mode" style="width: 300px">
                <el-option label="自动（推荐，探测内核能力）" value="auto" />
                <el-option label="V2Ray API（按用户，需自编译内核）" value="v2ray" />
                <el-option label="Clash API（按入站，官方内核可用）" value="clash" />
                <el-option label="关闭统计" value="off" />
              </el-select>
              <div class="hint">
                官方 sing-box 发行版不含 V2Ray API，无法按用户统计；按用户限额需使用带
                with_v2ray_api 参数编译的内核，可在下方填写其下载地址。
              </div>
            </el-form-item>
            <el-form-item label="自定义内核下载地址">
              <el-input v-model="settings.box_download_url" placeholder="留空使用官方 GitHub 地址" />
              <div class="hint">支持占位符 {version} {os} {arch} {ext}。</div>
            </el-form-item>
            <el-form-item label="统计 gRPC 监听">
              <el-input v-model="settings.v2ray_listen" placeholder="127.0.0.1:9091（仅 V2Ray 方式使用）" />
            </el-form-item>
            <el-form-item label="日志级别">
              <el-select v-model="settings.log_level" style="width: 200px">
                <el-option v-for="l in ['trace', 'debug', 'info', 'warn', 'error']" :key="l" :label="l" :value="l" />
              </el-select>
            </el-form-item>
            <el-form-item label="节点测速地址">
              <el-input v-model="settings.test_url" placeholder="http://www.gstatic.com/generate_204" />
              <div class="hint">
                入站 / 出站测速共用；建议用返回 204 的地址。预设：
                <el-link type="primary" @click="settings.test_url = 'http://www.gstatic.com/generate_204'">gstatic</el-link>
                <el-link type="primary" style="margin-left: 8px" @click="settings.test_url = 'https://cp.cloudflare.com/generate_204'">cloudflare</el-link>
                <el-link type="primary" style="margin-left: 8px" @click="settings.test_url = 'http://www.baidu.com'">baidu</el-link>
                <el-link type="primary" style="margin-left: 8px" @click="settings.test_url = 'http://connectivitycheck.platform.hicloud.com/generate_204'">hicloud</el-link>
              </div>
            </el-form-item>
            <el-form-item label="ACME 邮箱">
              <el-input v-model="settings.acme_email" placeholder="用于 Let's Encrypt 证书申请" />
            </el-form-item>
            <el-form-item label="DNS 配置 (JSON)">
              <el-input v-model="settings.dns" type="textarea" :rows="4" placeholder='留空使用默认，例如 {"servers":[{"address":"8.8.8.8"}]}' />
            </el-form-item>
            <el-form-item label="路由配置 (JSON)">
              <el-input
                v-model="settings.route"
                type="textarea"
                :rows="5"
                placeholder='留空使用默认，例如 {"rules":[],"final":"direct"}'
              />
            </el-form-item>

            <el-divider content-position="left">广告过滤</el-divider>
            <el-form-item label="启用广告过滤">
              <el-switch v-model="settings.adblock_enable" active-value="1" inactive-value="0" />
              <div class="hint">
                命中列表的域名会被内核直接拒绝（不经过任何出站）。使用本地面域名列表，无需联网下载。
              </div>
            </el-form-item>
            <el-form-item label="广告域名列表">
              <el-input
                v-model="settings.adblock_domains"
                type="textarea"
                :rows="6"
                placeholder="每行一个域名后缀，如 doubleclick.net"
              />
              <div class="ab-row">
                <el-button size="small" @click="fillAdblockPreset">填入常用列表（域名 + 关键词）</el-button>
                <el-button size="small" @click="settings.adblock_domains = ''">清空</el-button>
                <span class="hint">当前 {{ adblockCount }} 条（# 开头为注释）</span>
              </div>
            </el-form-item>
            <el-form-item label="域名关键词">
              <el-input
                v-model="settings.adblock_keywords"
                type="textarea"
                :rows="3"
                placeholder="逗号或换行分隔，如 adserver, adclick"
              />
              <div class="hint">域名里出现该词就拦截（例如 xxx-adserver.com），可用于捕获列表里没有的广告域名。</div>
            </el-form-item>
            <el-form-item label="域名正则">
              <el-input
                v-model="settings.adblock_regex"
                type="textarea"
                :rows="3"
                placeholder="每行一条，如 ^ad[0-9]+[.]example[.]com$"
              />
              <div class="hint">每行一条正则；建议用 <span class="mono">[.]</span> 代替 <span class="mono">\.</span> 表示点号。</div>
            </el-form-item>
            <el-form-item label="IP / CIDR 黑名单">
              <el-input v-model="settings.adblock_ips" type="textarea" :rows="3" placeholder="每行一个，如 203.0.113.0/24" />
              <div class="hint">按目标 IP 拦截，用于没有明显域名的广告服务器。</div>
            </el-form-item>
            <el-form-item label="远程规则集 URL">
              <el-input v-model="settings.adblock_url" placeholder="可选：.srs 规则集地址；留空则只用上面的本地列表" />
              <div class="hint warn">
                远程规则集在启动时下载，<b>地址不可达会导致内核启动失败</b>；国内网络建议留空，只用本地列表。
              </div>
            </el-form-item>

            <el-divider content-position="left">安全设置</el-divider>
            <el-form-item label="面板 HTTPS">
              <el-select v-model="settings.panel_tls_enable" style="width: 200px">
                <el-option label="关闭（HTTP）" value="0" />
                <el-option label="启用（HTTPS）" value="1" />
              </el-select>
              <div class="hint">
                默认开启：首次启动会自动生成一张自签证书并在下方选中，浏览器首次访问会提示证书不受信任（自签证书的正常现象）。
                有域名时可在「证书管理」申请 ACME 证书后替换。改动需重启面板生效，证书续签会自动热加载。
              </div>
            </el-form-item>
            <el-form-item label="面板证书">
              <el-select v-model="settings.panel_tls_cert_id" clearable style="width: 320px" placeholder="选择用于面板 HTTPS 的证书">
                <el-option
                  v-for="ct in certs"
                  :key="ct.id"
                  :label="`${ct.domain}（${ct.kind}）`"
                  :value="String(ct.id)"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="客户端证书指纹">
              <div class="fp-box">
                <span v-if="panelCert?.sha256" class="fp">{{ panelCert.sha256 }}</span>
                <span v-else class="hint" style="margin-top: 0">面板证书未加载</span>
                <el-button v-if="panelCert?.sha256" size="small" @click="copyFingerprint">复制</el-button>
                <el-button size="small" @click="loadPanelCert">刷新</el-button>
              </div>
              <div class="hint">
                把这段 SHA-256 填到 HorseBox →「设置 → 安全 → 面板证书指纹」。
                Windows 和 Android 填同一份，客户端只信任这张证书。
              </div>
              <div v-if="panelCert?.self_signed && panelCert?.not_after" class="hint">
                自签证书，有效期至 {{ panelCert.not_after.slice(0, 10) }}；续签后指纹会变化，需要重新复制。
              </div>
              <div v-if="panelCert?.warning" class="hint warn">{{ panelCert.warning }}</div>
            </el-form-item>
            <el-form-item label="明文跳转监听">
              <el-input v-model="settings.panel_http_listen" placeholder="如 0.0.0.0:80；留空则不启用" />
              <div class="hint">启用 HTTPS 后，该地址上的请求会 308 跳转到 HTTPS。</div>
            </el-form-item>
            <el-form-item label="会话有效期（小时）">
              <el-input v-model="settings.session_ttl_hours" style="width: 120px" placeholder="24" />
              <div class="hint">登录令牌有效时长；修改密码或登出会使旧令牌立即失效。</div>
            </el-form-item>
            <el-form-item label="登录失败锁定阈值">
              <el-input v-model="settings.login_max_fails" style="width: 120px" placeholder="5" />
              <div class="hint">同一账号连续失败达到该次数后锁定 15 分钟。</div>
            </el-form-item>
            <el-form-item label="请求体上限（字节）">
              <el-input v-model="settings.max_body_bytes" style="width: 160px" placeholder="2097152" />
            </el-form-item>
            <el-form-item label="CORS 白名单">
              <el-input v-model="settings.cors_origins" placeholder="如 http://localhost:5173；留空表示仅同源" />
            </el-form-item>
            <el-form-item label="订阅 UA 白名单">
              <el-input v-model="settings.sub_ua_whitelist" placeholder="如 v2rayN,clash；留空表示不限制" />
            </el-form-item>
            <el-form-item label="面板 IP 白名单">
              <el-input v-model="settings.panel_ip_whitelist" placeholder="如 1.2.3.4,10.0.0.0/8；留空表示不限制" />
              <div class="hint warn">
                谨慎填写：配置错误可能导致自己无法访问面板，请先确认新 IP 可用。
              </div>
            </el-form-item>

            <el-divider content-position="left">行为抹除（抗识别）</el-divider>
            <el-form-item label="启用行为抹除">
              <el-switch v-model="settings.behavior_mask" active-value="1" inactive-value="0" />
              <div class="hint">
                对<b>全部 TCP 系协议</b>（VLESS / VMess / Trojan / Shadowsocks）下发的客户端配置统一开启多路复用：
                把几十条短连接压成 1~2 条长连接，从而抹掉「连接数 / 每秒建连数 / 反复 TLS 握手」这三个最容易触发限速的指纹。
                <br />
                代价是所有流共用一条 TCP，丢包时会有队头阻塞 —— 浏览 / 社交推荐开，大文件多线程下载建议关。
                Hysteria2 / TUIC 本身就在一条 QUIC 连接上复用流，不受此项影响。
                <br />
                注意：只影响 Clash / sing-box 订阅配置；vless:// 这类分享链接没有标准写法承载多路复用。
              </div>
            </el-form-item>
            <el-form-item label="每连接并发流">
              <el-input v-model="settings.behavior_mask_streams" style="width: 120px" placeholder="8" />
              <div class="hint">
                默认 8（1-128）。调小更省连接、但队头阻塞更明显；调大并发更好、但连接数会回升。
              </div>
            </el-form-item>
            <el-form-item label="填充帧长">
              <el-switch v-model="settings.behavior_mask_padding" active-value="1" inactive-value="0" />
              <div class="hint">给多路复用帧补齐长度，抹掉包长分布指纹；会略微增加流量开销。</div>
            </el-form-item>
            <el-form-item label="UDP 特征抹除">
              <el-button :loading="maskingUdp" @click="enableUdpMask">为 Hysteria2 入站启用混淆</el-button>
              <div class="hint">
                UDP 侧 sing-box 只提供 Hysteria2 的 obfs（salamander / gecko）：它把 QUIC 报文整体打乱，
                DPI 既看不到 QUIC 结构、也拿不到包长分布。点按钮会给<b>所有未配置混淆的 Hysteria2 入站</b>
                自动生成混淆密码并立即应用。
                <br />
                TUIC 没有等价字段（sing-box 未提供），抹不掉 —— 线路对 UDP 特别敏感时建议直接改用 Hysteria2。
                <br />
                <b>注意：会改变服务端配置，所有 Hysteria2 客户端必须重新导入订阅，否则会连不上。</b>
              </div>
            </el-form-item>

            <el-form-item>
              <el-button type="primary" :loading="saving" @click="save">保存并应用</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <el-col :span="10">
        <el-card shadow="never">
          <template #header>修改面板密码</template>
          <el-form :model="pwd" label-width="100px">
            <el-form-item label="原密码">
              <el-input v-model="pwd.old" type="password" show-password />
            </el-form-item>
            <el-form-item label="新密码">
              <el-input v-model="pwd.next" type="password" show-password />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="changing" @click="changePwd">修改密码</el-button>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" style="margin-top: 12px">
          <template #header>
            系统调优（弱网优化）
            <el-tag v-if="tune" size="small" :type="tune.applied ? 'success' : 'info'" style="margin-left: 8px">
              {{ tune.applied ? '已应用' : '未应用' }}
            </el-tag>
          </template>

          <div v-if="tune && tune.supported" class="bbr-row">
            <div class="bbr-text">
              <div class="bbr-title">BBR 拥塞控制</div>
              <div class="hint">
                开启后 TCP 系协议（VLESS / VMess / Trojan / Shadowsocks）在丢包、高延迟线路上不再明显掉速；
                Hysteria2 / TUIC 走用户态拥塞控制，不受它影响。
                开启时同时设置默认队列规则为 fq，关闭时恢复为 cubic + fq_codel。
              </div>
            </div>
            <el-switch
              v-model="bbrOn"
              :loading="bbrBusy"
              :disabled="!tune.can_apply || !tune.bbr_available"
              active-text="已开启"
              inactive-text="已关闭"
              @change="toggleBBR"
            />
          </div>

          <div v-if="tuneLoading && !tune" class="hint">检测中 ...</div>
          <div v-else-if="tune && !tune.supported" class="hint">
            当前系统不支持内核参数调优（仅 Linux 支持）。
          </div>
          <template v-else-if="tune">
            <div class="tune-summary">
              内存 {{ tune.mem_mb }}MB · BBR {{ tune.bbr_available ? '可用' : '不可用' }} · 容器
              {{ tune.in_container ? '是' : '否' }}
            </div>
            <el-table :data="tune.items" size="small" border max-height="320">
              <el-table-column prop="key" label="参数" min-width="180" show-overflow-tooltip />
              <el-table-column prop="current" label="当前" width="100" show-overflow-tooltip />
              <el-table-column prop="suggest" label="建议" width="100" show-overflow-tooltip />
              <el-table-column label="状态" width="86">
                <template #default="{ row }">
                  <el-tag size="small" :type="row.ok ? 'success' : 'warning'">
                    {{ row.ok ? '已达标' : row.safe ? '待优化' : '仅建议' }}
                  </el-tag>
                </template>
              </el-table-column>
            </el-table>
            <div v-for="w in tune.warnings" :key="w" class="hint warn">! {{ w }}</div>
            <div class="tune-actions">
              <el-button type="primary" size="small" :loading="tuning" :disabled="!tune.can_apply" @click="applyTune">
                一键应用
              </el-button>
              <el-button size="small" :loading="tuning" :disabled="!tune.can_apply || !tune.applied" @click="revertTune">
                回滚调优
              </el-button>
              <el-button size="small" :loading="tuneLoading" @click="loadTune">重新检测</el-button>
            </div>
            <div class="hint">
              只改内核参数，写入 /etc/sysctl.d/99-horseui.conf，可随时回滚；UDP 缓冲对 Hysteria2 / TUIC
              最有用，BBR 只帮 TCP 系协议。缓冲类参数需重启内核进程后完全生效。
            </div>
          </template>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { Certificate, TuneReport } from '@/api/types'
import { useAuthStore } from '@/store/auth'

const auth = useAuthStore()
const saving = ref(false)
const changing = ref(false)
const certs = ref<Certificate[]>([])

interface PanelCertInfo {
  enabled: boolean
  loaded: boolean
  cert_id?: number
  sha256?: string
  subject?: string
  issuer?: string
  san?: string[]
  self_signed?: boolean
  not_before?: string
  not_after?: string
  warning?: string
}
const panelCert = ref<PanelCertInfo | null>(null)

// ---- 系统调优（弱网优化） ----
const tune = ref<TuneReport | null>(null)
const tuneLoading = ref(false)
const tuning = ref(false)
// ---- UDP 特征抹除 ----
const maskingUdp = ref(false)

// ---- 全局 BBR 滑块 ----
const bbrOn = ref(false)
const bbrBusy = ref(false)

/** 用体检结果同步滑块状态（当前拥塞控制算法是否为 bbr）。 */
function syncBBR() {
  const item = tune.value?.items.find((i) => i.key === 'net.ipv4.tcp_congestion_control')
  bbrOn.value = item?.current === 'bbr'
}

async function toggleBBR(value: string | number | boolean) {
  const enable = Boolean(value)
  bbrBusy.value = true
  try {
    const rep = await api.post<TuneReport>('/api/sys/bbr', { enabled: enable })
    tune.value = rep
    syncBBR()
    if (bbrOn.value !== enable) {
      ElMessage.warning('配置已写入，但当前内核未接受（常见于容器只读 /proc/sys），重启后生效')
    } else {
      ElMessage.success(enable ? 'BBR 已开启（fq + bbr）' : 'BBR 已关闭（已恢复 cubic + fq_codel）')
    }
  } catch {
    syncBBR() // 失败时把滑块拨回真实状态
  } finally {
    bbrBusy.value = false
  }
}

async function enableUdpMask() {
  try {
    await ElMessageBox.confirm(
      '这会为所有未配置混淆的 Hysteria2 入站生成新的混淆密码，并立即应用到内核。所有 Hysteria2 客户端必须重新导入订阅，否则会连不上。确认继续？',
      'UDP 特征抹除',
      { type: 'warning', confirmButtonText: '确认启用', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  maskingUdp.value = true
  try {
    const res = await api.post<{ changed: number; tuic: number; message: string; apply_error?: string }>(
      '/api/sys/mask-udp',
      {},
    )
    if (res.apply_error) {
      ElMessage.warning(`已完成，但内核应用失败：${res.apply_error}`)
    } else {
      ElMessage.success(res.message)
    }
  } finally {
    maskingUdp.value = false
  }
}

async function loadTune() {
  tuneLoading.value = true
  try {
    tune.value = await api.get<TuneReport>('/api/sys/tune')
    syncBBR()
  } catch {
    /* 拦截器已提示 */
  } finally {
    tuneLoading.value = false
  }
}

async function applyTune() {
  tuning.value = true
  try {
    tune.value = await api.post<TuneReport>('/api/sys/tune', { action: 'apply' })
    syncBBR()
    ElMessage.success('调优已应用；缓冲类参数需重启内核进程后完全生效')
  } catch {
    /* 拦截器已提示 */
  } finally {
    tuning.value = false
  }
}

async function revertTune() {
  tuning.value = true
  try {
    tune.value = await api.post<TuneReport>('/api/sys/tune', { action: 'revert' })
    syncBBR()
    ElMessage.success('已回滚到调优前的值')
    await loadTune()
  } catch {
    /* 拦截器已提示 */
  } finally {
    tuning.value = false
  }
}

const adblockCount = computed(
  () => settings.adblock_domains.split('\n').filter((s) => s.trim() && !s.trim().startsWith('#')).length,
)

async function fillAdblockPreset() {
  try {
    const res = await api.get<{ domains: string[]; keywords: string[] }>('/api/tools/adblock-preset')
    const merge = (cur: string, add: string[]) => {
      const list = cur
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
      return Array.from(new Set([...list, ...add])).join('\n')
    }
    settings.adblock_domains = merge(settings.adblock_domains, res.domains || [])
    settings.adblock_keywords = merge(settings.adblock_keywords, res.keywords || [])
    ElMessage.success(`已填入 ${(res.domains || []).length} 个域名 + ${(res.keywords || []).length} 个关键词`)
  } catch {
    /* 拦截器已提示 */
  }
}

const settings = reactive<Record<string, string>>({
  panel_listen: '',
  panel_domain: '',
  sub_host: '',
  sub_path: '/sub',
  port_pool_start: '10000',
  port_pool_end: '60000',
  clash_listen: '',
  clash_secret: '',
  stats_mode: 'auto',
  v2ray_listen: '',
  box_download_url: '',
  log_level: 'info',
  test_url: 'http://www.gstatic.com/generate_204',
  adblock_enable: '0',
  adblock_domains: '',
  adblock_keywords: '',
  adblock_regex: '',
  adblock_ips: '',
  adblock_url: '',
  acme_email: '',
  dns: '',
  route: '',
  panel_tls_enable: '1',
  panel_tls_cert_id: '',
  panel_http_listen: '',
  session_ttl_hours: '24',
  login_max_fails: '5',
  max_body_bytes: '2097152',
  cors_origins: '',
  sub_ua_whitelist: '',
  panel_ip_whitelist: '',
  behavior_mask: '0',
  behavior_mask_streams: '8',
  behavior_mask_padding: '1',
})

const pwd = reactive({ old: '', next: '' })

async function load() {
  const res = await api.get<Record<string, string>>('/api/settings')
  Object.keys(settings).forEach((k) => {
    if (res[k] !== undefined) settings[k] = res[k]
  })
}

async function loadCerts() {
  try {
    certs.value = await api.get<Certificate[]>('/api/certs')
  } catch {
    /* 忽略 */
  }
}

async function loadPanelCert() {
  try {
    panelCert.value = await api.get<PanelCertInfo>('/api/panelcert')
  } catch {
    panelCert.value = null
  }
}

async function copyFingerprint() {
  const text = panelCert.value?.sha256
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('证书指纹已复制')
  } catch {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

async function save() {
  saving.value = true
  try {
    const res = await api.put<{ apply_error?: string }>('/api/settings', { ...settings })
    if (res.apply_error) {
      ElMessage.warning(`已保存，但内核应用失败：${res.apply_error}`)
    } else {
      ElMessage.success('已保存并应用')
    }
  } finally {
    saving.value = false
  }
}

async function changePwd() {
  if (!pwd.old || !pwd.next) {
    ElMessage.warning('请填写原密码和新密码')
    return
  }
  changing.value = true
  try {
    await auth.changePassword(pwd.old, pwd.next)
    ElMessage.success('密码已修改')
    pwd.old = ''
    pwd.next = ''
  } finally {
    changing.value = false
  }
}

onMounted(() => {
  load()
  loadCerts()
  loadPanelCert()
  loadTune()
})
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}

.hint.warn {
  color: #e6a23c;
}

.fp-box {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.fp {
  font-family: Consolas, Menlo, monospace;
  font-size: 12px;
  line-height: 1.5;
  word-break: break-all;
  background: #f5f7fa;
  border-radius: 4px;
  padding: 6px 8px;
  max-width: 520px;
}

.ab-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}

.ab-row .hint {
  margin-top: 0;
}

.tune-summary {
  font-size: 12px;
  color: #606266;
  margin-bottom: 8px;
}

.tune-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 0 6px;
}

.bbr-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  margin-bottom: 10px;
  background: #f5f7fa;
  border-radius: 4px;
}

.bbr-text {
  flex: 1;
  min-width: 0;
}

.bbr-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}
</style>
