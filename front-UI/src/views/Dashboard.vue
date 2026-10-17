<template>
  <div class="page">
    <el-row :gutter="12">
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">内核状态</div>
          <div class="stat-value">
            <el-tag :type="core?.running ? 'success' : 'danger'" size="large">
              {{ core?.running ? '运行中' : core?.installed ? '已停止' : '未安装' }}
            </el-tag>
          </div>
          <div class="stat-sub">版本 {{ core?.version || '-' }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">入站节点</div>
          <div class="stat-value">{{ inbounds.length }}</div>
          <div class="stat-sub">启用 {{ enabledInbounds }} 个</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">用户数</div>
          <div class="stat-value">{{ clients.length }}</div>
          <div class="stat-sub">启用 {{ activeClients }} 个</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="stat-label">累计流量</div>
          <div class="stat-value">{{ formatBytes(totalUsed) }}</div>
          <div class="stat-sub">上行 + 下行</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="12" style="margin-top: 12px">
      <el-col :span="12">
        <el-card shadow="never">
          <div ref="chartEl" style="height: 320px"></div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>系统信息</span>
              <el-button size="small" @click="load">刷新</el-button>
            </div>
          </template>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="主机名">{{ info?.hostname || '-' }}</el-descriptions-item>
            <el-descriptions-item label="系统">{{ info?.os }} / {{ info?.arch }}</el-descriptions-item>
            <el-descriptions-item label="Go 版本">{{ info?.go_version }}</el-descriptions-item>
            <el-descriptions-item label="CPU 核心">{{ info?.cpus }}</el-descriptions-item>
            <el-descriptions-item label="面板运行">{{ formatDuration(info?.uptime || 0) }}</el-descriptions-item>
            <el-descriptions-item label="内存占用">{{ formatBytes(info?.mem_alloc || 0) }}</el-descriptions-item>
            <el-descriptions-item label="协程数">{{ info?.goroutines }}</el-descriptions-item>
            <el-descriptions-item label="面板版本">{{ info?.version }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>
    <el-row style="margin-top: 12px">
      <el-col :span="24">
        <el-alert
          v-if="stats && stats.split"
          type="info"
          show-icon
          :closable="false"
          title="按入站统计：用户用量由绑定入站分摊"
          description="官方 sing-box 发行版无法区分同一入站内不同用户的流量。面板按「入站流量 ÷ 绑定用户数」均摊给各用户；一个入站只绑定一个用户时即为精确值。如需内核级精确统计，请安装带 with_v2ray_api 参数编译的内核，并在「面板设置」填写其下载地址。"
        />
        <el-card shadow="never" :style="{ marginTop: stats && !stats.per_user ? '12px' : '0' }">
          <template #header>
            <div class="card-header">
              <span>入站流量（面板运行期间累计，统计方式：{{ stats?.mode || '-' }}）</span>
              <el-button size="small" @click="loadStats">刷新</el-button>
            </div>
          </template>
          <el-table v-if="stats?.inbounds.length" :data="stats.inbounds" size="small" border>
            <el-table-column prop="tag" label="入站" min-width="180" />
            <el-table-column label="上行" width="130">
              <template #default="{ row }">{{ formatBytes(row.up) }}</template>
            </el-table-column>
            <el-table-column label="下行" width="130">
              <template #default="{ row }">{{ formatBytes(row.down) }}</template>
            </el-table-column>
            <el-table-column label="合计" width="130">
              <template #default="{ row }">{{ formatBytes(row.bytes) }}</template>
            </el-table-column>
            <el-table-column label="绑定用户" width="100">
              <template #default="{ row }">{{ row.clients }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-else description="暂无流量数据" :image-size="60" />
        </el-card>
      </el-col>
    </el-row>
    <el-row style="margin-top: 12px">
      <el-col :span="24">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>连接数诊断（判断是否被按「连接数 / 建连速率」限速）</span>
              <el-button size="small" @click="loadConns">刷新</el-button>
            </div>
          </template>

          <div v-if="!conns" class="stat-sub">加载中 ...</div>
          <div v-else-if="!conns.supported" class="stat-sub">{{ conns.reason || '暂不可用' }}</div>
          <template v-else>
            <el-alert
              :type="connAlertType"
              show-icon
              :closable="false"
              :title="`当前活动连接 ${conns.total} 条`"
              :description="conns.advice"
            />
            <el-table
              v-if="conns.users.length"
              :data="conns.users"
              size="small"
              border
              style="margin-top: 10px"
            >
              <el-table-column prop="source_ip" label="来源 IP" min-width="170" />
              <el-table-column prop="conns" label="并发连接" width="110" />
              <el-table-column label="近 1 分钟新建" width="140">
                <template #default="{ row }">
                  {{ conns?.has_start ? row.new_last_min : '不可用' }}
                </template>
              </el-table-column>
              <el-table-column label="风险" width="100">
                <template #default="{ row }">
                  <el-tag size="small" :type="levelTag(row.level)">{{ levelText(row.level) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="主要目标" min-width="240">
                <template #default="{ row }">{{ (row.top_hosts || []).join(' , ') || '-' }}</template>
              </el-table-column>
            </el-table>
            <el-empty v-else description="当前没有活动连接" :image-size="60" />
            <div class="stat-sub" style="margin-top: 8px">
              参考阈值：并发连接 ≥ {{ conns.watch_conns }} 为「注意」、≥ {{ conns.high_conns }} 为「高危」。
              运营商策略差异很大，这里只用来判断「明显偏高」；同一出口 IP 后有多人（NAT）时数值会叠加。
              <span v-if="!conns.has_start">当前内核未返回连接建立时间，「近 1 分钟新建」不可用。</span>
            </div>
          </template>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import * as echarts from 'echarts'
import { api } from '@/api'
import type { Client, ConnReport, CoreStatus, Inbound, StatsSummary, SysInfo } from '@/api/types'
import { formatBytes, formatDuration } from '@/utils/format'

const info = ref<SysInfo | null>(null)
const core = ref<CoreStatus | null>(null)
const clients = ref<Client[]>([])
const inbounds = ref<Inbound[]>([])
const stats = ref<StatsSummary | null>(null)
const conns = ref<ConnReport | null>(null)
const chartEl = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
let statsTimer: number | undefined

/** 取所有来源 IP 里最差的风险等级。 */
function worstLevel(): string {
  let worst = 'ok'
  for (const row of conns.value?.users || []) {
    if (row.level === 'high') return 'high'
    if (row.level === 'watch') worst = 'watch'
  }
  return worst
}

const connAlertType = computed(() => {
  const w = worstLevel()
  if (w === 'high') return 'error'
  if (w === 'watch') return 'warning'
  return 'success'
})

function levelTag(level: string): 'success' | 'warning' | 'danger' {
  if (level === 'high') return 'danger'
  if (level === 'watch') return 'warning'
  return 'success'
}

function levelText(level: string): string {
  if (level === 'high') return '高危'
  if (level === 'watch') return '注意'
  return '正常'
}

const totalUsed = computed(() => clients.value.reduce((s, c) => s + c.used_up + c.used_down, 0))
const activeClients = computed(() => clients.value.filter((c) => c.enable).length)
const enabledInbounds = computed(() => inbounds.value.filter((i) => i.enable).length)

async function load() {
  const [i, c, cl, ib] = await Promise.all([
    api.get<SysInfo>('/api/sys/info'),
    api.get<CoreStatus>('/api/sys/core'),
    api.get<Client[]>('/api/clients'),
    api.get<Inbound[]>('/api/inbounds'),
  ])
  info.value = i
  core.value = c
  clients.value = cl
  inbounds.value = ib
  renderChart()
  await loadStats()
  await loadConns()
}

async function loadStats() {
  try {
    stats.value = await api.get<StatsSummary>('/api/stats/summary')
  } catch {
    /* 忽略统计接口异常 */
  }
}

async function loadConns() {
  try {
    conns.value = await api.get<ConnReport>('/api/stats/conns')
  } catch {
    /* 忽略诊断接口异常 */
  }
}

function renderChart() {
  if (!chartEl.value) return
  if (!chart) {
    chart = echarts.init(chartEl.value)
  }
  const data = clients.value
    .map((c) => ({ name: c.name, value: c.used_up + c.used_down }))
    .filter((d) => d.value > 0)
    .sort((a, b) => b.value - a.value)
    .slice(0, 10)

  chart.setOption({
    title: { text: '用户流量占比（Top 10）', left: 'center', textStyle: { fontSize: 14 } },
    tooltip: {
      trigger: 'item',
      formatter: (p: { name: string; value: number; percent: number }) =>
        `${p.name}<br/>${formatBytes(p.value)} (${p.percent}%)`,
    },
    legend: { bottom: 0, type: 'scroll' },
    series: [
      {
        type: 'pie',
        radius: ['42%', '66%'],
        center: ['50%', '50%'],
        data: data.length ? data : [{ name: '暂无流量', value: 1 }],
        label: { show: false },
      },
    ],
  })
}

function onResize() {
  chart?.resize()
}

onMounted(async () => {
  await load()
  window.addEventListener('resize', onResize)
  statsTimer = window.setInterval(() => {
    loadStats()
    loadConns()
  }, 10000)
})

onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  if (statsTimer) window.clearInterval(statsTimer)
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.stat-label {
  color: #909399;
  font-size: 13px;
}

.stat-value {
  font-size: 26px;
  font-weight: 600;
  margin: 8px 0 4px;
}

.stat-sub {
  color: #909399;
  font-size: 12px;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>
