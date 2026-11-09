<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2>IP 养护</h2>
        <p class="sub">检测服务器出口 IP 在 Google 的区域，判断是否“送中”。</p>
      </div>
      <el-button :loading="loading" @click="loadAll">刷新</el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="默认只手动检测。自动检测需要你手动保存周期后才会运行。"
      style="margin-bottom: 14px"
    />

    <el-row :gutter="12">
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">当前状态</div>
          <div class="stat-value">
            <el-tag :type="stateType" size="large">{{ stateText }}</el-tag>
          </div>
          <div class="stat-sub">{{ status?.last_error || '等待检测' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">出口 IP</div>
          <div class="stat-value mono">{{ status?.ip || '-' }}</div>
          <div class="stat-sub">服务器公网地址</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">Google 区域</div>
          <div class="stat-value">{{ status?.google_region || '未知' }}</div>
          <div class="stat-sub">目标 {{ status?.target_region || '-' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">最后检测</div>
          <div class="stat-value small">{{ fmtTime(status?.last_check) }}</div>
          <div class="stat-sub">信誉 {{ trustText }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="12" style="margin-top: 12px">
      <el-col :span="14">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>检测与纠偏</span>
              <div>
                <el-button :loading="warming || status?.busy" @click="runWarm">立即纠偏</el-button>
                <el-button type="primary" :loading="checking || status?.busy" @click="runCheck">
                  {{ status?.busy ? '执行中…' : '立即检测' }}
                </el-button>
              </div>
            </div>
          </template>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="出口 IP">{{ status?.ip || '-' }}</el-descriptions-item>
            <el-descriptions-item label="Google 区域">{{ status?.google_region || '未知' }}</el-descriptions-item>
            <el-descriptions-item label="目标区域">{{ status?.target_region || '-' }}</el-descriptions-item>
            <el-descriptions-item label="是否送中">
              <el-tag :type="status?.is_cn ? 'warning' : 'success'" size="small">
                {{ status?.last_check ? (status.is_cn ? '是' : '否') : '未检测' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="耗时">
              {{ status?.last ? `${status.last.latency_ms} ms` : '-' }}
            </el-descriptions-item>
            <el-descriptions-item label="最后检测">
              {{ fmtTime(status?.last_check) }}
            </el-descriptions-item>
            <el-descriptions-item label="最后纠偏">
              {{ fmtTime(status?.last_warm) }}
            </el-descriptions-item>
          </el-descriptions>

          <el-divider content-position="left">检测设置</el-divider>
          <el-form :model="form" label-width="120px" style="max-width: 520px">
            <el-form-item label="目标区域">
              <el-select v-model="form.target_region" style="width: 100%">
                <el-option v-for="r in regions" :key="r.value" :label="r.label" :value="r.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="检测模式">
              <el-select v-model="form.detect_mode" style="width: 100%">
                <el-option label="手动" value="manual" />
                <el-option label="每 20 分钟" value="20m" />
                <el-option label="每小时" value="hourly" />
                <el-option label="每 6 小时" value="6h" />
                <el-option label="每 12 小时" value="12h" />
                <el-option label="每天一次" value="daily" />
                <el-option label="每周一次" value="weekly" />
              </el-select>
            </el-form-item>
            <el-form-item label="纠偏模式">
              <el-select v-model="form.warm_mode" style="width: 100%">
                <el-option label="手动" value="manual" />
                <el-option label="检测到送中后自动纠偏" value="auto" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="saving" @click="saveSettings">保存设置</el-button>
              <span class="hint">自动检测和自动纠偏都只在保存并开启后运行。</span>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <el-col :span="10">
        <el-card shadow="never">
          <template #header><span>HorseBox 连接</span></template>
          <div class="tip">
            <p>在 App 的「IP 养护」里粘贴这个令牌，App 就能读取状态并触发检测/纠偏。</p>
          </div>
          <el-input :model-value="token" readonly style="margin: 10px 0">
            <template #append>
              <el-button @click="copyToken">复制</el-button>
            </template>
          </el-input>
          <el-button text type="danger" :loading="tokenLoading" @click="rotateToken">
            重新生成令牌
          </el-button>
          <p class="hint" style="margin-left: 0">重新生成后，旧令牌立即失效。</p>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="card-head">
          <span>检测历史</span>
          <el-button text @click="loadHistory">刷新</el-button>
        </div>
      </template>
      <el-table :data="history" size="small" style="width: 100%">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="ip" label="出口 IP" width="150" />
        <el-table-column prop="google_region" label="Google 区域" width="110" />
        <el-table-column prop="target_region" label="目标" width="80" />
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag :type="row.error ? 'danger' : row.is_cn ? 'warning' : 'success'" size="small">
              {{ row.error ? '失败' : row.is_cn ? '送中' : '正常' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="latency_ms" label="耗时" width="90">
          <template #default="{ row }">{{ row.latency_ms }} ms</template>
        </el-table-column>
        <el-table-column prop="error" label="错误" min-width="200" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!history.length" description="还没有检测记录" />
    </el-card>

    <el-card shadow="never" style="margin-top: 12px">
      <template #header>
        <div class="card-head">
          <span>纠偏历史</span>
          <el-button text @click="loadWarmHistory">刷新</el-button>
        </div>
      </template>
      <el-table :data="warmHistory" size="small" style="width: 100%">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'success' ? 'success' : row.status === 'running' ? 'info' : 'danger'" size="small">
              {{ row.status === 'success' ? '成功' : row.status === 'running' ? '执行中' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="requests" label="请求数" width="90" />
        <el-table-column prop="error" label="错误" min-width="240" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!warmHistory.length" description="还没有纠偏记录" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'

interface IPCareSettings {
  target_region: string
  detect_mode: string
  detect_interval_seconds: number
  warm_mode: string
  notify_cn: boolean
  notify_fail: boolean
}

interface IPCareCheck {
  id: number
  ip: string
  google_region: string
  target_region: string
  is_cn: boolean
  trust_score: number
  latency_ms: number
  error: string
  created_at: number
}

interface IPCareJob {
  id: number
  type: string
  status: string
  requests: number
  error: string
  created_at: number
  finished_at: number
}

interface IPCareStatus {
  ip: string
  google_region: string
  target_region: string
  is_cn: boolean
  trust_score: number
  last_check: number
  last_warm: number
  busy: boolean
  last_error: string
  settings: IPCareSettings
  last?: IPCareCheck
}

const regions = [
  { label: '美国 US', value: 'US' },
  { label: '香港 HK', value: 'HK' },
  { label: '日本 JP', value: 'JP' },
  { label: '新加坡 SG', value: 'SG' },
  { label: '台湾 TW', value: 'TW' },
  { label: '英国 GB', value: 'GB' },
  { label: '德国 DE', value: 'DE' },
  { label: '韩国 KR', value: 'KR' },
  { label: '加拿大 CA', value: 'CA' },
  { label: '澳大利亚 AU', value: 'AU' },
]

const status = ref<IPCareStatus | null>(null)
const history = ref<IPCareCheck[]>([])
const warmHistory = ref<IPCareJob[]>([])
const token = ref('')
const tokenLoading = ref(false)
const loading = ref(false)
const checking = ref(false)
const warming = ref(false)
const saving = ref(false)

const form = reactive({
  target_region: 'US',
  detect_mode: 'manual',
  warm_mode: 'manual',
})

const stateText = computed(() => {
  if (status.value?.busy) return '检测中'
  if (!status.value?.last_check) return '未检测'
  if (status.value.last_error) return '检测失败'
  return status.value.is_cn ? '送中' : '正常'
})

const stateType = computed(() => {
  if (status.value?.busy) return 'info'
  if (!status.value?.last_check) return 'info'
  if (status.value.last_error) return 'danger'
  return status.value.is_cn ? 'warning' : 'success'
})

const trustText = computed(() => {
  const v = status.value?.trust_score ?? -1
  return v >= 0 ? String(v) : '未检测'
})

function fmtTime(ts?: number) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function loadStatus() {
  status.value = await api.get<IPCareStatus>('/api/ip-care/status')
  form.target_region = status.value.settings.target_region || 'US'
  form.detect_mode = status.value.settings.detect_mode || 'manual'
  form.warm_mode = status.value.settings.warm_mode || 'manual'
}

async function loadHistory() {
  const res = await api.get<{ items: IPCareCheck[] }>('/api/ip-care/history?limit=50')
  history.value = res.items || []
}

async function loadWarmHistory() {
  const res = await api.get<{ items: IPCareJob[] }>('/api/ip-care/warm/history?limit=50')
  warmHistory.value = res.items || []
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadStatus(), loadHistory(), loadWarmHistory(), loadToken()])
  } finally {
    loading.value = false
  }
}

async function loadToken() {
  try {
    const res = await api.get<{ token: string }>('/api/ip-care/token')
    token.value = res.token || ''
  } catch {
    token.value = ''
  }
}

async function copyToken() {
  if (!token.value) return
  try {
    await navigator.clipboard.writeText(token.value)
    ElMessage.success('令牌已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择')
  }
}

async function rotateToken() {
  tokenLoading.value = true
  try {
    const res = await api.post<{ token: string }>('/api/ip-care/token/rotate')
    token.value = res.token || ''
    ElMessage.success('已重新生成')
  } finally {
    tokenLoading.value = false
  }
}

async function waitIdle(timeoutMs = 100000) {
  const started = Date.now()
  while (Date.now() - started < timeoutMs) {
    const st = await api.get<IPCareStatus>('/api/ip-care/status')
    status.value = st
    if (!st.busy) return
    await new Promise((resolve) => setTimeout(resolve, 1500))
  }
}

async function runCheck() {
  checking.value = true
  try {
    const res = await api.post<IPCareCheck>('/api/ip-care/check', undefined, 35000)
    if (res.error) {
      ElMessage.warning(res.error)
    } else {
      ElMessage.success('检测完成')
    }
    // 检测后可能还有自动纠偏在跑，等它结束再刷新，避免一直转圈。
    await waitIdle()
    await loadAll()
  } finally {
    checking.value = false
  }
}

async function runWarm() {
  warming.value = true
  try {
    const job = await api.post<IPCareJob>('/api/ip-care/warm', undefined, 95000)
    if (job.error) {
      ElMessage.warning(job.error)
    } else {
      ElMessage.success(`纠偏完成，成功请求 ${job.requests} 次`)
    }
    await loadAll()
  } finally {
    warming.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await api.put<IPCareSettings>('/api/ip-care/settings', {
      target_region: form.target_region,
      detect_mode: form.detect_mode,
      warm_mode: form.warm_mode,
      notify_cn: true,
      notify_fail: true,
    })
    ElMessage.success('设置已保存')
    await loadStatus()
  } finally {
    saving.value = false
  }
}

onMounted(loadAll)
</script>

<style scoped>
.page {
  padding: 18px;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 14px;
}

.page-head h2 {
  margin: 0;
  font-size: 20px;
}

.sub {
  margin: 4px 0 0;
  color: #909399;
  font-size: 13px;
}

.stat-label {
  color: #909399;
  font-size: 13px;
}

.stat-value {
  margin: 10px 0 6px;
  font-size: 24px;
  font-weight: 700;
  color: #303133;
}

.stat-value.small {
  font-size: 16px;
  line-height: 40px;
}

.stat-value.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 18px;
}

.stat-sub {
  color: #909399;
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}

.tip {
  color: #606266;
  font-size: 13px;
  line-height: 1.8;
}

.tip p {
  margin: 0 0 6px;
}
</style>
