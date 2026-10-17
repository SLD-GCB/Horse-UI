<template>
  <el-container class="layout">
    <el-aside width="210px" class="aside">
      <div class="brand">
        <el-icon><Connection /></el-icon>
        <span>HorseUI</span>
      </div>
      <el-menu :default-active="active" router class="menu">
        <el-menu-item index="/dashboard">
          <el-icon><Odometer /></el-icon><span>仪表盘</span>
        </el-menu-item>
        <el-menu-item index="/inbounds">
          <el-icon><Share /></el-icon><span>入站管理</span>
        </el-menu-item>
        <el-menu-item index="/clients">
          <el-icon><User /></el-icon><span>用户管理</span>
        </el-menu-item>
        <el-menu-item index="/outbounds">
          <el-icon><Guide /></el-icon><span>出站与转发</span>
        </el-menu-item>
        <el-menu-item index="/certificates">
          <el-icon><Lock /></el-icon><span>证书管理</span>
        </el-menu-item>
        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon><span>面板设置</span>
        </el-menu-item>
        <el-menu-item index="/audit">
          <el-icon><Document /></el-icon><span>审计日志</span>
        </el-menu-item>
        <el-menu-item index="/system">
          <el-icon><Monitor /></el-icon><span>系统与内核</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="page-title">{{ currentTitle }}</div>
        <div class="right">
          <el-tag v-if="auth.version" size="small" type="info">面板 v{{ auth.version }}</el-tag>
          <el-tag v-if="core" size="small" :type="core.running ? 'success' : 'danger'">
            内核 {{ core.installed ? core.version || '已安装' : '未安装' }}
          </el-tag>
          <el-dropdown @command="onCommand">
            <span class="user">
              <el-icon><UserFilled /></el-icon>
              {{ auth.username }}
              <el-icon><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="password">修改密码</el-dropdown-item>
                <el-dropdown-item command="settings">面板设置</el-dropdown-item>
                <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>

    <el-dialog
      v-model="pwdDialog"
      title="修改密码"
      width="420px"
      :close-on-click-modal="!auth.mustChange"
      :close-on-press-escape="!auth.mustChange"
      :show-close="!auth.mustChange"
    >
      <el-alert
        v-if="auth.mustChange"
        type="warning"
        :closable="false"
        show-icon
        title="首次登录需修改初始密码后才能继续使用面板"
        style="margin-bottom: 12px"
      />
      <el-form :model="pwdForm" label-width="90px">
        <el-form-item label="原密码">
          <el-input v-model="pwdForm.old" type="password" show-password />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="pwdForm.neo" type="password" show-password placeholder="至少 8 位，含字母和数字" />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="pwdForm.confirm" type="password" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button v-if="!auth.mustChange" @click="pwdDialog = false">取消</el-button>
        <el-button type="primary" :loading="pwdLoading" @click="submitPassword">确定</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import type { CoreStatus, SysInfo } from '@/api/types'
import { useAuthStore } from '@/store/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const core = ref<CoreStatus | null>(null)

const pwdDialog = ref(false)
const pwdLoading = ref(false)
const pwdForm = reactive({ old: '', neo: '', confirm: '' })

const active = computed(() => route.path)
const currentTitle = computed(() => (route.meta.title as string) || 'HorseUI')

async function loadCore() {
  try {
    core.value = await api.get<CoreStatus>('/api/sys/core')
    auth.version ||= ''
  } catch {
    /* 忽略 */
  }
}

function openPassword() {
  pwdForm.old = ''
  pwdForm.neo = ''
  pwdForm.confirm = ''
  pwdDialog.value = true
}

async function submitPassword() {
  if (!pwdForm.old || !pwdForm.neo) {
    ElMessage.warning('请填写原密码和新密码')
    return
  }
  if (pwdForm.neo.length < 8) {
    ElMessage.warning('新密码至少 8 位')
    return
  }
  if (pwdForm.neo !== pwdForm.confirm) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  pwdLoading.value = true
  try {
    await auth.changePassword(pwdForm.old, pwdForm.neo)
    ElMessage.success('密码修改成功')
    pwdDialog.value = false
  } catch {
    /* 拦截器已提示 */
  } finally {
    pwdLoading.value = false
  }
}

async function onCommand(cmd: string) {
  if (cmd === 'logout') {
    await auth.logout()
    router.push('/login')
  } else if (cmd === 'settings') {
    router.push('/settings')
  } else if (cmd === 'password') {
    openPassword()
  }
}

onMounted(async () => {
  await loadCore()
  if (auth.mustChange) {
    openPassword()
  }
  if (!auth.version) {
    try {
      const info = await api.get<SysInfo>('/api/sys/info')
      auth.version = info.version
    } catch {
      /* 忽略 */
    }
  }
})
</script>

<style scoped>
.layout {
  height: 100vh;
}

.aside {
  background: #001529;
  color: #fff;
}

.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 56px;
  padding: 0 18px;
  font-size: 18px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 1px;
}

.menu {
  border-right: none;
  background: transparent;
  --el-menu-text-color: #c0c4cc;
  --el-menu-hover-bg-color: #0b2a45;
  --el-menu-active-color: #ffffff;
}

.menu :deep(.el-menu-item.is-active) {
  background: #1890ff;
  color: #fff;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #eaecef;
}

.header .right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.user {
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  color: #303133;
}

.main {
  padding: 0;
  background: #f5f7fa;
  overflow: auto;
}
</style>
