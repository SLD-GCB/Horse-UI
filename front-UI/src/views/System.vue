<template>
  <div class="page">
    <el-alert
      v-if="core?.apply_error"
      type="error"
      show-icon
      :closable="false"
      title="最近一次配置应用失败，内核仍在使用旧配置"
      :description="`${core.apply_error}　→　你新加/改动的入站与出站可能没有生效，请修正后重新保存任一配置项以触发重新应用`"
      style="margin-bottom: 12px"
    />
    <el-card shadow="never" class="addr-card">
      <template #header>
        <div class="card-header">
          <span>面板访问地址</span>
          <el-button size="small" @click="loadSys">刷新</el-button>
        </div>
      </template>
      <el-descriptions :column="1" border size="small">
        <el-descriptions-item label="当前访问">
          <span class="mono">{{ sys?.panel?.current || '-' }}</span>
          <el-button link type="primary" @click="copyText(sys?.panel?.current || '')">复制</el-button>
        </el-descriptions-item>
        <el-descriptions-item label="当前监听">
          <span class="mono">{{ sys?.panel?.listen || '-' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="面板域名">
          <template v-if="sys?.panel?.domain">
            <span class="mono">{{ sys.panel.domain }}</span>
            <el-button link type="primary" @click="copyText(sys?.panel?.domain || '')">复制</el-button>
          </template>
          <span v-else class="muted">未设置（可在「面板设置 → 面板域名」填写）</span>
        </el-descriptions-item>
        <el-descriptions-item label="可用地址">
          <div v-for="u in sys?.panel?.urls || []" :key="u" class="addr-line">
            <span class="mono">{{ u }}</span>
            <el-button link type="primary" @click="copyText(u)">复制</el-button>
          </div>
          <span v-if="!sys?.panel?.urls?.length" class="muted">-</span>
        </el-descriptions-item>
      </el-descriptions>
      <div class="hint">
        本机网卡地址一般仅内网可达；公网访问请用公网 IP 或域名，并确保防火墙已放行面板端口。
      </div>
    </el-card>

    <el-row :gutter="12">
      <el-col :span="10">
        <el-card shadow="never">
          <template #header>sing-box 内核</template>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="安装状态">
              <el-tag :type="core?.installed ? 'success' : 'danger'" size="small">
                {{ core?.installed ? '已安装' : '未安装' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="运行状态">
              <el-tag :type="core?.running ? 'success' : 'warning'" size="small">
                {{ core?.running ? '运行中' : '已停止' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="版本">{{ core?.version || '-' }}</el-descriptions-item>
            <el-descriptions-item label="配置文件">
              <span class="mono">{{ core?.config_path }}</span>
            </el-descriptions-item>
          </el-descriptions>

          <el-divider content-position="left">操作</el-divider>
          <el-space wrap>
            <el-button type="primary" :loading="busy" @click="install">安装/更新内核</el-button>
            <el-button :disabled="!core?.installed" @click="action('start')">启动</el-button>
            <el-button :disabled="!core?.running" @click="action('stop')">停止</el-button>
            <el-button :disabled="!core?.installed" @click="action('restart')">重启</el-button>
            <el-button @click="viewConfig">查看配置</el-button>
          </el-space>
          <el-form-item label="指定版本" style="margin-top: 12px">
            <el-input v-model="version" placeholder="留空安装最新版，如 1.10.7" />
          </el-form-item>
          <div class="hint">内核由面板从 GitHub 官方发布页下载，请确保服务器可访问外网。</div>
        </el-card>
      </el-col>

      <el-col :span="14">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>内核日志</span>
              <div>
                <el-switch v-model="autoRefresh" active-text="自动刷新" style="margin-right: 12px" />
                <el-button size="small" @click="loadLogs">刷新</el-button>
                <el-button size="small" @click="clearLogs">清空</el-button>
              </div>
            </div>
          </template>
          <div ref="logBox" class="log-view">
            <div v-for="(line, i) in logs" :key="i">{{ line }}</div>
            <div v-if="!logs.length" class="empty">暂无日志</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="12" style="margin-top: 12px">
      <el-col :span="8">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>本机网卡</span>
              <el-button size="small" @click="loadPorts">刷新</el-button>
            </div>
          </template>
          <el-table :data="pool?.interfaces || []" size="small" border>
            <el-table-column prop="name" label="网卡" min-width="100" />
            <el-table-column label="地址" min-width="170">
              <template #default="{ row }">
                <div v-for="a in row.addresses" :key="a" class="mono">{{ a }}</div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="8">
        <el-card shadow="never">
          <template #header>本机监听端口（{{ (pool?.listening || []).length }}）</template>
          <el-table :data="pool?.listening || []" size="small" border height="300">
            <el-table-column prop="port" label="端口" width="80" />
            <el-table-column label="协议" width="80">
              <template #default="{ row }">{{ (row.proto || []).join('/') }}</template>
            </el-table-column>
            <el-table-column label="归属" min-width="80">
              <template #default="{ row }">
                <el-tag v-if="row.panel" size="small" type="warning">面板</el-tag>
                <span v-else style="color: #909399">其他</span>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="8">
        <el-card shadow="never">
          <template #header>
            可用端口池
            <span v-if="pool" class="pool-range">（{{ pool.range.start }}-{{ pool.range.end }}）</span>
          </template>
          <div v-if="pool && pool.free && pool.free.length" class="pool-box">
            <el-tag v-for="p in pool.free" :key="p" size="small" class="pool-chip" @click="copyPort(p)">
              {{ p }}
            </el-tag>
          </div>
          <el-empty v-else description="范围内无可用端口，请调整端口池区间" :image-size="60" />
          <div class="hint">新建入站时会自动从这里分配；点击端口可复制。</div>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="configDialog" title="当前 sing-box 配置" width="760px">
      <el-input :model-value="configText" type="textarea" :rows="24" readonly />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import type { CoreStatus, PortPool, SysInfo } from '@/api/types'

const core = ref<CoreStatus | null>(null)
const pool = ref<PortPool | null>(null)
const sys = ref<SysInfo | null>(null)
const version = ref('')
const busy = ref(false)
const logs = ref<string[]>([])
const autoRefresh = ref(false)
const configDialog = ref(false)
const configText = ref('')
const logBox = ref<HTMLDivElement>()
let timer: number | undefined

async function loadCore() {
  core.value = await api.get<CoreStatus>('/api/sys/core')
}

async function loadPorts() {
  try {
    pool.value = await api.get<PortPool>('/api/ports')
  } catch {
    /* 忽略 */
  }
}

async function loadSys() {
  try {
    sys.value = await api.get<SysInfo>('/api/sys/info')
  } catch {
    /* 忽略 */
  }
}

async function copyText(t: string) {
  if (!t) return
  try {
    await navigator.clipboard.writeText(t)
    ElMessage.success('已复制')
  } catch {
    /* 忽略 */
  }
}

async function copyPort(p: number) {
  try {
    await navigator.clipboard.writeText(String(p))
    ElMessage.success(`已复制端口 ${p}`)
  } catch {
    /* 忽略 */
  }
}

async function loadLogs() {
  const res = await api.get<{ lines: string[] }>('/api/sys/logs')
  logs.value = res.lines || []
  requestAnimationFrame(() => {
    if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight
  })
}

async function clearLogs() {
  await api.del('/api/sys/logs')
  logs.value = []
}

async function install() {
  busy.value = true
  try {
    const res = await api.post<{ apply_error?: string; start_error?: string }>('/api/sys/core/install', {
      version: version.value,
    })
    if (res.apply_error) ElMessage.warning(`配置应用失败：${res.apply_error}`)
    else if (res.start_error) ElMessage.warning(`内核启动失败：${res.start_error}`)
    else ElMessage.success('内核已安装并启动')
    await loadCore()
    await loadLogs()
  } finally {
    busy.value = false
  }
}

async function action(name: 'start' | 'stop' | 'restart') {
  const res = await api.post<CoreStatus>(`/api/sys/core/${name}`)
  core.value = res
  ElMessage.success('操作成功')
  await loadLogs()
}

async function viewConfig() {
  const res = await api.get<{ config: string }>('/api/sys/config')
  configText.value = res.config
  configDialog.value = true
}

function startTimer() {
  stopTimer()
  timer = window.setInterval(loadLogs, 3000)
}

function stopTimer() {
  if (timer) {
    window.clearInterval(timer)
    timer = undefined
  }
}

watch(autoRefresh, (v) => {
  if (v) startTimer()
  else stopTimer()
})

onMounted(async () => {
  await loadCore()
  await loadLogs()
  await loadPorts()
  await loadSys()
})

onUnmounted(stopTimer)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.empty {
  color: #666;
}

.pool-range {
  font-size: 12px;
  color: #909399;
  font-weight: normal;
}

.pool-box {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-height: 260px;
  overflow: auto;
}

.pool-chip {
  cursor: pointer;
}

.hint {
  margin-top: 8px;
  font-size: 12px;
  color: #909399;
}

.addr-card {
  margin-bottom: 12px;
}

.addr-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.muted {
  color: #909399;
}
</style>
