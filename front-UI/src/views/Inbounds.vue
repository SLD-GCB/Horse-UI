<template>
  <div class="page">
    <div class="page-header">
      <div class="page-title">入站管理</div>
      <el-button type="primary" @click="openCreate">新增入站</el-button>
    </div>

    <el-table :data="rows" border stripe>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="tag" label="标识" min-width="130" />
      <el-table-column prop="type" label="协议" width="110" />
      <el-table-column label="端口" width="80">
        <template #default="{ row }">{{ row.port }}</template>
      </el-table-column>
      <el-table-column label="传输" width="110">
        <template #default="{ row }">{{ transportLabel(row) }}</template>
      </el-table-column>
      <el-table-column label="安全" width="150">
        <template #default="{ row }">
          <el-tag v-if="isReality(row)" size="small" type="warning">Reality</el-tag>
          <el-tag v-else-if="hasTLS(row)" size="small" type="success">TLS</el-tag>
          <el-tag v-else size="small" type="info">明文</el-tag>
          <span v-if="alpnLabel(row)" class="alpn-tag">{{ alpnLabel(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="client_count" label="用户数" width="90" />
      <el-table-column label="累计流量" width="130">
        <template #default="{ row }">
          {{ formatBytes((row.used_up || 0) + (row.used_down || 0)) }}
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" min-width="110" />
      <el-table-column label="启用" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enable ? 'success' : 'info'" size="small">{{ row.enable ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="延迟" width="110">
        <template #default="{ row }">
          <el-tag
            v-if="row.id in delays"
            size="small"
            :type="delays[row.id] < 0 ? 'danger' : delays[row.id] < 200 ? 'success' : delays[row.id] < 500 ? 'warning' : 'info'"
          >
            {{ delays[row.id] < 0 ? '失败' : delays[row.id] + ' ms' }}
          </el-tag>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :loading="testing[row.id]" @click="testDelay(row)">测速</el-button>
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="form.id ? '编辑入站' : '新增入站'" width="900px" top="4vh">
      <el-form :model="form" label-width="150px">
        <el-collapse v-model="activeSections">
          <!-- ================= 基础 ================= -->
          <el-collapse-item name="basic" title="基础">
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="协议类型">
                  <el-select v-model="form.type" style="width: 100%">
                    <el-option v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="标识 (tag)">
                  <el-input v-model="form.tag" placeholder="留空自动生成" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="监听地址">
                  <el-select
                    v-model="form.listen"
                    filterable
                    allow-create
                    default-first-option
                    style="width: 100%"
                    placeholder="监听的本机接口地址"
                  >
                    <el-option label=":: （IPv6 + IPv4 全部）" value="::" />
                    <el-option label="0.0.0.0 （仅 IPv4 全部）" value="0.0.0.0" />
                    <el-option
                      v-for="a in listenAddresses"
                      :key="a.value"
                      :label="a.label"
                      :value="a.value"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="端口">
                  <el-input-number v-model="form.port" :min="1" :max="65535" style="width: 100%" />
                  <div class="hint">
                    <el-button link size="small" @click="autoPort">自动分配可用端口</el-button>
                    <span v-if="portPool">
                      端口池 {{ portPool.range.start }}-{{ portPool.range.end }}
                    </span>
                  </div>
                  <div v-if="portPool && portPool.free && portPool.free.length" class="port-choices">
                    <el-tag
                      v-for="p in portPool.free.slice(0, 10)"
                      :key="p"
                      size="small"
                      :type="form.port === p ? 'primary' : 'info'"
                      class="port-chip"
                      @click="form.port = p"
                    >
                      {{ p }}
                    </el-tag>
                  </div>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="备注">
                  <el-input v-model="form.remark" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="启用 / 嗅探">
                  <el-switch v-model="form.enable" active-text="启用" />
                  <el-switch v-model="form.sniff" active-text="嗅探" style="margin-left: 16px" />
                </el-form-item>
              </el-col>
              <el-col v-if="supportsTFO" :span="12">
                <el-form-item label="TCP Fast Open">
                  <el-switch v-model="form.tfo" active-text="开启" />
                  <span class="hint">握手省一次 RTT，高延迟线路有感；需客户端也支持</span>
                </el-form-item>
              </el-col>
              <el-col v-if="!form.id" :span="24">
                <el-form-item label="批量创建">
                  <el-switch v-model="form.batch_enabled" active-text="按当前配置创建多个" />
                  <template v-if="form.batch_enabled">
                    <el-input-number
                      v-model="form.batch_count"
                      :min="2"
                      :max="100"
                      style="width: 110px; margin-left: 12px"
                    />
                    <span class="hint" style="display: inline">个，端口步长</span>
                    <el-input-number
                      v-model="form.batch_step"
                      :min="1"
                      :max="1000"
                      style="width: 110px; margin-left: 8px"
                    />
                  </template>
                  <div v-if="form.batch_enabled" class="hint">
                    标识可用 <code>{n}</code> 占位（如 node-{n}），留空按「协议-端口」自动生成。
                    将创建：{{ batchPreview }}
                  </div>
                </el-form-item>
              </el-col>
            </el-row>
          </el-collapse-item>

          <!-- ================= 协议选项 ================= -->
          <el-collapse-item name="proto" title="协议选项">
            <!-- 弱网优化引导：线路差时先点这里，再按提示补真实带宽 -->
            <el-alert v-if="supportsWeakPreset" type="success" :closable="false" show-icon class="weak-preset">
              <template #title>
                <span class="weak-preset-title">线路质量差（高延迟 / 丢包 / 抖动）？</span>
                <el-button link type="primary" size="small" @click="applyWeakNetPreset">
                  一键弱网预置
                </el-button>
              </template>
              <div class="hint">{{ weakPresetHint }}</div>
            </el-alert>

            <template v-if="form.type === 'shadowsocks'">
              <el-form-item label="加密方法">
                <el-select v-model="form.method" style="width: 100%">
                  <el-option v-for="m in presets.ss_methods" :key="m" :label="m" :value="m" />
                </el-select>
              </el-form-item>
              <el-form-item label="服务端密码">
                <el-input v-model="form.server_password">
                  <template #append><el-button @click="genSSPassword">生成</el-button></template>
                </el-input>
              </el-form-item>
              <el-form-item label="网络">
                <el-select v-model="form.ss_network" style="width: 100%">
                  <el-option v-for="n in presets.ss_networks" :key="n || 'both'" :label="n === '' ? 'tcp+udp（默认）' : n" :value="n" />
                </el-select>
              </el-form-item>
              <el-form-item label="托管模式">
                <el-switch v-model="form.ss_managed" active-text="managed（由 SSM API 动态管理用户）" />
              </el-form-item>
            </template>

            <template v-else-if="form.type === 'hysteria2'">
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="上行 Mbps">
                    <el-input-number v-model="form.up_mbps" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="下行 Mbps">
                    <el-input-number v-model="form.down_mbps" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="混淆类型">
                    <el-select v-model="form.obfs_type" style="width: 100%">
                      <el-option v-for="t in presets.obfs_types" :key="t" :label="t" :value="t" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="混淆密码">
                    <el-input v-model="form.obfs_password" placeholder="留空则不启用混淆" />
                  </el-form-item>
                </el-col>
                <el-col v-if="form.obfs_type === 'gecko'" :span="12">
                  <el-form-item label="gecko 最小包">
                    <el-input-number v-model="form.hy2_obfs_min_packet" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col v-if="form.obfs_type === 'gecko'" :span="12">
                  <el-form-item label="gecko 最大包">
                    <el-input-number v-model="form.hy2_obfs_max_packet" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="伪装地址">
                <el-input v-model="form.masquerade" placeholder="留空返回 404；可填 https://www.example.com 或 file:///var/www" />
              </el-form-item>
              <el-form-item label="忽略客户端带宽">
                <el-switch v-model="form.hy2_ignore_bw" />
              </el-form-item>
              <div class="hint weak-modes">
                <div class="weak-modes-title">弱网要「丢包不减速」，按 A 或 B 配，别用 C：</div>
                <div>
                  <b>A 服务端 Brutal（最省事，客户端无需配置）</b>：上下行<b>填写 VPS 真实带宽</b> + 本开关<b>开</b>
                  → 服务端按该速率硬推、丢包不减速，并拒绝客户端改用 BBR。
                </div>
                <div>
                  <b>B 客户端 Brutal</b>：上下行<b>留空</b> + 本开关<b>关</b>
                  → 由客户端自己填带宽启用 Brutal（需要会配客户端）。
                </div>
                <div>
                  <b>C 纯 BBR（丢包会减速）</b>：上下行<b>留空</b> + 本开关<b>开</b>
                  → 指示客户端改用 BBR，适合防滥用。
                </div>
                <div class="weak-modes-warn">
                  带宽务必填 VPS 的真实值：填得过高会加剧丢包，反而更慢。
                </div>
              </div>
              <el-form-item label="BBR 配置">
                <el-select v-model="form.hy2_bbr_profile" clearable style="width: 240px">
                  <el-option v-for="p in presets.bbr_profiles" :key="p" :label="p" :value="p" />
                </el-select>
                <span class="hint">仅当客户端走 BBR（模式 C）时生效；用 Brutal（A / B）不用设</span>
              </el-form-item>
              <el-form-item label="Brutal 调试">
                <el-switch v-model="form.hy2_brutal_debug" />
              </el-form-item>
            </template>

            <template v-else-if="form.type === 'tuic'">
              <el-form-item label="拥塞控制">
                <el-select v-model="form.congestion_control" style="width: 100%">
                  <el-option v-for="c in presets.congestion" :key="c" :label="c" :value="c" />
                </el-select>
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="认证超时">
                    <el-input v-model="form.tuic_auth_timeout" placeholder="3s" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="心跳间隔">
                    <el-input v-model="form.tuic_heartbeat" placeholder="10s" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="0-RTT 握手">
                <el-switch v-model="form.tuic_zero_rtt" />
                <span class="hint">不建议开启，存在重放攻击风险</span>
              </el-form-item>
            </template>

            <template v-else>
              <el-alert
                type="info"
                :closable="false"
                show-icon
                :title="`${form.type.toUpperCase()} 的协议参数由「传输层 / TLS / Reality」及用户级配置决定`"
              />
            </template>

            <!-- QUIC 调优：hysteria2 / tuic 共用，全部留空 = 内核默认 -->
            <template v-if="supportsQUIC">
              <el-divider content-position="left">QUIC 调优（弱网）</el-divider>
              <el-row :gutter="12">
                <el-col :span="8">
                  <el-form-item label="初始包大小">
                    <el-input-number v-model="form.quic_initial_packet" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="8">
                  <el-form-item label="空闲超时">
                    <el-input v-model="form.quic_idle_timeout" placeholder="如 30s" />
                  </el-form-item>
                </el-col>
                <el-col :span="8">
                  <el-form-item label="保活周期">
                    <el-input v-model="form.quic_keepalive" placeholder="如 15s" />
                  </el-form-item>
                </el-col>
                <el-col :span="8">
                  <el-form-item label="流接收窗口">
                    <el-input-number v-model="form.quic_stream_window" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="8">
                  <el-form-item label="连接接收窗口">
                    <el-input-number v-model="form.quic_conn_window" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="8">
                  <el-form-item label="最大并发流">
                    <el-input-number v-model="form.quic_max_streams" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="禁用路径 MTU 发现">
                <el-switch v-model="form.quic_disable_pmtud" />
                <span class="hint">中间设备丢 PMTU 探测包导致「能连上但传不动」时打开</span>
              </el-form-item>
              <div class="hint">
                弱网/高丢包可加大「流接收窗口 / 连接接收窗口」提升 QUIC 吞吐；留空或 0 表示用内核默认。
                改完要让内核进程重启才生效。
              </div>
            </template>
          </el-collapse-item>

          <!-- ================= 传输层 ================= -->
          <el-collapse-item v-if="supportsTransport" name="transport" title="传输层">
            <el-form-item label="传输类型">
              <el-select v-model="form.transport_type" style="width: 100%">
                <el-option label="TCP（默认，无额外传输）" value="" />
                <el-option v-for="t in ['ws', 'grpc', 'http', 'httpupgrade']" :key="t" :label="t" :value="t" />
              </el-select>
            </el-form-item>

            <template v-if="form.transport_type === 'ws'">
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="路径 (path)">
                    <el-input v-model="form.transport_path" placeholder="/ws" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="Host 头">
                    <el-input v-model="form.transport_host" placeholder="可选" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="最大早期数据">
                    <el-input-number v-model="form.ws_max_early_data" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="早期数据头名">
                    <el-input v-model="form.ws_early_data_header" placeholder="Sec-WebSocket-Protocol" />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <template v-else-if="form.transport_type === 'grpc'">
              <el-form-item label="服务名">
                <el-input v-model="form.transport_service" placeholder="grpc" />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="空闲超时">
                    <el-input v-model="form.grpc_idle_timeout" placeholder="15s" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="Ping 超时">
                    <el-input v-model="form.grpc_ping_timeout" placeholder="15s" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="允许无流连接">
                <el-switch v-model="form.grpc_permit_without_stream" />
              </el-form-item>
            </template>

            <template v-else-if="form.transport_type === 'http'">
              <el-form-item label="主机名列表">
                <el-input v-model="form.http_hosts_text" placeholder="多个用逗号分隔，如 a.com,b.com" />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="路径 (path)">
                    <el-input v-model="form.transport_path" placeholder="/" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="请求方法">
                    <el-select v-model="form.http_method" style="width: 100%">
                      <el-option v-for="m in presets.http_methods" :key="m" :label="m" :value="m" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <template v-else-if="form.transport_type === 'httpupgrade'">
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="路径 (path)">
                    <el-input v-model="form.transport_path" placeholder="/up" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="Host">
                    <el-input v-model="form.transport_host" />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <el-form-item v-if="form.transport_type" label="额外 Headers">
              <el-input
                v-model="form.transport_headers"
                type="textarea"
                :rows="3"
                placeholder='JSON 对象，可选，例如 {"X-Custom":"1"}'
              />
            </el-form-item>
          </el-collapse-item>

          <!-- ================= TLS ================= -->
          <el-collapse-item v-if="supportsTLS" name="tls" title="TLS">
            <el-form-item label="启用 TLS">
              <el-switch :model-value="tlsActive" disabled />
              <span v-if="forceTLS" class="hint">该协议强制启用 TLS（不可关闭）</span>
              <span v-if="form.reality_enabled" class="hint">Reality 已包含 TLS</span>
            </el-form-item>

            <template v-if="tlsActive">
              <el-form-item label="服务名 (SNI)">
                <el-input v-model="form.server_name" :placeholder="form.reality_enabled ? form.reality_server : '如 example.com'" />
              </el-form-item>

              <el-form-item label="ALPN">
                <el-select
                  v-model="form.alpn"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  style="width: 100%"
                  placeholder="可选，如 h2 / http/1.1 / h3"
                >
                  <el-option label="h2" value="h2" />
                  <el-option label="http/1.1" value="http/1.1" />
                  <el-option label="h3" value="h3" />
                </el-select>
                <div class="hint">
                  <el-button link size="small" @click="form.alpn = ['h2', 'http/1.1']">h2+http/1.1</el-button>
                  <el-button link size="small" @click="form.alpn = ['h3']">h3</el-button>
                </div>
              </el-form-item>

              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="最低版本">
                    <el-select v-model="form.tls_min" clearable style="width: 100%" placeholder="默认">
                      <el-option v-for="v in presets.tls_versions" :key="v" :label="v" :value="v" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="最高版本">
                    <el-select v-model="form.tls_max" clearable style="width: 100%" placeholder="默认">
                      <el-option v-for="v in presets.tls_versions" :key="v" :label="v" :value="v" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>

              <el-form-item label="加密套件">
                <el-select v-model="form.cipher_suites" multiple clearable style="width: 100%" placeholder="默认（留空）">
                  <el-option v-for="c in presets.cipher_suites" :key="c" :label="c" :value="c" />
                </el-select>
              </el-form-item>

              <template v-if="!form.reality_enabled">
                <el-form-item label="证书来源">
                  <el-radio-group v-model="certSource">
                    <el-radio-button value="manual">手动路径</el-radio-button>
                    <el-radio-button value="panel">面板证书 (ACME)</el-radio-button>
                  </el-radio-group>
                </el-form-item>
                <template v-if="certSource === 'panel'">
                  <el-form-item label="选择证书">
                    <el-select
                      v-model="form.cert_id"
                      clearable
                      style="width: 100%"
                      placeholder="在「证书管理」中申请或生成"
                      @change="onCertChange"
                    >
                      <el-option v-for="ct in certs" :key="ct.id" :label="certLabel(ct)" :value="ct.id" />
                    </el-select>
                    <div class="hint">
                      自签证书：客户端需勾选「允许不安全」，或把该证书导入为受信任根。
                    </div>
                  </el-form-item>
                </template>
                <template v-else>
                  <el-row :gutter="12">
                    <el-col :span="12">
                      <el-form-item label="证书路径">
                        <el-input v-model="form.cert_path" placeholder="fullchain.pem" />
                      </el-form-item>
                    </el-col>
                    <el-col :span="12">
                      <el-form-item label="私钥路径">
                        <el-input v-model="form.key_path" placeholder="privkey.pem" />
                      </el-form-item>
                    </el-col>
                  </el-row>
                </template>

                <el-form-item label="客户端跳验证">
                  <el-switch v-model="form.insecure" />
                  <span class="hint">
                    自签证书必须开启；开启后下发的订阅会带上 allowInsecure / skip-cert-verify / insecure
                  </span>
                </el-form-item>

                <el-divider content-position="left">客户端证书校验（mTLS，可选）</el-divider>
                <el-form-item label="客户端认证">
                  <el-select v-model="form.client_auth" style="width: 100%">
                    <el-option v-for="o in presets.client_auth" :key="o.value" :label="o.label" :value="o.value" />
                  </el-select>
                </el-form-item>
                <el-form-item label="CA 证书路径">
                  <el-input v-model="form.client_cert_path" placeholder="校验客户端证书时填写 CA 文件路径" />
                </el-form-item>
                <el-form-item label="证书公钥 SHA256">
                  <el-input
                    v-model="form.client_cert_sha256"
                    type="textarea"
                    :rows="2"
                    placeholder="可选，每行一个 64 位十六进制指纹"
                  />
                </el-form-item>
              </template>
            </template>
          </el-collapse-item>

          <!-- ================= Reality ================= -->
          <el-collapse-item v-if="supportsReality" name="reality" title="Reality">
            <el-form-item label="启用 Reality">
              <el-switch v-model="form.reality_enabled" />
            </el-form-item>
            <template v-if="form.reality_enabled">
              <el-row :gutter="12">
                <el-col :span="14">
                  <el-form-item label="握手目标">
                    <el-input v-model="form.reality_server" placeholder="www.microsoft.com" />
                  </el-form-item>
                </el-col>
                <el-col :span="10">
                  <el-form-item label="握手端口">
                    <el-input-number v-model="form.reality_port" :min="1" :max="65535" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="私钥">
                <el-input v-model="form.reality_private_key">
                  <template #append><el-button @click="genReality">生成</el-button></template>
                </el-input>
              </el-form-item>
              <el-form-item label="公钥">
                <el-input v-model="realityPublicKey" readonly placeholder="由私钥自动推导，填到客户端" />
                <div class="hint"><el-button link size="small" @click="copy(realityPublicKey)">复制公钥</el-button></div>
              </el-form-item>
              <el-form-item label="Short ID">
                <el-input v-model="form.reality_short_ids" type="textarea" :rows="2" placeholder="可多个，逗号或换行分隔" />
                <div class="hint"><el-button link size="small" @click="genShortID">生成一个</el-button></div>
              </el-form-item>
              <el-form-item label="时间差容忍">
                <el-select v-model="form.reality_max_diff" clearable style="width: 240px" placeholder="默认 1m">
                  <el-option v-for="d in presets.reality_maxdiff" :key="d || 'default'" :label="d === '' ? '默认' : d" :value="d" />
                </el-select>
              </el-form-item>
            </template>
          </el-collapse-item>

          <!-- ================= 高级 ================= -->
          <el-collapse-item name="advanced" title="高级（自定义 JSON）">
            <el-form-item label="附加字段">
              <el-input
                v-model="form.extra_raw"
                type="textarea"
                :rows="4"
                placeholder='直接合并进入站对象的 JSON，例如 {"some_option": true}'
              />
              <div class="hint">受保护字段 type / tag / listen / listen_port 不会被覆盖。</div>
            </el-form-item>
            <el-form-item label="配置预览">
              <el-input :model-value="preview" type="textarea" :rows="12" readonly />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>

      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存并应用</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { Certificate, Inbound, PortPool, Presets } from '@/api/types'
import { formatBytes } from '@/utils/format'

const rows = ref<Inbound[]>([])
const delays = reactive<Record<number, number>>({})
const testing = reactive<Record<number, boolean>>({})
const certs = ref<Certificate[]>([])
const portPool = ref<PortPool | null>(null)
const dialog = ref(false)
const saving = ref(false)
const realityPublicKey = ref('')
const activeSections = ref<string[]>(['basic', 'proto'])

const presets = ref<Presets>({
  ss_methods: [],
  ss2022_methods: [],
  inbound_types: [],
  transports: [],
  congestion: [],
  obfs_types: [],
  tls_versions: [],
  alpn_presets: [],
  cipher_suites: [],
  client_auth: [],
  ss_networks: [],
  http_methods: [],
  bbr_profiles: [],
  reality_maxdiff: [],
})

const typeOptions = [
  { label: 'VLESS', value: 'vless' },
  { label: 'VMess', value: 'vmess' },
  { label: 'Trojan', value: 'trojan' },
  { label: 'Shadowsocks', value: 'shadowsocks' },
  { label: 'TUIC', value: 'tuic' },
  { label: 'Hysteria2', value: 'hysteria2' },
  { label: 'HTTP (H2)', value: 'http' },
]

function defaultForm() {
  return {
    id: 0,
    tag: '',
    type: 'vless',
    remark: '',
    enable: true,
    listen: '::',
    port: 443,
    sniff: true,

    // 协议选项
    method: '2022-blake3-aes-128-gcm',
    server_password: '',
    ss_network: '',
    ss_managed: false,
    up_mbps: 0,
    down_mbps: 0,
    obfs_type: 'salamander',
    obfs_password: '',
    congestion_control: 'bbr',
    masquerade: '',
    hy2_ignore_bw: false,
    hy2_bbr_profile: '',
    hy2_brutal_debug: false,
    hy2_obfs_min_packet: 0,
    hy2_obfs_max_packet: 0,
    tuic_auth_timeout: '',
    tuic_heartbeat: '',
    tuic_zero_rtt: false,

    // 弱网：Listen 层与 QUIC 层调优（留空/0 = 内核默认）
    tfo: false,
    quic_initial_packet: 0,
    quic_disable_pmtud: false,
    quic_idle_timeout: '',
    quic_keepalive: '',
    quic_stream_window: 0,
    quic_conn_window: 0,
    quic_max_streams: 0,

    // 传输
    transport_type: '',
    transport_path: '',
    transport_host: '',
    transport_service: '',
    transport_headers: '',
    ws_max_early_data: 0,
    ws_early_data_header: '',
    grpc_idle_timeout: '',
    grpc_ping_timeout: '',
    grpc_permit_without_stream: false,
    http_hosts_text: '',
    http_method: 'GET',

    // TLS
    tls_enabled: false,
    server_name: '',
    alpn: [] as string[],
    tls_min: '',
    tls_max: '',
    cipher_suites: [] as string[],
    client_auth: '',
    client_cert_path: '',
    client_cert_sha256: '',
    cert_source: 'panel' as 'manual' | 'panel',
    cert_id: null as number | null,
    insecure: false,
    cert_path: '',
    key_path: '',

    // Reality
    reality_enabled: false,
    reality_server: 'www.microsoft.com',
    reality_port: 443,
    reality_private_key: '',
    reality_short_ids: '',
    reality_max_diff: '',

    // 其它
    extra_raw: '',

    // 批量创建（仅新增时生效）
    batch_enabled: false,
    batch_count: 5,
    batch_step: 1,
  }
}

const form = reactive(defaultForm())

// 除 Shadowsocks（自带加密）外，所有协议强制启用 TLS。
const forceTLS = computed(() => !!form.type && form.type !== 'shadowsocks')
const supportsTLS = computed(() => form.type !== 'shadowsocks')
const supportsReality = computed(() => ['vless', 'trojan', 'vmess'].includes(form.type))
const supportsTransport = computed(() => ['vless', 'vmess', 'trojan'].includes(form.type))
const tlsActive = computed(() => form.reality_enabled || form.tls_enabled || forceTLS.value)
// QUIC 类入站（hysteria2 / tuic）走用户态 UDP，BBR 对它们无效，真正有用的是 QUIC 参数与系统 UDP 缓冲。
const supportsQUIC = computed(() => ['hysteria2', 'tuic'].includes(form.type))
// TCP Fast Open 是 Listen 层选项，对 QUIC 类入站没有意义。
const supportsTFO = computed(() => !!form.type && !supportsQUIC.value)
// 提供「一键弱网预置」的协议。
const supportsWeakPreset = computed(() =>
  ['hysteria2', 'tuic', 'vless', 'vmess', 'trojan'].includes(form.type),
)

/** 一键弱网预置会做什么，随协议变化。 */
const weakPresetHint = computed(() => {
  if (form.type === 'hysteria2') {
    return '将开启「服务端 Brutal」：忽略客户端带宽 + 混淆 salamander（密码自动生成）。之后请把上面的上下行带宽改成 VPS 的真实带宽。'
  }
  if (form.type === 'tuic') {
    return '将设置拥塞控制 bbr + 心跳 10s。'
  }
  return '将开启 TCP Fast Open，握手少一次往返（需客户端也支持）。'
})
const certSource = computed({
  get: () => form.cert_source,
  set: (v: 'manual' | 'panel') => {
    form.cert_source = v
    if (v === 'panel') {
      form.cert_path = ''
      form.key_path = ''
    } else {
      form.cert_id = null
    }
  },
})

const preview = computed(() => JSON.stringify(buildPayload(), null, 2))

/** 批量创建预览：前几个标识 + 端口区间，和后端生成规则保持一致。 */
const batchPreview = computed(() => {
  const count = Math.max(2, Math.min(form.batch_count || 2, 100))
  const step = Math.max(1, form.batch_step || 1)
  const baseTag = form.tag.trim()
  const tagAt = (i: number) => {
    if (!baseTag) {
      const port = form.port + (i - 1) * step
      return `${form.type}-${port}`
    }
    return baseTag.includes('{n}') ? baseTag.replaceAll('{n}', String(i)) : `${baseTag}-${i}`
  }
  const shown = [1, 2, 3].slice(0, count).map(tagAt)
  const ellipsis = count > 3 ? ' … ' : ''
  const lastPort = form.port + (count - 1) * step
  return `${shown.join('、')}${ellipsis}（端口 ${form.port}–${lastPort}）`
})

/** 本机可用监听地址（网卡地址 + 全部地址） */
const listenAddresses = computed(() => {
  const out: { label: string; value: string }[] = []
  for (const it of portPool.value?.interfaces || []) {
    for (const a of it.addresses) {
      out.push({ label: `${a}（${it.name}${it.loopback ? ' 回环' : ''}）`, value: a })
    }
  }
  return out
})

/** 从端口池里取一个可用端口 */
function autoPort() {
  const pool = portPool.value
  if (!pool || !pool.free || !pool.free.length) return
  const p = pool.suggested || pool.free[0]
  form.port = p
  // 用掉一个就顺延，避免连续新建拿到同一个
  pool.free = pool.free.filter((x) => x !== p)
  pool.suggested = pool.free[0] || p
}

async function loadPortPool() {
  try {
    portPool.value = await api.get<PortPool>('/api/ports')
  } catch {
    portPool.value = null
  }
}

/* ---------------- 表格展示 ---------------- */

function asObj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
}

function isReality(row: Inbound) {
  const r = asObj(row.reality)
  return !!r && Object.keys(r).length > 0
}

function hasTLS(row: Inbound) {
  const t = asObj(row.tls)
  return !!t && t.enabled !== false
}

function transportLabel(row: Inbound) {
  const tr = asObj(row.transport)
  return tr && tr.type ? String(tr.type) : 'tcp'
}

function alpnLabel(row: Inbound) {
  const t = asObj(row.tls)
  const alpn = t?.alpn
  return Array.isArray(alpn) && alpn.length ? alpn.join(',') : ''
}

/* ---------------- 构建配置 ---------------- */

function clean(o: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(o)) {
    if (v === undefined || v === null || v === '') continue
    if (Array.isArray(v) && v.length === 0) continue
    if (typeof v === 'object' && !Array.isArray(v) && Object.keys(v as object).length === 0) continue
    out[k] = v
  }
  return out
}

function parseRawJSON(text: string): Record<string, unknown> | null {
  const s = text.trim()
  if (!s) return null
  try {
    const v = JSON.parse(s)
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function splitList(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function buildTLS(): Record<string, unknown> | null {
  if (!tlsActive.value) return null
  const base: Record<string, unknown> = {
    enabled: true,
    server_name: form.server_name || undefined,
    alpn: form.alpn.length ? form.alpn : undefined,
    min_version: form.tls_min || undefined,
    max_version: form.tls_max || undefined,
    cipher_suites: form.cipher_suites.length ? form.cipher_suites : undefined,
  }
  // Reality 由后端根据 reality 字段注入 tls.reality，这里不重复
  if (!form.reality_enabled && form.cert_source === 'manual') {
    base.certificate_path = form.cert_path || undefined
    base.key_path = form.key_path || undefined
  }
  if (!form.reality_enabled) {
    base.client_authentication = form.client_auth || undefined
    if (form.client_auth) {
      base.client_certificate_path = form.client_cert_path || undefined
      const sha = splitList(form.client_cert_sha256)
      base.client_certificate_public_key_sha256 = sha.length ? sha : undefined
    }
  }
  return clean(base)
}

function buildReality(): Record<string, unknown> | null {
  if (!form.reality_enabled) return null
  return clean({
    handshake: { server: form.reality_server, server_port: form.reality_port },
    private_key: form.reality_private_key || undefined,
    short_id: splitList(form.reality_short_ids),
    max_time_difference: form.reality_max_diff || undefined,
  })
}

function buildTransport(): Record<string, unknown> | null {
  const t = form.transport_type
  if (!t || !supportsTransport.value) return null
  const extraHeaders = parseRawJSON(form.transport_headers) || undefined
  switch (t) {
    case 'ws':
      return clean({
        type: 'ws',
        path: form.transport_path || undefined,
        headers: form.transport_host ? { Host: form.transport_host, ...(extraHeaders || {}) } : extraHeaders,
        max_early_data: form.ws_max_early_data || undefined,
        early_data_header_name: form.ws_early_data_header || undefined,
      })
    case 'grpc':
      return clean({
        type: 'grpc',
        service_name: form.transport_service || undefined,
        idle_timeout: form.grpc_idle_timeout || undefined,
        ping_timeout: form.grpc_ping_timeout || undefined,
        permit_without_stream: form.grpc_permit_without_stream || undefined,
      })
    case 'http':
      return clean({
        type: 'http',
        host: splitList(form.http_hosts_text),
        path: form.transport_path || undefined,
        method: form.http_method || undefined,
        headers: extraHeaders,
      })
    case 'httpupgrade':
      return clean({
        type: 'httpupgrade',
        path: form.transport_path || undefined,
        host: form.transport_host || undefined,
        headers: extraHeaders,
      })
    default:
      return null
  }
}

/** 已知的结构化选项之外、用户手写的附加字段 */
const KNOWN_EXTRA_KEYS = [
  'network', 'managed',
  'auth_timeout', 'heartbeat', 'zero_rtt_handshake',
  'ignore_client_bandwidth', 'bbr_profile', 'brutal_debug',
  // 弱网：QUIC 调优 + Listen 层 + gecko 混淆
  'initial_packet_size', 'disable_path_mtu_discovery', 'idle_timeout', 'keep_alive_period',
  'stream_receive_window', 'connection_receive_window', 'max_concurrent_streams',
  'tcp_fast_open', 'obfs',
]

function buildExtra(): Record<string, unknown> | null {
  const e: Record<string, unknown> = {}
  if (form.type === 'shadowsocks') {
    if (form.ss_network) e.network = form.ss_network
    if (form.ss_managed) e.managed = true
  }
  if (form.type === 'tuic') {
    if (form.tuic_auth_timeout) e.auth_timeout = form.tuic_auth_timeout
    if (form.tuic_heartbeat) e.heartbeat = form.tuic_heartbeat
    if (form.tuic_zero_rtt) e.zero_rtt_handshake = true
  }
  if (form.type === 'hysteria2') {
    if (form.hy2_ignore_bw) e.ignore_client_bandwidth = true
    if (form.hy2_bbr_profile) e.bbr_profile = form.hy2_bbr_profile
    if (form.hy2_brutal_debug) e.brutal_debug = true
    // gecko 的包大小只能从 obfs 对象里给，这里整体覆盖（与后端从 obfs_type/obfs_password 生成的一致）。
    if (form.obfs_type === 'gecko' && form.obfs_password) {
      const obfs: Record<string, unknown> = { type: 'gecko', password: form.obfs_password }
      if (form.hy2_obfs_min_packet > 0) obfs.min_packet_size = form.hy2_obfs_min_packet
      if (form.hy2_obfs_max_packet > 0) obfs.max_packet_size = form.hy2_obfs_max_packet
      e.obfs = obfs
    }
  }
  // QUIC 调优（hysteria2 / tuic 共用）
  if (supportsQUIC.value) {
    if (form.quic_initial_packet > 0) e.initial_packet_size = form.quic_initial_packet
    if (form.quic_disable_pmtud) e.disable_path_mtu_discovery = true
    if (form.quic_idle_timeout) e.idle_timeout = form.quic_idle_timeout
    if (form.quic_keepalive) e.keep_alive_period = form.quic_keepalive
    if (form.quic_stream_window > 0) e.stream_receive_window = form.quic_stream_window
    if (form.quic_conn_window > 0) e.connection_receive_window = form.quic_conn_window
    if (form.quic_max_streams > 0) e.max_concurrent_streams = form.quic_max_streams
  }
  // Listen 层：TCP Fast Open
  if (supportsTFO.value && form.tfo) e.tcp_fast_open = true
  const raw = parseRawJSON(form.extra_raw)
  if (raw) Object.assign(e, raw)
  return Object.keys(e).length ? e : null
}

function buildPayload(): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    id: form.id,
    tag: form.tag,
    type: form.type,
    remark: form.remark,
    enable: form.enable,
    listen: form.listen,
    port: form.port,
    sniff: form.sniff,
    method: form.method,
    server_password: form.server_password,
    up_mbps: form.up_mbps,
    down_mbps: form.down_mbps,
    obfs_type: form.obfs_type,
    obfs_password: form.obfs_password,
    congestion_control: form.congestion_control,
    masquerade: form.masquerade,
    cert_id: form.cert_source === 'panel' ? form.cert_id : null,
    insecure: form.insecure,
    tls: buildTLS() ?? '',
    reality: buildReality() ?? '',
    transport: buildTransport() ?? '',
    extra: buildExtra() ?? '',
  }
  return payload
}

/* ---------------- 回填表单 ---------------- */

async function openCreate() {
  Object.assign(form, defaultForm())
  realityPublicKey.value = ''
  activeSections.value = ['basic', 'proto']
  // 新建时自动从本机可用端口里分配一个
  await loadPortPool()
  form.listen = '::'
  autoPort()
  // 强制 TLS 的协议：默认选中面板证书（优先自签），省去手选
  if (form.type !== 'shadowsocks' && certs.value.length) {
    const pick = certs.value.find((c) => c.kind === 'selfsigned') || certs.value[0]
    form.cert_source = 'panel'
    form.cert_id = pick.id
    onCertChange(pick.id)
  }
  dialog.value = true
}

async function openEdit(row: Inbound) {
  Object.assign(form, defaultForm())
  form.id = row.id
  form.tag = row.tag
  form.type = row.type
  form.remark = row.remark
  form.enable = row.enable
  form.listen = row.listen
  form.port = row.port
  form.sniff = row.sniff
  form.method = row.method || '2022-blake3-aes-128-gcm'
  form.server_password = row.server_password
  form.up_mbps = row.up_mbps
  form.down_mbps = row.down_mbps
  form.obfs_type = row.obfs_type || 'salamander'
  form.obfs_password = row.obfs_password
  form.congestion_control = row.congestion_control || 'bbr'
  form.masquerade = row.masquerade

  // ---- TLS ----
  const tls = asObj(row.tls)
  if (tls) {
    form.tls_enabled = tls.enabled !== false
    form.server_name = (tls.server_name as string) || ''
    form.alpn = Array.isArray(tls.alpn) ? (tls.alpn as string[]) : []
    form.tls_min = (tls.min_version as string) || ''
    form.tls_max = (tls.max_version as string) || ''
    form.cipher_suites = Array.isArray(tls.cipher_suites) ? (tls.cipher_suites as string[]) : []
    form.client_auth = (tls.client_authentication as string) || ''
    form.client_cert_path = (tls.client_certificate_path as string) || ''
    const sha = tls.client_certificate_public_key_sha256
    form.client_cert_sha256 = Array.isArray(sha) ? (sha as string[]).join('\n') : ''
    form.cert_path = (tls.certificate_path as string) || ''
    form.key_path = (tls.key_path as string) || ''
  }
  if (row.cert_id) {
    form.cert_id = row.cert_id
    form.cert_source = 'panel'
  }
  form.insecure = !!row.insecure

  // ---- Reality ----
  const reality = asObj(row.reality)
  if (reality) {
    form.reality_enabled = true
    const hs = asObj(reality.handshake) || {}
    form.reality_server = (hs.server as string) || ''
    form.reality_port = (hs.server_port as number) || 443
    form.reality_private_key = (reality.private_key as string) || ''
    form.reality_short_ids = (Array.isArray(reality.short_id) ? (reality.short_id as string[]) : []).join(', ')
    form.reality_max_diff = (reality.max_time_difference as string) || ''
    if (form.reality_private_key) derivePublicKey()
  }

  // ---- 传输 ----
  const tr = asObj(row.transport)
  if (tr) {
    form.transport_type = (tr.type as string) || ''
    form.transport_path = (tr.path as string) || ''
    form.transport_service = (tr.service_name as string) || ''
    form.ws_max_early_data = (tr.max_early_data as number) || 0
    form.ws_early_data_header = (tr.early_data_header_name as string) || ''
    form.grpc_idle_timeout = (tr.idle_timeout as string) || ''
    form.grpc_ping_timeout = (tr.ping_timeout as string) || ''
    form.grpc_permit_without_stream = !!tr.permit_without_stream
    form.http_method = (tr.method as string) || 'GET'

    const headers = { ...(asObj(tr.headers) || {}) }
    const hostHeader = headers.Host
    delete headers.Host
    form.transport_headers = Object.keys(headers).length ? JSON.stringify(headers, null, 2) : ''

    if (tr.type === 'ws' || tr.type === 'httpupgrade') {
      form.transport_host = (hostHeader as string) || (typeof tr.host === 'string' ? tr.host : '')
    } else if (tr.type === 'http') {
      form.http_hosts_text = Array.isArray(tr.host) ? (tr.host as string[]).join(',') : ''
    }
  }

  // ---- 协议附加字段 ----
  const extra = { ...(asObj(row.extra) || {}) }
  if (extra.network !== undefined) {
    form.ss_network = String(extra.network)
    delete extra.network
  }
  if (extra.managed !== undefined) {
    form.ss_managed = !!extra.managed
    delete extra.managed
  }
  if (extra.auth_timeout !== undefined) {
    form.tuic_auth_timeout = String(extra.auth_timeout)
    delete extra.auth_timeout
  }
  if (extra.heartbeat !== undefined) {
    form.tuic_heartbeat = String(extra.heartbeat)
    delete extra.heartbeat
  }
  if (extra.zero_rtt_handshake !== undefined) {
    form.tuic_zero_rtt = !!extra.zero_rtt_handshake
    delete extra.zero_rtt_handshake
  }
  if (extra.ignore_client_bandwidth !== undefined) {
    form.hy2_ignore_bw = !!extra.ignore_client_bandwidth
    delete extra.ignore_client_bandwidth
  }
  if (extra.bbr_profile !== undefined) {
    form.hy2_bbr_profile = String(extra.bbr_profile)
    delete extra.bbr_profile
  }
  if (extra.brutal_debug !== undefined) {
    form.hy2_brutal_debug = !!extra.brutal_debug
    delete extra.brutal_debug
  }

  // ---- 弱网调优字段回填（顺带从 extra 里剔除，避免重复落进 extra_raw） ----
  const takeNum = (key: string): number => {
    if (extra[key] === undefined) return 0
    const v = Number(extra[key])
    delete extra[key]
    return Number.isFinite(v) ? v : 0
  }
  const takeStr = (key: string): string => {
    if (extra[key] === undefined) return ''
    const v = String(extra[key])
    delete extra[key]
    return v
  }
  const takeBool = (key: string): boolean => {
    if (extra[key] === undefined) return false
    const v = !!extra[key]
    delete extra[key]
    return v
  }
  const obfsObj = asObj(extra.obfs)
  if (obfsObj && obfsObj.type === 'gecko') {
    form.obfs_type = 'gecko'
    if (obfsObj.password !== undefined) form.obfs_password = String(obfsObj.password)
    form.hy2_obfs_min_packet = Number(obfsObj.min_packet_size) || 0
    form.hy2_obfs_max_packet = Number(obfsObj.max_packet_size) || 0
  }
  delete extra.obfs
  form.quic_initial_packet = takeNum('initial_packet_size')
  form.quic_idle_timeout = takeStr('idle_timeout')
  form.quic_keepalive = takeStr('keep_alive_period')
  form.quic_stream_window = takeNum('stream_receive_window')
  form.quic_conn_window = takeNum('connection_receive_window')
  form.quic_max_streams = takeNum('max_concurrent_streams')
  form.quic_disable_pmtud = takeBool('disable_path_mtu_discovery')
  form.tfo = takeBool('tcp_fast_open')

  form.extra_raw = Object.keys(extra).length
    ? JSON.stringify(
        Object.fromEntries(Object.entries(extra).filter(([k]) => !KNOWN_EXTRA_KEYS.includes(k))),
        null,
        2,
      )
    : ''

  realityPublicKey.value = ''
  activeSections.value = ['basic', 'proto', 'transport', 'tls', 'reality', 'advanced']
  await loadPortPool()
  dialog.value = true
}

/* ---------------- 工具 ---------------- */

async function genReality() {
  const res = await api.post<{ private_key: string; public_key: string }>('/api/tools/reality', {})
  form.reality_private_key = res.private_key
  realityPublicKey.value = res.public_key
}

async function genObfsPassword() {
  const res = await api.post<{ password: string }>('/api/tools/password', {})
  form.obfs_password = res.password
}

/** 一键弱网预置：把「弱网下真正有用的开关」按协议一次配好（不改带宽，带宽必须用真实值）。 */
async function applyWeakNetPreset() {
  if (form.type === 'hysteria2') {
    form.hy2_ignore_bw = true
    if (!form.obfs_type) form.obfs_type = 'salamander'
    if (!form.obfs_password) await genObfsPassword()
    if (!form.up_mbps && !form.down_mbps) {
      ElMessage.warning(
        '已开启「忽略客户端带宽」+ 混淆。但上下行带宽仍是 0，此刻等于模式 C（纯 BBR、丢包会减速）；填入 VPS 真实带宽才进入模式 A（Brutal、丢包不减速）',
      )
    } else {
      ElMessage.success('已按模式 A 配好：服务端按真实带宽硬推，丢包不减速')
    }
    return
  }
  if (form.type === 'tuic') {
    form.congestion_control = 'bbr'
    if (!form.tuic_heartbeat) form.tuic_heartbeat = '10s'
    ElMessage.success('已设置拥塞控制 bbr + 心跳 10s（TUIC 走 QUIC，BBR 对它是客户端侧拥塞控制）')
    return
  }
  form.tfo = true
  ElMessage.success('已开启 TCP Fast Open：握手少一次往返，高延迟线路有感')
}

async function derivePublicKey() {
  if (!form.reality_private_key) {
    realityPublicKey.value = ''
    return
  }
  try {
    const res = await api.post<{ public_key: string }>('/api/tools/reality', { private_key: form.reality_private_key })
    realityPublicKey.value = res.public_key
  } catch {
    realityPublicKey.value = ''
  }
}

async function genShortID() {
  const res = await api.post<{ short_id: string }>('/api/tools/short-id', {})
  const list = splitList(form.reality_short_ids)
  list.push(res.short_id)
  form.reality_short_ids = list.join(', ')
}

async function genSSPassword() {
  const res = await api.post<{ password: string }>('/api/tools/ss-password', { method: form.method })
  form.server_password = res.password
}

/** 证书下拉的展示名 */
function certLabel(ct: Certificate): string {
  const kind = ct.kind === 'selfsigned' ? '自签' : ct.provider || 'ACME'
  return `${ct.domain}（${kind}）`
}

/** 选中证书时的联动：自签证书自动开启客户端跳验证，并用来填 SNI */
function onCertChange(id: number | null) {
  if (!id) return
  const ct = certs.value.find((c) => c.id === id)
  if (!ct || ct.kind !== 'selfsigned') return

  form.insecure = true
  if (form.server_name) return
  const cred = ct.credentials as { names?: unknown } | null
  const names = cred && Array.isArray(cred.names) ? (cred.names as string[]) : []
  if (names.length) {
    form.server_name = String(names[0])
  }
}

async function copy(text: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

/* ---------------- CRUD ---------------- */

async function load() {
  rows.value = await api.get<Inbound[]>('/api/inbounds')
}

async function loadPresets() {
  presets.value = await api.get<Presets>('/api/tools/presets')
}

async function loadCerts() {
  certs.value = await api.get<Certificate[]>('/api/certs')
}

async function save() {
  if (!form.port) {
    ElMessage.warning('请填写端口')
    return
  }
  if (form.reality_enabled && !form.reality_private_key) {
    ElMessage.warning('Reality 模式需要私钥')
    return
  }
  if (form.reality_enabled && splitList(form.reality_short_ids).length === 0) {
    ElMessage.warning('Reality 模式至少需要一个 Short ID')
    return
  }
  if (form.transport_headers.trim() && !parseRawJSON(form.transport_headers)) {
    ElMessage.error('额外 Headers 必须是合法的 JSON 对象')
    return
  }
  if (form.extra_raw.trim() && !parseRawJSON(form.extra_raw)) {
    ElMessage.error('附加字段必须是合法的 JSON 对象')
    return
  }
  // 非 Shadowsocks 协议强制 TLS：必须具备 Reality 或有效证书
  if (form.type !== 'shadowsocks' && !form.reality_enabled) {
    if (form.cert_source === 'panel' && !form.cert_id) {
      ElMessage.warning('该协议强制 TLS，请选择面板证书（可在「证书管理」生成自签证书）')
      return
    }
    if (form.cert_source === 'manual' && (!form.cert_path || !form.key_path)) {
      ElMessage.warning('该协议强制 TLS，请填写证书与私钥路径，或改用 Reality')
      return
    }
  }

  const batch = !form.id && form.batch_enabled && form.batch_count > 1
  const step = Math.max(1, form.batch_step || 1)
  if (batch && form.port + (form.batch_count - 1) * step > 65535) {
    ElMessage.warning('批量创建的最后一个端口超出 65535，请减小数量或步长')
    return
  }

  saving.value = true
  try {
    const payload = buildPayload()
    if (batch) {
      const res = await api.post<{ count?: number; apply_error?: string }>('/api/inbounds/batch', {
        ...payload,
        count: form.batch_count,
        port_step: step,
      })
      if (res.apply_error) {
        ElMessage.warning(`已创建 ${res.count || form.batch_count} 个入站，但内核应用失败：${res.apply_error}`)
      } else {
        ElMessage.success(`已创建 ${res.count || form.batch_count} 个入站并应用`)
      }
    } else {
      const res = form.id
        ? await api.put<{ apply_error?: string }>(`/api/inbounds/${form.id}`, payload)
        : await api.post<{ apply_error?: string }>('/api/inbounds', payload)
      if (res.apply_error) {
        ElMessage.warning(`已保存，但内核应用失败：${res.apply_error}`)
      } else {
        ElMessage.success('已保存并应用')
      }
    }
    dialog.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function testDelay(row: Inbound) {
  testing[row.id] = true
  try {
    const res = await api.post<{ delay: number }>(`/api/inbounds/${row.id}/test`)
    delays[row.id] = res.delay
    ElMessage.success(`节点可用，延迟 ${res.delay} ms`)
  } catch {
    delays[row.id] = -1
  } finally {
    testing[row.id] = false
  }
}

async function remove(row: Inbound) {
  await ElMessageBox.confirm(`确认删除入站 ${row.tag}？其下所有用户也会被删除。`, '提示', { type: 'warning' })
  await api.del(`/api/inbounds/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

onMounted(async () => {
  await Promise.all([load(), loadPresets(), loadCerts()])
})
</script>

<style scoped>
.hint {
  display: block;
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
}

.alpn-tag {
  margin-left: 6px;
  font-size: 11px;
  color: #909399;
}

.port-choices {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.port-chip {
  cursor: pointer;
}

:deep(.el-collapse-item__content) {
  padding-bottom: 8px;
}
.muted {
  color: #909399;
}

/* ---- 弱网优化提示 ---- */
.weak-preset {
  margin-bottom: 12px;
}

.weak-preset-title {
  margin-right: 8px;
}

.weak-modes {
  margin: -6px 0 14px 110px;
  padding: 8px 12px;
  border-left: 3px solid #67c23a;
  background: #f0f9eb;
  border-radius: 4px;
}

.weak-modes div {
  margin-bottom: 4px;
}

.weak-modes-title {
  color: #529b2e;
  font-weight: 600;
}

.weak-modes-warn {
  color: #e6a23c;
}
</style>
