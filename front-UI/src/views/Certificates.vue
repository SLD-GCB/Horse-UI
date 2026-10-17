<template>
  <div class="page">
    <div class="page-header">
      <div class="page-title">证书管理</div>
      <el-button type="primary" @click="openCreate">申请 / 生成证书</el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 12px"
      title="有域名可用 ACME 自动签发（Let's Encrypt）。没有域名可用自签证书，客户端需勾选「允许不安全」或导入该证书作为信任根。"
    />

    <el-table :data="rows" border stripe>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="类型" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.kind === 'selfsigned' ? 'warning' : 'success'">
            {{ row.kind === 'selfsigned' ? '自签' : 'ACME' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="domain" label="名称 / 域名" min-width="200" />
      <el-table-column label="签发方式" width="130">
        <template #default="{ row }">{{ row.kind === 'selfsigned' ? '本地生成' : row.provider || 'http' }}</template>
      </el-table-column>
      <el-table-column label="覆盖名称 (SAN)" min-width="180">
        <template #default="{ row }">
          <span class="hint">{{ sanLabel(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="到期时间" min-width="200">
        <template #default="{ row }">
          <span>{{ fmt(row.not_after) }}</span>
          <el-tag v-if="daysLeft(row.not_after) !== null" size="small" :type="tagType(row.not_after)" style="margin-left: 8px">
            剩余 {{ daysLeft(row.not_after) }} 天
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="renew(row)">
            {{ row.kind === 'selfsigned' ? '重新生成' : '续签' }}
          </el-button>
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" title="申请 / 生成证书" width="660px">
      <el-form :model="form" label-width="130px">
        <el-form-item label="证书类型">
          <el-radio-group v-model="form.kind">
            <el-radio-button value="acme">ACME（有域名）</el-radio-button>
            <el-radio-button value="selfsigned">自签（无域名）</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <!-- ============ ACME ============ -->
        <template v-if="form.kind === 'acme'">
          <el-form-item label="域名">
            <el-input v-model="form.domain" placeholder="如 node.example.com" />
          </el-form-item>
          <el-form-item label="签发方式">
            <el-select v-model="form.provider" style="width: 100%">
              <el-option v-for="p in providers" :key="p.name" :label="p.label" :value="p.name" />
            </el-select>
          </el-form-item>
          <el-form-item v-for="f in currentFields" :key="f.key" :label="f.label">
            <el-input
              v-model="form.credentials[f.key]"
              :type="f.secret ? 'password' : 'text'"
              :show-password="f.secret"
              :placeholder="f.required ? '必填' : '可选'"
            />
          </el-form-item>
        </template>

        <!-- ============ 自签 ============ -->
        <template v-else>
          <el-form-item label="证书名称">
            <el-input v-model="form.domain" placeholder="留空取第一个名称，仅用于面板内标识" />
          </el-form-item>
          <el-form-item label="覆盖名称 (SAN)">
            <el-input
              v-model="form.names"
              type="textarea"
              :rows="3"
              placeholder="域名或 IP，多个用逗号或换行分隔，例如：&#10;example.com&#10;192.168.1.10"
            />
            <div class="hint">客户端用哪个地址连你，就把它填进来（域名或 IP 都可以）。</div>
          </el-form-item>
          <el-form-item label="有效期（天）">
            <el-input-number v-model="form.days" :min="1" :max="36500" style="width: 200px" />
            <span class="hint" style="margin-left: 10px">默认 3650 天（10 年）</span>
          </el-form-item>
          <el-form-item label="本机地址快捷填入">
            <div class="quick-names">
              <el-tag
                v-for="a in localAddresses"
                :key="a"
                size="small"
                class="port-chip"
                @click="addName(a)"
              >
                + {{ a }}
              </el-tag>
              <span v-if="!localAddresses.length" class="hint">未读取到本机地址</span>
            </div>
          </el-form-item>
        </template>

        <el-form-item label="自动续签">
          <el-switch v-model="form.auto_renew" />
          <span class="hint" style="margin-left: 10px">
            {{ form.kind === 'selfsigned' ? '距到期 30 天内重新生成（会更换密钥）' : '距到期 30 天内自动续签' }}
          </span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">
          {{ form.kind === 'selfsigned' ? '生成' : '申请' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { Certificate, CertProviderSpec, PortPool } from '@/api/types'

const rows = ref<Certificate[]>([])
const providers = ref<CertProviderSpec[]>([])
const localAddresses = ref<string[]>([])
const dialog = ref(false)
const saving = ref(false)

const form = reactive({
  kind: 'acme' as 'acme' | 'selfsigned',
  domain: '',
  provider: 'http',
  auto_renew: true,
  credentials: {} as Record<string, string>,
  names: '',
  days: 3650,
})

const currentFields = computed(() => providers.value.find((p) => p.name === form.provider)?.fields || [])

function asObj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
}

function sanLabel(row: Certificate): string {
  if (row.kind !== 'selfsigned') return row.domain
  const names = asObj(row.credentials)?.names
  return Array.isArray(names) && names.length ? (names as string[]).join(', ') : '-'
}

function daysLeft(notAfter: string | null): number | null {
  if (!notAfter) return null
  const t = new Date(notAfter).getTime()
  if (Number.isNaN(t)) return null
  return Math.max(0, Math.floor((t - Date.now()) / 86400000))
}

function tagType(notAfter: string | null): 'success' | 'warning' | 'danger' | 'info' {
  const d = daysLeft(notAfter)
  if (d === null) return 'info'
  if (d > 30) return 'success'
  if (d > 7) return 'warning'
  return 'danger'
}

function fmt(notAfter: string | null): string {
  if (!notAfter) return '-'
  const d = new Date(notAfter)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString()
}

async function load() {
  rows.value = await api.get<Certificate[]>('/api/certs')
}

async function loadProviders() {
  providers.value = await api.get<CertProviderSpec[]>('/api/cert-providers')
}

async function loadLocalAddresses() {
  try {
    const pool = await api.get<PortPool>('/api/ports')
    const out: string[] = []
    for (const it of pool.interfaces || []) {
      for (const a of it.addresses) {
        if (!out.includes(a)) out.push(a)
      }
    }
    localAddresses.value = out
  } catch {
    localAddresses.value = []
  }
}

function addName(a: string) {
  const list = form.names
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (list.includes(a)) return
  list.push(a)
  form.names = list.join('\n')
}

function openCreate() {
  form.kind = 'acme'
  form.domain = ''
  form.provider = 'http'
  form.auto_renew = true
  form.credentials = {}
  form.names = ''
  form.days = 3650
  dialog.value = true
}

async function save() {
  if (form.kind === 'acme') {
    if (!form.domain) {
      ElMessage.warning('请填写域名')
      return
    }
    for (const f of currentFields.value) {
      if (f.required && !form.credentials[f.key]) {
        ElMessage.warning(`请填写 ${f.label}`)
        return
      }
    }
    saving.value = true
    try {
      await api.post<Certificate>(
        '/api/certs',
        {
          kind: 'acme',
          domain: form.domain,
          provider: form.provider,
          credentials: form.credentials,
          auto_renew: form.auto_renew,
        },
        300000,
      )
      ElMessage.success('证书申请成功')
      dialog.value = false
      await load()
    } finally {
      saving.value = false
    }
    return
  }

  // ---- 自签 ----
  const names = form.names
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (!names.length) {
    ElMessage.warning('请至少填写一个域名或 IP')
    return
  }
  saving.value = true
  try {
    await api.post<Certificate>(
      '/api/certs',
      {
        kind: 'selfsigned',
        domain: form.domain,
        names,
        days: form.days,
        auto_renew: form.auto_renew,
      },
      60000,
    )
    ElMessage.success('自签证书已生成')
    dialog.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function renew(row: Certificate) {
  saving.value = true
  try {
    await api.post<Certificate>(`/api/certs/${row.id}/renew`, {}, 300000)
    ElMessage.success(row.kind === 'selfsigned' ? '已重新生成' : '续签成功')
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row: Certificate) {
  await ElMessageBox.confirm(`确认删除证书 ${row.domain}？已引用该证书的入站将失效。`, '提示', { type: 'warning' })
  await api.del(`/api/certs/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

onMounted(async () => {
  await Promise.all([loadProviders(), load(), loadLocalAddresses()])
})
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
}

.quick-names {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.port-chip {
  cursor: pointer;
}
</style>
