<template>
  <div class="page">
    <div class="page-header">
      <div class="page-title">用户管理</div>
      <div>
        <el-select v-model="filterInbound" clearable placeholder="按入站筛选" style="width: 200px; margin-right: 8px" @change="load">
          <el-option v-for="ib in inbounds" :key="ib.id" :label="`${ib.tag} (${ib.type})`" :value="ib.id" />
        </el-select>
        <el-button @click="openBatch">批量新增</el-button>
        <el-button type="primary" @click="openCreate">新增用户</el-button>
      </div>
    </div>

    <el-table :data="rows" border stripe>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="name" label="用户名" min-width="120" />
      <el-table-column label="生效入站" min-width="200">
        <template #default="{ row }">
          <el-tag v-for="id in row.inbound_ids" :key="id" size="small" style="margin: 2px 4px 2px 0">
            {{ inboundTag(id) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="已用 / 配额" min-width="170">
        <template #default="{ row }">
          {{ formatBytes(row.used_up + row.used_down) }} /
          {{ row.quota_bytes ? formatBytes(row.quota_bytes) : '不限' }}
        </template>
      </el-table-column>
      <el-table-column label="到期" width="180">
        <template #default="{ row }">{{ formatTime(row.expire_at) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.enable ? 'success' : 'info'" size="small">{{ row.enable ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="360" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openLinks(row)">链接</el-button>
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="warning" @click="resetTraffic(row)">重置流量</el-button>
          <el-button link type="warning" @click="rotate(row)">换密钥</el-button>
          <el-button link type="warning" @click="resetSub(row)">重置订阅</el-button>
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="form.id ? '编辑用户' : '新增用户'" width="620px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="所属入站">
          <el-select v-model="form.inbound_id" :disabled="!!form.id" style="width: 100%" @change="onInboundChange">
            <el-option
              v-for="ib in inbounds"
              :key="ib.id"
              :label="`${ib.tag} (${ib.type})`"
              :value="ib.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="额外绑定入站">
          <el-select
            v-model="form.inbound_ids"
            multiple
            clearable
            style="width: 100%"
            placeholder="可再绑定其他入站，流量合并统计"
          >
            <el-option
              v-for="ib in bindableInbounds"
              :key="ib.id"
              :label="`${ib.tag} (${ib.type})`"
              :value="ib.id"
            />
          </el-select>
          <div class="hint">
            该用户的凭据会同时写入这些入站的用户列表；流量按绑定入站合并统计。
            若一个入站被多个用户绑定，则按绑定人数均摊。
          </div>
        </el-form-item>
        <el-form-item label="用户名">
          <el-input v-model="form.name" placeholder="全局唯一，用作内核统计名" />
        </el-form-item>
        <el-form-item v-if="needUUID" label="UUID">
          <el-input v-model="form.uuid">
            <template #append><el-button @click="genUUID">生成</el-button></template>
          </el-input>
        </el-form-item>
        <el-form-item v-if="needPassword" label="密码">
          <el-input v-model="form.password">
            <template #append><el-button @click="genPassword">生成</el-button></template>
          </el-input>
        </el-form-item>
        <el-form-item v-if="formType === 'vless'" label="Flow">
          <el-select v-model="form.flow" clearable style="width: 100%">
            <el-option label="无" value="" />
            <el-option label="xtls-rprx-vision" value="xtls-rprx-vision" />
          </el-select>
        </el-form-item>
        <el-form-item label="流量配额 (GB)">
          <el-input-number v-model="form.quota_gb" :min="0" :precision="2" style="width: 100%" />
          <div class="hint">0 表示不限流量</div>
        </el-form-item>
        <el-form-item label="到期时间">
          <el-date-picker
            v-model="form.expire"
            type="datetime"
            placeholder="留空表示永不过期"
            style="width: 100%"
            clearable
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enable" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存并应用</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="batchDialog" title="批量新增用户" width="620px">
      <el-form :model="batchForm" label-width="110px">
        <el-form-item label="所属入站">
          <el-select v-model="batchForm.inbound_id" style="width: 100%">
            <el-option
              v-for="ib in inbounds"
              :key="ib.id"
              :label="`${ib.tag} (${ib.type})`"
              :value="ib.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="额外绑定入站">
          <el-select
            v-model="batchForm.inbound_ids"
            multiple
            clearable
            style="width: 100%"
            placeholder="可再绑定其他入站，流量合并统计"
          >
            <el-option
              v-for="ib in batchBindableInbounds"
              :key="ib.id"
              :label="`${ib.tag} (${ib.type})`"
              :value="ib.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="创建数量">
          <el-input-number v-model="batchForm.count" :min="1" :max="200" style="width: 100%" />
        </el-form-item>
        <el-form-item label="用户名前缀">
          <el-input v-model="batchForm.name_prefix" placeholder="user-" />
          <div class="hint">按「前缀 + 编号」生成，例如 user-01、user-02</div>
        </el-form-item>
        <el-form-item label="起始编号">
          <el-input-number v-model="batchForm.name_start" :min="0" :max="999999" style="width: 100%" />
          <div class="hint">0 表示接着同前缀的已有最大编号继续</div>
        </el-form-item>
        <el-form-item v-if="batchNeedFlow" label="Flow">
          <el-select v-model="batchForm.flow" clearable style="width: 100%">
            <el-option label="无" value="" />
            <el-option label="xtls-rprx-vision" value="xtls-rprx-vision" />
          </el-select>
        </el-form-item>
        <el-form-item label="流量配额 (GB)">
          <el-input-number v-model="batchForm.quota_gb" :min="0" :precision="2" style="width: 100%" />
          <div class="hint">0 表示不限流量，每个用户独立计算</div>
        </el-form-item>
        <el-form-item label="到期时间">
          <el-date-picker
            v-model="batchForm.expire"
            type="datetime"
            placeholder="留空表示永不过期"
            style="width: 100%"
            clearable
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="batchForm.remark" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="batchForm.enable" />
        </el-form-item>
      </el-form>
      <div class="batch-preview">将创建 {{ batchForm.count }} 个用户：{{ batchPreview }}</div>
      <template #footer>
        <el-button @click="batchDialog = false">取消</el-button>
        <el-button type="primary" :loading="batchSaving" @click="saveBatch">批量创建并应用</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="linkDialog" title="分享与订阅" width="680px">
      <div v-if="links">
        <el-row :gutter="16">
          <el-col :span="9">
            <div class="qr-box">
              <img v-if="links.qrcode" :src="links.qrcode" alt="qrcode" />
            </div>
            <div v-if="links.nodes > 1" class="qr-hint">
              共 {{ links.nodes }} 个节点<br />二维码仅含第 1 个
            </div>
          </el-col>
          <el-col :span="15">
            <el-form label-width="110px">
              <el-form-item label="通用订阅">
                <el-input v-model="links.sub_url" readonly>
                  <template #append><el-button @click="copy(links.sub_url)">复制</el-button></template>
                </el-input>
              </el-form-item>
              <el-form-item label="Clash 订阅">
                <el-input :model-value="links.sub_url + '?target=clash'" readonly>
                  <template #append>
                    <el-button @click="copy(links.sub_url + '?target=clash')">复制</el-button>
                  </template>
                </el-input>
              </el-form-item>
              <el-form-item label="sing-box 订阅">
                <el-input :model-value="links.sub_url + '?target=singbox'" readonly>
                  <template #append>
                    <el-button @click="copy(links.sub_url + '?target=singbox')">复制</el-button>
                  </template>
                </el-input>
              </el-form-item>
              <el-form-item :label="`分享链接(${links.nodes})`">
                <el-input v-model="links.link" type="textarea" :rows="3" readonly />
              </el-form-item>
              <el-button size="small" @click="copy(links.link)">复制全部节点</el-button>
            </el-form>
          </el-col>
        </el-row>
        <el-tabs style="margin-top: 12px">
          <el-tab-pane label="sing-box 配置文件">
            <div class="tab-actions">
              <span class="hint">可直接另存为 config.json 导入 sing-box 客户端</span>
              <el-button size="small" @click="copy(singboxText)">复制配置</el-button>
            </div>
            <el-input :model-value="singboxText" type="textarea" :rows="16" readonly />
          </el-tab-pane>
          <el-tab-pane label="Clash 配置文件">
            <div class="tab-actions">
              <span class="hint">可直接另存为 config.yaml 导入 Clash.Meta / mihomo</span>
              <el-button size="small" @click="copy(clashText)">复制配置</el-button>
            </div>
            <el-input :model-value="clashText" type="textarea" :rows="16" readonly />
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { Client, ClientLinks, Inbound } from '@/api/types'
import { formatBytes, formatTime } from '@/utils/format'

const rows = ref<Client[]>([])
const inbounds = ref<Inbound[]>([])
const filterInbound = ref<number | undefined>(undefined)
const dialog = ref(false)
const saving = ref(false)
const batchDialog = ref(false)
const batchSaving = ref(false)
const linkDialog = ref(false)
const links = ref<ClientLinks | null>(null)
const clashText = ref('')
const singboxText = ref('')

const form = reactive({
  id: 0,
  inbound_id: 0,
  inbound_ids: [] as number[],
  name: '',
  uuid: '',
  password: '',
  flow: '',
  enable: true,
  quota_gb: 0,
  expire: null as Date | null,
  remark: '',
})

const batchForm = reactive({
  inbound_id: 0,
  inbound_ids: [] as number[],
  count: 10,
  name_prefix: 'user-',
  name_start: 0,
  flow: '',
  quota_gb: 0,
  expire: null as Date | null,
  enable: true,
  remark: '',
})

const formType = computed(() => inbounds.value.find((i) => i.id === form.inbound_id)?.type || '')
const currentInbound = computed(() => inbounds.value.find((i) => i.id === form.inbound_id))
const needUUID = computed(() => ['vless', 'vmess', 'tuic'].includes(formType.value))
const needPassword = computed(() => ['trojan', 'shadowsocks', 'tuic', 'hysteria2', 'http'].includes(formType.value))
const bindableInbounds = computed(() => inbounds.value.filter((ib) => ib.id !== form.inbound_id))
const batchType = computed(() => inbounds.value.find((i) => i.id === batchForm.inbound_id)?.type || '')
const batchNeedFlow = computed(() => batchType.value === 'vless')
const batchBindableInbounds = computed(() =>
  inbounds.value.filter((ib) => ib.id !== batchForm.inbound_id),
)
const batchPreview = computed(() => {
  const count = Math.max(1, Math.min(batchForm.count || 1, 200))
  const prefix = batchForm.name_prefix || 'user-'
  if (!batchForm.name_start) return `${prefix}（自动接续已有编号）`
  const start = batchForm.name_start
  const width = Math.max(2, String(start + count - 1).length)
  const first = `${prefix}${String(start).padStart(width, '0')}`
  const last = `${prefix}${String(start + count - 1).padStart(width, '0')}`
  return count > 1 ? `${first} … ${last}` : first
})

function inboundTag(id: number): string {
  const ib = inbounds.value.find((i) => i.id === id)
  return ib ? ib.tag : `#${id}`
}

async function load() {
  const params = filterInbound.value ? { inbound_id: filterInbound.value } : undefined
  rows.value = await api.get<Client[]>('/api/clients', params)
}

async function loadInbounds() {
  inbounds.value = await api.get<Inbound[]>('/api/inbounds')
}

function openCreate() {
  if (!inbounds.value.length) {
    ElMessage.warning('请先创建入站')
    return
  }
  Object.assign(form, {
    id: 0,
    inbound_id: inbounds.value[0].id,
    inbound_ids: [],
    name: '',
    uuid: '',
    password: '',
    flow: '',
    enable: true,
    quota_gb: 0,
    expire: null,
    remark: '',
  })
  dialog.value = true
  void fillSecrets()
}

function openBatch() {
  if (!inbounds.value.length) {
    ElMessage.warning('请先创建入站')
    return
  }
  Object.assign(batchForm, {
    inbound_id: inbounds.value[0].id,
    inbound_ids: [],
    count: 10,
    name_prefix: 'user-',
    name_start: 0,
    flow: '',
    quota_gb: 0,
    expire: null,
    enable: true,
    remark: '',
  })
  batchDialog.value = true
}

async function saveBatch() {
  if (!batchForm.inbound_id) {
    ElMessage.warning('请选择入站')
    return
  }
  if (batchForm.count < 1 || batchForm.count > 200) {
    ElMessage.warning('单次创建数量需在 1-200 之间')
    return
  }
  batchSaving.value = true
  try {
    const res = await api.post<{ count?: number; apply_error?: string }>('/api/clients/batch', {
      inbound_id: batchForm.inbound_id,
      inbound_ids: batchForm.inbound_ids,
      count: batchForm.count,
      name_prefix: batchForm.name_prefix.trim(),
      name_start: batchForm.name_start,
      flow: batchForm.flow,
      quota_bytes: Math.round((batchForm.quota_gb || 0) * 1024 ** 3),
      expire_at: batchForm.expire ? batchForm.expire.toISOString() : null,
      enable: batchForm.enable,
      remark: batchForm.remark,
    })
    if (res.apply_error) {
      ElMessage.warning(`已创建 ${res.count || batchForm.count} 个用户，但内核应用失败：${res.apply_error}`)
    } else {
      ElMessage.success(`已创建 ${res.count || batchForm.count} 个用户并应用`)
    }
    batchDialog.value = false
    await load()
  } finally {
    batchSaving.value = false
  }
}

// onInboundChange 切换协议后清空旧凭据，并按新协议自动补全。
function onInboundChange() {
  form.uuid = ''
  form.password = ''
  form.flow = ''
  void fillSecrets()
}

// fillSecrets 新增用户时按协议自动生成 UUID / 16 位随机密码，节省手填。
async function fillSecrets() {
  if (form.id) return
  if (needUUID.value && !form.uuid) await genUUID()
  if (needPassword.value && !form.password) await genPassword()
}

function openEdit(row: Client) {
  Object.assign(form, {
    id: row.id,
    inbound_id: row.inbound_id,
    inbound_ids: (row.inbound_ids || []).filter((id) => id !== row.inbound_id),
    name: row.name,
    uuid: row.uuid,
    password: row.password,
    flow: row.flow,
    enable: row.enable,
    quota_gb: row.quota_bytes ? row.quota_bytes / 1024 ** 3 : 0,
    expire: row.expire_at ? new Date(row.expire_at) : null,
    remark: row.remark,
  })
  dialog.value = true
}

async function genUUID() {
  const res = await api.post<{ uuid: string }>('/api/tools/uuid', {})
  form.uuid = res.uuid
}

async function genPassword() {
  // Shadowsocks（尤其 2022）的密码必须是符合 method 的 base64 密钥，不能随便生成。
  const url = formType.value === 'shadowsocks' ? '/api/tools/ss-password' : '/api/tools/password'
  const res = await api.post<{ password: string }>(url, { method: currentInbound.value?.method })
  form.password = res.password
}

async function save() {
  if (!form.inbound_id) {
    ElMessage.warning('请选择入站')
    return
  }
  saving.value = true
  try {
    const payload = {
      id: form.id,
      inbound_id: form.inbound_id,
      inbound_ids: form.inbound_ids,
      name: form.name,
      uuid: form.uuid,
      password: form.password,
      flow: form.flow,
      enable: form.enable,
      quota_bytes: Math.round((form.quota_gb || 0) * 1024 ** 3),
      expire_at: form.expire ? form.expire.toISOString() : null,
      remark: form.remark,
    }
    const res = form.id
      ? await api.put<{ apply_error?: string }>(`/api/clients/${form.id}`, payload)
      : await api.post<{ apply_error?: string }>('/api/clients', payload)
    if (res.apply_error) {
      ElMessage.warning(`已保存，但内核应用失败：${res.apply_error}`)
    } else {
      ElMessage.success('已保存并应用')
    }
    dialog.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function openLinks(row: Client) {
  links.value = await api.get<ClientLinks>(`/api/clients/${row.id}/links`)
  clashText.value = ''
  singboxText.value = ''
  linkDialog.value = true
  await Promise.all([loadRaw('clash'), loadRaw('singbox')])
}

// loadRaw 直接拉取订阅端点原文，展示客户端实际会收到的内容。
async function loadRaw(target: 'clash' | 'singbox') {
  const url = links.value?.sub_url
  if (!url) return
  try {
    const res = await fetch(`${url}?target=${target}`)
    const text = await res.text()
    if (target === 'clash') clashText.value = text
    else singboxText.value = text
  } catch {
    /* 忽略拉取失败 */
  }
}

async function resetTraffic(row: Client) {
  await ElMessageBox.confirm(`确认重置 ${row.name} 的流量统计？`, '提示', { type: 'warning' })
  await api.post(`/api/clients/${row.id}/reset-traffic`)
  ElMessage.success('已重置')
  await load()
}

async function rotate(row: Client) {
  await ElMessageBox.confirm(`确认重置 ${row.name} 的密钥？旧配置将立即失效。`, '提示', { type: 'warning' })
  await api.post(`/api/clients/${row.id}/rotate`)
  ElMessage.success('已重置密钥')
  await load()
}

async function resetSub(row: Client) {
  await ElMessageBox.confirm(`确认重置 ${row.name} 的订阅令牌？旧订阅地址将立即失效。`, '提示', { type: 'warning' })
  await api.post(`/api/clients/${row.id}/reset-sub-token`)
  ElMessage.success('已重置订阅令牌')
  await load()
}

async function remove(row: Client) {
  await ElMessageBox.confirm(`确认删除用户 ${row.name}？`, '提示', { type: 'warning' })
  await api.del(`/api/clients/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

async function copy(text: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}

onMounted(async () => {
  await Promise.all([loadInbounds(), load()])
})
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: #909399;
}

.batch-preview {
  margin: 0 0 4px 110px;
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}

.qr-hint {
  margin-top: 6px;
  font-size: 12px;
  color: #909399;
  text-align: center;
  line-height: 1.5;
}

.tab-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
</style>
