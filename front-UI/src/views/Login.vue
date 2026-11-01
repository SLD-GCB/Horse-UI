<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <div class="login-title">
        <el-icon :size="28"><Connection /></el-icon>
        <h2>HorseUI 面板</h2>
      </div>
      <el-form v-if="step === 1" :model="form" @submit.prevent="onSubmit">
        <el-form-item>
          <el-input v-model="form.username" placeholder="用户名" size="large" :prefix-icon="User" />
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            size="large"
            show-password
            :prefix-icon="Lock"
            @keyup.enter="onSubmit"
          />
        </el-form-item>
        <el-button type="primary" size="large" class="login-btn" :loading="loading" @click="onSubmit">
          登 录
        </el-button>
      </el-form>

      <el-form v-else :model="twofa" @submit.prevent="onVerify">
        <div class="twofa-tip">请输入身份验证器中的 6 位动态码，或一次性恢复码</div>
        <el-form-item>
          <el-input
            v-model="twofa.code"
            placeholder="动态码 / 恢复码"
            size="large"
            :prefix-icon="Key"
            maxlength="16"
            @keyup.enter="onVerify"
          />
        </el-form-item>
        <el-button type="primary" size="large" class="login-btn" :loading="loading" @click="onVerify">
          验 证
        </el-button>
        <el-button link class="login-back" @click="backToPassword">返回重新输入密码</el-button>
      </el-form>
      <div class="tip">初始密码见服务启动日志（首次启动时随机生成）；登录后请及时修改</div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Key, Lock, User } from '@element-plus/icons-vue'
import { useAuthStore } from '@/store/auth'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const step = ref(1)
const challenge = ref('')
const form = reactive({ username: '', password: '' })
const twofa = reactive({ code: '' })

async function onSubmit() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    const res = await auth.login(form.username, form.password)
    if (res?.two_factor_required && res.challenge) {
      challenge.value = res.challenge
      twofa.code = ''
      step.value = 2
      ElMessage.info('请输入两步验证动态码')
      return
    }
    ElMessage.success('登录成功')
    router.push('/dashboard')
  } catch {
    /* 拦截器已提示 */
  } finally {
    loading.value = false
  }
}

async function onVerify() {
  if (!twofa.code.trim()) {
    ElMessage.warning('请输入动态码或恢复码')
    return
  }
  loading.value = true
  try {
    await auth.login2FA(challenge.value, twofa.code.trim())
    ElMessage.success('登录成功')
    router.push('/dashboard')
  } catch {
    /* 拦截器已提示 */
  } finally {
    loading.value = false
  }
}

function backToPassword() {
  step.value = 1
  challenge.value = ''
  twofa.code = ''
}
</script>

<style scoped>
.login-wrap {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #001529 0%, #1890ff 100%);
}

.login-card {
  width: 380px;
  padding: 8px 4px;
}

.login-title {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-bottom: 20px;
  color: #001529;
}

.login-title h2 {
  margin: 0;
  font-size: 20px;
}

.login-btn {
  width: 100%;
}

.tip {
  margin-top: 14px;
  font-size: 12px;
  color: #909399;
  text-align: center;
}

.twofa-tip {
  margin-bottom: 14px;
  font-size: 13px;
  color: #606266;
  line-height: 1.6;
}

.login-back {
  width: 100%;
  margin-top: 8px;
}
</style>
