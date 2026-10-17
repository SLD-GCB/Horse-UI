<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="路由规则按顺序匹配，命中后走指定出站；全部未命中则走「默认出站」。"
      style="margin-bottom: 12px"
    >
      <template #default>
        需要「改发往的地址 / 端口」时，填这一行的<strong>覆盖地址 / 覆盖端口</strong>——
        sing-box 从 1.13 起把该能力从出站挪到了路由层。
      </template>
    </el-alert>

    <div class="toolbar">
      <div class="left">
        <span class="label">默认出站</span>
        <el-select v-model="final" clearable placeholder="direct" style="width: 220px">
          <el-option label="direct（直连）" value="direct" />
          <el-option label="block（拦截）" value="block" />
          <el-option v-for="ob in outbounds" :key="ob.tag" :label="`${ob.tag}（${ob.type}）`" :value="ob.tag" />
        </el-select>
      </div>
      <div class="right">
        <el-button @click="addRule">新增规则</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存并应用</el-button>
      </div>
    </div>

    <el-table :data="rules" border size="small">
      <el-table-column type="index" label="#" width="50" />
      <el-table-column label="来源入站" min-width="130">
        <template #default="{ row }"><el-input v-model="row.inbound" placeholder="可选，逗号分隔" /></template>
      </el-table-column>
      <el-table-column label="域名后缀" min-width="170">
        <template #default="{ row }"><el-input v-model="row.domain_suffix" placeholder="如 google.com" /></template>
      </el-table-column>
      <el-table-column label="域名关键字" min-width="130">
        <template #default="{ row }"><el-input v-model="row.domain_keyword" placeholder="可选" /></template>
      </el-table-column>
      <el-table-column label="IP CIDR" min-width="150">
        <template #default="{ row }"><el-input v-model="row.ip_cidr" placeholder="如 1.2.3.0/24" /></template>
      </el-table-column>
      <el-table-column label="协议" width="110">
        <template #default="{ row }"><el-input v-model="row.protocol" placeholder="如 http" /></template>
      </el-table-column>
      <el-table-column label="覆盖地址" width="140">
        <template #default="{ row }"><el-input v-model="row.override_address" placeholder="可选" /></template>
      </el-table-column>
      <el-table-column label="覆盖端口" width="110">
        <template #default="{ row }"><el-input v-model="row.override_port" placeholder="可选" /></template>
      </el-table-column>
      <el-table-column label="目标出站" width="190">
        <template #default="{ row }">
          <el-select v-model="row.outbound" clearable placeholder="direct" style="width: 100%">
            <el-option label="direct（直连）" value="direct" />
            <el-option label="block（拦截）" value="block" />
            <el-option v-for="ob in outbounds" :key="ob.tag" :label="ob.tag" :value="ob.tag" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="150" fixed="right">
        <template #default="{ $index }">
          <el-button link @click="move($index, -1)">上移</el-button>
          <el-button link @click="move($index, 1)">下移</el-button>
          <el-button link type="danger" @click="rules.splice($index, 1)">删除</el-button>
        </template>
      </el-table-column>
      <template #empty><span class="empty">暂无规则</span></template>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import type { Outbound, RouteConfig } from '@/api/types'

interface RuleForm {
  inbound: string
  domain_suffix: string
  domain_keyword: string
  ip_cidr: string
  protocol: string
  outbound: string
  override_address: string
  override_port: string
}

const rules = ref<RuleForm[]>([])
const final = ref('')
const outbounds = ref<Outbound[]>([])
const saving = ref(false)

function emptyRule(): RuleForm {
  return {
    inbound: '',
    domain_suffix: '',
    domain_keyword: '',
    ip_cidr: '',
    protocol: '',
    outbound: '',
    override_address: '',
    override_port: '',
  }
}

function toList(s: string): string[] {
  return s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
}

function arrText(v: unknown): string {
  return Array.isArray(v) ? (v as string[]).join(',') : ''
}

function ruleToForm(r: Record<string, unknown>): RuleForm {
  return {
    inbound: arrText(r.inbounds),
    domain_suffix: arrText(r.domain_suffix),
    domain_keyword: arrText(r.domain_keyword),
    ip_cidr: arrText(r.ip_cidr),
    protocol: typeof r.protocol === 'string' ? r.protocol : '',
    outbound: typeof r.outbound === 'string' ? r.outbound : '',
    override_address: typeof r.override_address === 'string' ? r.override_address : '',
    override_port: typeof r.override_port === 'number' ? String(r.override_port) : '',
  }
}

function formToRule(f: RuleForm): Record<string, unknown> {
  const r: Record<string, unknown> = {}
  if (f.inbound) r.inbounds = toList(f.inbound)
  if (f.domain_suffix) r.domain_suffix = toList(f.domain_suffix)
  if (f.domain_keyword) r.domain_keyword = toList(f.domain_keyword)
  if (f.ip_cidr) r.ip_cidr = toList(f.ip_cidr)
  if (f.protocol.trim()) r.protocol = f.protocol.trim()
  r.outbound = f.outbound || 'direct'
  // override_* 由 sing-box 在路由层实现（direct 出站自身不接受端口）。
  if (f.override_address.trim()) r.override_address = f.override_address.trim()
  const p = Number(f.override_port)
  if (p > 0 && p <= 65535) r.override_port = p
  return r
}

async function load() {
  const [cfg, obs] = await Promise.all([
    api.get<RouteConfig>('/api/route'),
    api.get<Outbound[]>('/api/outbounds'),
  ])
  outbounds.value = obs
  final.value = cfg.final || ''
  rules.value = (cfg.rules || []).map(ruleToForm)
}

function addRule() {
  rules.value.push(emptyRule())
}

function move(i: number, d: number) {
  const j = i + d
  if (j < 0 || j >= rules.value.length) return
  const t = rules.value[i]
  rules.value[i] = rules.value[j]
  rules.value[j] = t
}

async function save() {
  saving.value = true
  try {
    const res = await api.put<{ apply_error?: string }>('/api/route', {
      rules: rules.value.map(formToRule),
      final: final.value,
    })
    if (res.apply_error) {
      ElMessage.warning(`已保存，但内核应用失败：${res.apply_error}`)
    } else {
      ElMessage.success('已保存并应用')
    }
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.toolbar .left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.label {
  color: #606266;
}

.empty {
  color: #909399;
}
</style>
