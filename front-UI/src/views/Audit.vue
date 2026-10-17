<template>
  <div class="page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>审计日志（最近 10000 条）</span>
          <div class="filters">
            <el-input
              v-model="keyword"
              placeholder="按用户 / 路径 / 动作搜索"
              clearable
              style="width: 260px"
              @keyup.enter="reload"
            />
            <el-button @click="reload">查询</el-button>
            <el-button @click="load">刷新</el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="rows" size="small" border>
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="操作者" width="120" />
        <el-table-column prop="ip" label="来源 IP" width="140" />
        <el-table-column prop="method" label="方法" width="80" />
        <el-table-column prop="path" label="路径" min-width="200" show-overflow-tooltip />
        <el-table-column prop="action" label="动作" min-width="150" show-overflow-tooltip />
        <el-table-column prop="detail" label="详情" width="100" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status < 400 ? 'success' : 'danger'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <template #empty>
          <span class="empty">暂无记录</span>
        </template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, sizes, prev, pager, next"
        :total="total"
        :current-page="page"
        :page-size="size"
        :page-sizes="[20, 50, 100]"
        @current-change="onPage"
        @size-change="onSize"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api'
import type { AuditLog, AuditPage } from '@/api/types'
import { formatTime } from '@/utils/format'

const rows = ref<AuditLog[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const keyword = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await api.get<AuditPage>('/api/audit', {
      page: page.value,
      size: size.value,
      keyword: keyword.value,
    })
    rows.value = res.items || []
    total.value = res.total
  } catch {
    /* 拦截器已提示 */
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function onPage(p: number) {
  page.value = p
  load()
}

function onSize(s: number) {
  size.value = s
  page.value = 1
  load()
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.filters {
  display: flex;
  gap: 8px;
}

.pager {
  margin-top: 12px;
  justify-content: flex-end;
}

.empty {
  color: #909399;
}
</style>
