<template>
  <div class="attack-page">
    <div class="head">
      <div>
        <h2>攻击日志</h2>
        <p class="hint">异常来源由动态容量判定，永久封禁需人工确认。</p>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-tabs v-model="tab">
      <el-tab-pane label="异常 IP" name="sources">
        <el-table :data="sources" size="small" empty-text="暂无异常来源">
          <el-table-column prop="ip" label="IP" min-width="150" />
          <el-table-column label="端口" width="110">
            <template #default="{ row }">{{ row.protocol }}/{{ row.port }}</template>
          </el-table-column>
          <el-table-column prop="count" label="连接数" width="90" />
          <el-table-column label="首次" width="160">
            <template #default="{ row }">{{ fmt(row.first_seen) }}</template>
          </el-table-column>
          <el-table-column label="最近" width="160">
            <template #default="{ row }">{{ fmt(row.last_seen) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.banned ? 'danger' : 'warning'" size="small">
                {{ row.banned ? '已封禁' : '未封禁' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100">
            <template #default="{ row }">
              <el-button
                v-if="!row.banned"
                size="small"
                type="danger"
                plain
                @click="ban(row)"
              >永久封禁</el-button>
              <el-button v-else size="small" type="success" plain @click="unban(row)">
                解封
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="攻击事件" name="logs">
        <el-table :data="logs" size="small" empty-text="暂无攻击事件">
          <el-table-column label="时间" width="160">
            <template #default="{ row }">{{ fmt(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="端口" width="110">
            <template #default="{ row }">{{ row.protocol }}/{{ row.port }}</template>
          </el-table-column>
          <el-table-column label="当前/上限" width="120">
            <template #default="{ row }">
              {{ row.current }} / {{ row.limit.toFixed(0) }}
            </template>
          </el-table-column>
          <el-table-column label="动作" width="90">
            <template #default="{ row }">
              <el-tag :type="actionType(row.action)" size="small">
                {{ actionText(row.action) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="来源 TOP" min-width="300">
            <template #default="{ row }">{{ sourceText(row.sources) }}</template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { FirewallAttackLog, FirewallAttackSource } from '@/api/types'

const tab = ref('sources')
const loading = ref(false)
const sources = ref<FirewallAttackSource[]>([])
const logs = ref<FirewallAttackLog[]>([])
let timer: number | undefined

async function load() {
  loading.value = true
  try {
    const [s, l] = await Promise.all([
      api.get<{ items: FirewallAttackSource[] }>('/api/firewall/attack-sources?limit=200'),
      api.get<{ items: FirewallAttackLog[] }>('/api/firewall/attack-logs?limit=200'),
    ])
    sources.value = s.items ?? []
    logs.value = l.items ?? []
  } catch (e: any) {
    ElMessage.error(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function ban(row: FirewallAttackSource) {
  try {
    const { value } = await ElMessageBox.prompt(
      `确认永久封禁 ${row.ip}？`,
      '封禁 IP',
      { inputValue: `${row.protocol}/${row.port} 异常连接`, confirmButtonText: '封禁' },
    )
    await api.post('/api/firewall/bans', {
      ip: row.ip,
      reason: value || '异常连接',
      permanent: true,
    })
    ElMessage.success('已封禁')
    await load()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error(e?.message || '封禁失败')
  }
}

async function unban(row: FirewallAttackSource) {
  try {
    await api.del(`/api/firewall/bans/${encodeURIComponent(row.ip)}`)
    ElMessage.success('已解封')
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '解封失败')
  }
}

function fmt(ts: number) {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}

function actionText(action: string) {
  if (action === 'close') return '关端口'
  if (action === 'reopen') return '恢复'
  return '观察'
}

function actionType(action: string) {
  if (action === 'close') return 'danger'
  if (action === 'reopen') return 'success'
  return 'warning'
}

function sourceText(raw: string) {
  try {
    const list = JSON.parse(raw || '[]') as Array<{ ip: string; count: number }>
    return list.slice(0, 5).map((e) => `${e.ip} (${e.count})`).join('，') || '—'
  } catch {
    return '—'
  }
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 10000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<style scoped>
.attack-page { padding: 18px; }
.head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
h2 { margin: 0; font-size: 18px; }
.hint { margin: 4px 0 0; color: #8a8f99; font-size: 12px; }
</style>
