<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <div class="login-title">
        <el-icon :size="28"><Connection /></el-icon>
        <h2>HorseUI 面板</h2>
      </div>
      <el-form :model="form" @submit.prevent="onSubmit">
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
        <el-form-item>
          <el-input
            v-model="form.code"
            placeholder="两步验证码（未启用可留空）"
            size="large"
            :prefix-icon="Key"
            maxlength="16"
            @keyup.enter="onSubmit"
          />
        </el-form-item>
        <el-button type="primary" size="large" class="login-btn" :loading="loading" @click="onSubmit">
          登 录
        </el-button>
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
const form = reactive({ username: '', password: '', code: '' })

async function onSubmit() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    const res = await auth.login(form.username, form.password, form.code.trim())
    if (res?.two_factor_required) {
      ElMessage.warning('面板已启用两步验证，请填写 6 位动态码或恢复码')
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
