<template>
  <div>
    <el-alert
      v-if="status && !status.supported"
      type="warning"
      :closable="false"
      show-icon
      title="当前环境无法使用 nftables"
      description="需要 Linux + nftables + NET_ADMIN 权限。Windows、容器缺少 NET_ADMIN、或未安装 nftables 时只显示配置，不会修改防火墙。"
      style="margin-bottom: 12px"
    />
    <el-alert
      v-if="status?.error"
      type="error"
      :closable="false"
      show-icon
      :title="status.error"
      style="margin-bottom: 12px"
    />

    <el-card shadow="never">
      <template #header>
        <div class="head">
          <div>
            <b>玄武防火墙</b>
            <span class="sub">存量服务放行，新端口拒绝，受攻击端口关闭</span>
          </div>
          <div class="actions">
            <el-switch
              v-model="status.enabled"
              :disabled="!status.supported"
              active-text="启用"
              @change="toggleEnabled"
            />
            <el-switch
              v-model="status.auto_protect"
              :disabled="!status.supported || !status.enabled"
              active-text="自动关端口"
              @change="toggleAutoProtect"
            />
            <el-button size="small" @click="load">刷新</el-button>
            <el-button size="small" type="primary" :disabled="!status.supported" @click="discover">
              导入存量端口
            </el-button>
            <el-button size="small" :disabled="!status.supported" @click="apply">应用</el-button>
            <el-button size="small" type="danger" plain @click="rollback">回滚</el-button>
          </div>
        </div>
      </template>

      <el-row :gutter="12" class="stats">
        <el-col :span="6">
          <div class="stat"><span>状态</span><b>{{ status.applied ? '已应用' : '未应用' }}</b></div>
        </el-col>
        <el-col :span="6">
          <div class="stat"><span>默认策略</span><b>存量放行 / 新端口拒绝</b></div>
        </el-col>
        <el-col :span="6">
          <div class="stat"><span>端口规则</span><b>{{ status.ports.length }}</b></div>
        </el-col>
        <el-col :span="6">
          <div class="stat"><span>黑名单</span><b>{{ status.bans.length }}</b></div>
        </el-col>
      </el-row>
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="head">
          <b>动态容量</b>
          <div class="actions">
            <el-tag v-if="guard.stale" size="small" type="info">fwd 未运行 / 快照过期</el-tag>
            <el-tag v-else-if="guard.healthy" size="small" type="success">资源健康</el-tag>
            <el-tag v-else size="small" type="warning">资源紧张</el-tag>
            <el-button size="small" @click="loadGuard">刷新</el-button>
          </div>
        </div>
      </template>
      <el-table :data="guard.ports" size="small" empty-text="暂无容量数据：防火墙未启用、自动保护未开启，或 fwd 未运行">
        <el-table-column prop="port" label="端口" width="90" />
        <el-table-column label="协议" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.protocol === 'udp' ? 'warning' : 'info'">
              {{ (row.protocol || 'tcp').toUpperCase() }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="current" label="当前" width="80" />
        <el-table-column prop="baseline" label="基线" width="90">
          <template #default="{ row }">{{ row.baseline.toFixed(1) }}</template>
        </el-table-column>
        <el-table-column label="当前 / 动态上限" min-width="220">
          <template #default="{ row }">
            <el-progress
              :percentage="Math.min(100, Math.round((row.current / Math.max(1, row.limit)) * 100))"
              :status="row.status === 'closed' ? 'exception' : row.status === 'watch' ? 'warning' : 'success'"
              :stroke-width="10"
            />
            <span class="cap">{{ row.current }} / {{ row.limit.toFixed(1) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="下限 / 上限" width="140">
          <template #default="{ row }">{{ row.floor.toFixed(0) }} / {{ row.ceiling.toFixed(0) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="statusType(row)"
            >
              {{ statusText(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="关闭时间" width="170">
          <template #default="{ row }">
            {{ row.status === 'closed' ? fmtTime(row.closed_at) : '—' }}
          </template>
        </el-table-column>
        <el-table-column label="自动恢复" width="130">
          <template #default="{ row }">
            <span :class="row.status === 'closed' && !row.locked ? 'recover' : 'cap'">
              {{ recoverText(row) }}
            </span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="head">
          <b>端口</b>
          <el-button size="small" type="primary" @click="portDialog = true">添加规则</el-button>
        </div>
      </template>
      <el-table :data="status.ports" size="small" empty-text="还没有端口规则">
        <el-table-column prop="port" label="端口" width="90" />
        <el-table-column prop="protocol" label="协议" width="80" />
        <el-table-column label="动作" width="100">
          <template #default="{ row }">
            <el-tag :type="row.action === 'allow' ? 'success' : 'danger'" size="small">
              {{ row.action === 'allow' ? '允许' : '关闭' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="comment" label="备注" min-width="180" />
        <el-table-column label="来源" width="100">
          <template #default="{ row }">{{ row.auto ? '自动导入' : row.source }}</template>
        </el-table-column>
        <el-table-column label="自动保护" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.protect" size="small" type="success">不自动关</el-tag>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="380">
          <template #default="{ row }">
            <el-button
              v-if="row.action !== 'allow'"
              size="small"
              type="success"
              plain
              @click="setAction(row, 'allow')"
            >
              开放
            </el-button>
            <el-button
              v-if="row.action !== 'close'"
              size="small"
              type="danger"
              plain
              @click="setAction(row, 'close')"
            >
              临时关闭
            </el-button>
            <el-button
              v-if="row.action === 'allow' || !row.protect"
              size="small"
              type="danger"
              plain
              @click="permanentClose(row)"
            >
              永久关闭
            </el-button>
            <el-button size="small" @click="toggleProtect(row)">
              {{ row.protect ? '解除锁定' : row.action === 'close' ? '锁定关闭' : '锁定开放' }}
            </el-button>
            <el-button size="small" @click="removePort(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="head">
          <b>黑名单</b>
          <el-button size="small" type="danger" @click="banDialog = true">封禁 IP</el-button>
        </div>
      </template>
      <el-table :data="status.bans" size="small" empty-text="没有封禁">
        <el-table-column prop="ip" label="IP" min-width="160" />
        <el-table-column prop="reason" label="原因" min-width="180" />
        <el-table-column prop="source" label="来源" width="100" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="row.permanent ? 'danger' : 'warning'" size="small">
              {{ row.permanent ? '永久' : '临时' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button size="small" type="success" plain @click="unban(row)">解封</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="head">
          <div>
            <b>异常 IP（待确认）</b>
            <span class="sub">动态容量判定的来源，确认后可永久封禁</span>
          </div>
          <el-button size="small" :loading="sourceLoading" @click="loadSources">刷新</el-button>
        </div>
      </template>
      <el-table :data="attackSources" size="small" empty-text="暂无异常 IP">
        <el-table-column prop="ip" label="IP" min-width="150" />
        <el-table-column label="端口" width="110">
          <template #default="{ row }">{{ row.protocol }}/{{ row.port }}</template>
        </el-table-column>
        <el-table-column prop="count" label="连接数" width="90" />
        <el-table-column label="最近" width="160">
          <template #default="{ row }">{{ fmtTime(row.last_seen) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.banned ? 'danger' : 'warning'" size="small">
              {{ row.banned ? '已封' : '待确认' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110">
          <template #default="{ row }">
            <el-button
              v-if="!row.banned"
              size="small"
              type="danger"
              plain
              @click="banSource(row)"
            >永久封禁</el-button>
            <el-button v-else size="small" type="success" plain @click="unbanSource(row)">
              解封
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="head">
          <b>运行日志</b>
          <div class="actions">
            <el-button size="small" :loading="logLoading" @click="loadLogs">刷新</el-button>
          </div>
        </div>
      </template>
      <pre class="log">{{ logLines.length ? logLines.join('\n') : '暂无日志' }}</pre>
    </el-card>

    <el-dialog v-model="portDialog" title="添加端口规则" width="420px">
      <el-form label-width="80px">
        <el-form-item label="端口">
          <el-input-number v-model="portForm.port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="协议">
          <el-select v-model="portForm.protocol">
            <el-option label="TCP" value="tcp" />
            <el-option label="UDP" value="udp" />
          </el-select>
        </el-form-item>
        <el-form-item label="动作">
          <el-select v-model="portForm.action">
            <el-option label="允许" value="allow" />
            <el-option label="关闭" value="close" />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="portForm.comment" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="portDialog = false">取消</el-button>
        <el-button type="primary" @click="savePort">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="banDialog" title="封禁 IP" width="420px">
      <el-form label-width="80px">
        <el-form-item label="IP">
          <el-input v-model="banForm.ip" placeholder="1.2.3.4 或 IPv6" />
        </el-form-item>
        <el-form-item label="原因">
          <el-input v-model="banForm.reason" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="banForm.permanent">
            <el-radio :value="true">永久</el-radio>
            <el-radio :value="false">临时</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="banDialog = false">取消</el-button>
        <el-button type="danger" @click="saveBan">封禁</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { FirewallAttackSource, FirewallBan, FirewallPort, FirewallStatus, GuardSnapshot, PortGuardState } from '@/api/types'

const status = ref<FirewallStatus>({
  supported: false,
  applied: false,
  enabled: false,
  auto_import: true,
  auto_protect: false,
  default_policy: '',
  error: '',
  ports: [],
  bans: [],
})

const portDialog = ref(false)
const banDialog = ref(false)
const logLines = ref<string[]>([])
const logLoading = ref(false)
const guard = ref<GuardSnapshot>({
  updated_at: 0,
  healthy: false,
  running: false,
  stale: true,
  ports: [],
})
let logTimer: ReturnType<typeof setInterval> | undefined
let clockTimer: ReturnType<typeof setInterval> | undefined
const nowTick = ref(Date.now())
const portForm = reactive({ port: 0, protocol: 'tcp', action: 'allow', comment: '' })
const banForm = reactive({ ip: '', reason: '', permanent: true })
const attackSources = ref<FirewallAttackSource[]>([])
const sourceLoading = ref(false)

async function load() {
  status.value = await api.get<FirewallStatus>('/api/firewall/status')
}

async function loadSources() {
  sourceLoading.value = true
  try {
    const res = await api.get<{ items: FirewallAttackSource[] }>(
      '/api/firewall/attack-sources',
      { limit: 100 },
    )
    attackSources.value = res.items || []
  } finally {
    sourceLoading.value = false
  }
}

function banSource(row: FirewallAttackSource) {
  banForm.ip = row.ip
  banForm.reason = `${row.protocol}/${row.port} 异常连接`
  banForm.permanent = true
  banDialog.value = true
}

async function unbanSource(row: FirewallAttackSource) {
  await api.del(`/api/firewall/bans/${encodeURIComponent(row.ip)}`)
  await Promise.all([load(), loadSources()])
}

function fmtTime(ts: number) {
  return ts ? new Date(ts * 1000).toLocaleString() : '—'
}

async function loadLogs() {
  logLoading.value = true
  try {
    const res = await api.get<{ lines: string[] }>('/api/firewall/logs', { lines: 200 })
    logLines.value = res.lines || []
  } finally {
    logLoading.value = false
  }
}

async function loadGuard() {
  guard.value = await api.get<GuardSnapshot>('/api/firewall/guard')
}

function statusText(row: PortGuardState) {
  if (row.status === 'closed') return '已关端口'
  if (row.locked) return '永久开启'
  if (row.status === 'watch') return '观察中'
  return row.current === 0 ? '已开启' : '正常'
}

function statusType(row: PortGuardState) {
  if (row.status === 'closed') return 'danger'
  if (row.locked) return 'success'
  if (row.status === 'watch') return 'warning'
  return 'success'
}

function recoverText(row: PortGuardState) {
  if (row.locked) return row.status === 'closed' ? '永久' : '常开'
  if (row.status !== 'closed') return '—'
  if (!row.recover_at) return '永久'
  const left = row.recover_at * 1000 - nowTick.value
  if (left <= 0) return '等待恢复'
  const seconds = Math.ceil(left / 1000)
  return `${Math.floor(seconds / 60)}分${String(seconds % 60).padStart(2, '0')}秒`
}

async function toggleEnabled() {
  try {
    await api.post('/api/firewall/enabled', { enabled: status.value.enabled })
    ElMessage.success(status.value.enabled ? '已启用' : '已停用')
    await load()
  } catch {
    await load()
  }
}

async function toggleAutoProtect() {
  try {
    await api.post('/api/firewall/auto-protect', { enabled: status.value.auto_protect })
    ElMessage.success(status.value.auto_protect ? '自动关端口已开启' : '自动关端口已关闭')
    await load()
  } catch {
    await load()
  }
}

async function discover() {  const res = await api.post<{ added: number }>('/api/firewall/discover')
  ElMessage.success(`已导入 ${res.added} 个存量端口`)
  await load()
}

async function apply() {
  await api.post('/api/firewall/apply')
  ElMessage.success('已应用')
  await load()
}

async function rollback() {
  await ElMessageBox.confirm('回滚会删除玄武 nftables 表，确定？', '回滚', { type: 'warning' })
  await api.post('/api/firewall/rollback')
  ElMessage.success('已回滚')
  await load()
}

async function setAction(row: FirewallPort, action: string) {
  await api.post('/api/firewall/ports', {
    port: row.port,
    protocol: row.protocol,
    action,
    comment: action === 'close' ? '手动关闭（临时）' : '手动开放',
  })
  if (action === 'close') {
    // 临时关闭：解除锁定，让自动策略可以恢复。
    await api.post('/api/firewall/ports/protect', {
      port: row.port,
      protocol: row.protocol,
      protect: false,
    })
  }
  await load()
}

async function permanentClose(row: FirewallPort) {
  await api.post('/api/firewall/ports', {
    port: row.port,
    protocol: row.protocol,
    action: 'close',
    comment: '手动关闭（永久）',
  })
  await api.post('/api/firewall/ports/protect', {
    port: row.port,
    protocol: row.protocol,
    protect: true,
  })
  await load()
}

async function toggleProtect(row: FirewallPort) {
  await api.post('/api/firewall/ports/protect', {
    port: row.port,
    protocol: row.protocol,
    protect: !row.protect,
  })
  await load()
}

async function removePort(row: FirewallPort) {
  await ElMessageBox.confirm(`删除 ${row.port}/${row.protocol} 规则？`, '删除', { type: 'warning' })
  await api.del(`/api/firewall/ports/${row.id}`)
  await load()
}

async function savePort() {
  await api.post('/api/firewall/ports', { ...portForm })
  portDialog.value = false
  portForm.port = 0
  portForm.comment = ''
  await load()
}

async function saveBan() {
  await api.post('/api/firewall/bans', { ...banForm })
  banDialog.value = false
  banForm.ip = ''
  banForm.reason = ''
  await Promise.all([load(), loadSources()])
}

async function unban(row: FirewallBan) {
  await api.del(`/api/firewall/bans/${encodeURIComponent(row.ip)}`)
  await Promise.all([load(), loadSources()])
}

onMounted(() => {
  load()
  loadLogs()
  loadGuard()
  loadSources()
  logTimer = setInterval(() => {
    loadLogs()
    loadGuard()
  }, 5000)
  clockTimer = setInterval(() => {
    nowTick.value = Date.now()
  }, 1000)
})
onUnmounted(() => {
  if (logTimer) clearInterval(logTimer)
  if (clockTimer) clearInterval(clockTimer)
})
</script>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.sub {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sub {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: 400;
}
.recover {
  color: var(--el-color-warning);
  font-variant-numeric: tabular-nums;
}
.stats {
  margin-top: 4px;
}
.stat {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.stat span {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.stat b {
  font-size: 15px;
}
.cap {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.log {
  margin: 0;
  max-height: 360px;
  overflow: auto;
  padding: 10px 12px;
  border-radius: 8px;
  background: #111827;
  color: #d1d5db;
  font: 12px/1.6 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
