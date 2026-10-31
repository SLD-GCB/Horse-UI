<template>
  <div class="page">
    <el-tabs v-model="tab">
      <el-tab-pane label="出站节点" name="nodes">
        <div class="toolbar">
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="出站用于链式转发；创建后需在「路由规则」中指定使用，或在规则里选择目标出站。"
          />
          <div class="btns">
            <el-button @click="openImport">从链接导入</el-button>
            <el-button type="primary" @click="openCreate">新增出站</el-button>
          </div>
        </div>

        <el-table :data="rows" border stripe>
          <el-table-column prop="id" label="ID" width="60" />
          <el-table-column prop="tag" label="标识" min-width="130" />
          <el-table-column prop="type" label="类型" width="110" />
          <el-table-column label="服务器" min-width="180">
            <template #default="{ row }">{{ row.server ? `${row.server}:${row.port}` : '-' }}</template>
          </el-table-column>
          <el-table-column label="延迟" width="110">
            <template #default="{ row }">
              <el-tag
                v-if="row.id in delays"
                size="small"
                :type="delays[row.id] < 0 ? 'danger' : delays[row.id] < 200 ? 'success' : delays[row.id] < 500 ? 'warning' : 'info'"
              >
                {{ delays[row.id] < 0 ? '超时' : delays[row.id] + ' ms' }}
              </el-tag>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column prop="remark" label="备注" min-width="120" />
          <el-table-column label="状态" width="80">
            <template #default="{ row }">
              <el-tag :type="row.enable ? 'success' : 'info'" size="small">{{ row.enable ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="230" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" :loading="testing[row.id]" @click="testDelay(row)">测速</el-button>
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button link type="danger" @click="remove(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="路由规则" name="rules">
        <RouteRules v-if="tab === 'rules'" />
      </el-tab-pane>
    </el-tabs>

    <el-drawer v-model="drawer" :title="form.id ? '编辑出站' : '新增出站'" size="900px">
      <el-form :model="form" label-width="120px" class="ob-form">
        <el-collapse v-model="active">
          <el-collapse-item title="基础" name="basic">
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="标识 (tag)">
                  <el-input v-model="form.tag" placeholder="如 out-hk" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="类型">
                  <el-select v-model="form.type" style="width: 100%">
                    <el-option v-for="t in presets.outbound_types" :key="t" :label="t" :value="t" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row v-if="needServer" :gutter="12">
              <el-col :span="16">
                <el-form-item label="服务器">
                  <el-input v-model="form.server" placeholder="域名或 IP" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="端口">
                  <el-input-number v-model="form.port" :min="1" :max="65535" style="width: 100%" />
                  <div class="port-quick">
                    <el-tag
                      v-for="p in commonPorts"
                      :key="p"
                      size="small"
                      class="port-tag"
                      @click="form.port = p"
                      >{{ p }}</el-tag
                    >
                  </div>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="备注">
              <el-input v-model="form.remark" />
            </el-form-item>
            <el-form-item label="启用">
              <el-switch v-model="form.enable" />
            </el-form-item>
            <el-form-item v-if="!form.id" label="批量创建">
              <el-switch
                v-model="form.batch_enabled"
                active-text="按当前配置创建多个"
                :disabled="form.type === 'wireguard'"
              />
              <template v-if="form.batch_enabled">
                <el-input-number
                  v-model="form.batch_count"
                  :min="2"
                  :max="200"
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
                标识可用 <code>{n}</code> 占位（如 hk-{n}），留空按「类型-端口」自动生成。
                将创建：{{ batchPreview }}
              </div>
              <div v-if="form.type === 'wireguard'" class="hint">
                WireGuard 的 peer 是固定端点，不支持批量创建。
              </div>
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item title="协议参数" name="proto">
            <template v-if="form.type === 'direct' || form.type === 'block'">
              <el-text class="muted">该类型无需额外参数。</el-text>
            </template>

            <template v-if="form.type === 'vless'">
              <el-form-item label="UUID">
                <el-input v-model="form.uuid">
                  <template #append><el-button @click="genUUID">生成</el-button></template>
                </el-input>
              </el-form-item>
              <el-form-item label="flow">
                <el-select v-model="form.flow" clearable style="width: 100%">
                  <el-option label="（无）" value="" />
                  <el-option label="xtls-rprx-vision" value="xtls-rprx-vision" />
                </el-select>
              </el-form-item>
            </template>

            <template v-if="form.type === 'vmess'">
              <el-form-item label="UUID">
                <el-input v-model="form.uuid">
                  <template #append><el-button @click="genUUID">生成</el-button></template>
                </el-input>
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="加密 (security)">
                    <el-select v-model="form.security" style="width: 100%">
                      <el-option v-for="s in presets.vmess_securities" :key="s" :label="s" :value="s" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="alter_id">
                    <el-input-number v-model="form.alter_id" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <template v-if="form.type === 'trojan'">
              <el-form-item label="密码">
                <el-input v-model="form.password" show-password />
              </el-form-item>
            </template>

            <template v-if="form.type === 'shadowsocks'">
              <el-form-item label="加密方式">
                <el-select v-model="form.method" filterable allow-create style="width: 100%">
                  <el-option v-for="m in ssMethods" :key="m" :label="m" :value="m" />
                </el-select>
              </el-form-item>
              <el-form-item label="密码">
                <el-input v-model="form.password" show-password />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="插件">
                    <el-input v-model="form.plugin" placeholder="如 obfs-local" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="插件参数">
                    <el-input v-model="form.plugin_opts" placeholder="如 obfs=http" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="network">
                <el-select v-model="form.network" clearable style="width: 100%">
                  <el-option label="（默认）" value="" />
                  <el-option label="tcp" value="tcp" />
                  <el-option label="udp" value="udp" />
                </el-select>
              </el-form-item>
            </template>

            <template v-if="form.type === 'hysteria2'">
              <el-form-item label="密码">
                <el-input v-model="form.password" show-password />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="混淆类型">
                    <el-select v-model="form.obfs_type" clearable style="width: 100%">
                      <el-option label="（无）" value="" />
                      <el-option label="salamander" value="salamander" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="混淆密码">
                    <el-input v-model="form.obfs_password" />
                  </el-form-item>
                </el-col>
              </el-row>
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
              </el-row>
            </template>

            <template v-if="form.type === 'skyhorse'">
              <el-form-item label="UUID">
                <el-input v-model="form.uuid" />
              </el-form-item>
              <el-form-item label="密码">
                <el-input v-model="form.password" show-password />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="混淆密码">
                    <el-input v-model="form.obfs_password" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="混淆轮换（秒）">
                    <el-input-number v-model="form.obfs_rotation" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
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
              </el-row>
            </template>

            <template v-if="form.type === 'tuic'">
              <el-form-item label="UUID">
                <el-input v-model="form.uuid">
                  <template #append><el-button @click="genUUID">生成</el-button></template>
                </el-input>
              </el-form-item>
              <el-form-item label="密码">
                <el-input v-model="form.password" show-password />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="拥塞控制">
                    <el-select v-model="form.congestion_control" clearable style="width: 100%">
                      <el-option label="（默认）" value="" />
                      <el-option v-for="x in presets.congestion" :key="x" :label="x" :value="x" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="UDP 中继">
                    <el-select v-model="form.udp_relay_mode" clearable style="width: 100%">
                      <el-option label="（默认）" value="" />
                      <el-option v-for="x in presets.tuic_relay_modes" :key="x" :label="x" :value="x" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="0-RTT">
                <el-switch v-model="form.zero_rtt_handshake" />
              </el-form-item>
            </template>

            <template v-if="form.type === 'socks'">
              <el-form-item label="版本">
                <el-select v-model="form.version" style="width: 100%">
                  <el-option v-for="v in presets.socks_versions" :key="v" :label="v" :value="v" />
                </el-select>
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="用户名">
                    <el-input v-model="form.username" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="密码">
                    <el-input v-model="form.password" show-password />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="UDP over TCP">
                <el-switch v-model="form.udp_over_tcp" />
              </el-form-item>
            </template>

            <template v-if="form.type === 'http'">
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="用户名">
                    <el-input v-model="form.username" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="密码">
                    <el-input v-model="form.password" show-password />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>

            <template v-if="form.type === 'wireguard'">
              <el-alert
                type="warning"
                :closable="false"
                show-icon
                title="WireGuard 会生成到内核配置的顶层 endpoints（sing-box 1.14 已不再支持 wireguard outbound）。"
                style="margin-bottom: 10px"
              />
              <el-form-item label="私钥">
                <el-input v-model="form.wg_private_key" placeholder="Base64 私钥" />
              </el-form-item>
              <el-form-item label="本地地址">
                <el-input v-model="form.wg_address" placeholder="如 10.0.0.1/32，逗号分隔多个" />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="MTU">
                    <el-input-number v-model="form.wg_mtu" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="workers">
                    <el-input-number v-model="form.wg_workers" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="本地监听端口">
                <el-input-number v-model="form.wg_listen_port" :min="0" :max="65535" style="width: 100%" />
                <div class="port-quick">
                  <el-button size="small" @click="assignWgPort">自动分配可用端口</el-button>
                  <el-tag
                    v-for="p in freePorts"
                    :key="p"
                    size="small"
                    class="port-tag"
                    @click="form.wg_listen_port = p"
                    >{{ p }}</el-tag
                  >
                </div>
                <div class="hint">留空（0）表示不固定监听端口；仅在需要被对端主动连接时才填。</div>
              </el-form-item>
              <el-divider content-position="left">对端 peer</el-divider>
              <el-form-item label="公钥">
                <el-input v-model="form.wg_peer_public_key" placeholder="Base64 公钥" />
              </el-form-item>
              <el-form-item label="allowed_ips">
                <el-input v-model="form.wg_peer_allowed_ips" placeholder="如 0.0.0.0/0, ::/0" />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="预共享密钥">
                    <el-input v-model="form.wg_peer_pre_shared_key" />
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="保活间隔(秒)">
                    <el-input-number v-model="form.wg_peer_keepalive" :min="0" style="width: 100%" />
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="reserved">
                <el-input v-model="form.wg_peer_reserved" placeholder="如 1,2,3（Cloudflare WARP 用）" />
              </el-form-item>
            </template>
          </el-collapse-item>

          <el-collapse-item v-if="supportsTransport" title="传输层" name="transport">
            <el-form-item label="类型">
              <el-select v-model="form.trans_type" clearable style="width: 100%">
                <el-option label="（默认 tcp）" value="" />
                <el-option label="ws" value="ws" />
                <el-option label="grpc" value="grpc" />
                <el-option label="http" value="http" />
                <el-option label="httpupgrade" value="httpupgrade" />
              </el-select>
            </el-form-item>
            <el-form-item v-if="['ws', 'http', 'httpupgrade'].includes(form.trans_type)" label="path">
              <el-input v-model="form.trans_path" placeholder="/" />
            </el-form-item>
            <el-form-item v-if="['ws', 'http', 'httpupgrade'].includes(form.trans_type)" label="host">
              <el-input v-model="form.trans_host" placeholder="可选，逗号分隔" />
            </el-form-item>
            <el-form-item v-if="form.trans_type === 'grpc'" label="service_name">
              <el-input v-model="form.trans_service" />
            </el-form-item>
            <el-form-item v-if="form.trans_type === 'http'" label="method">
              <el-select v-model="form.trans_method" style="width: 100%">
                <el-option v-for="m in presets.http_methods" :key="m" :label="m" :value="m" />
              </el-select>
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item v-if="needTLS" title="TLS" name="tls">
            <el-form-item label="启用 TLS">
              <el-switch
                :model-value="tlsActive"
                :disabled="forceTLS"
                @update:model-value="onTlsToggle"
              />
              <span v-if="forceTLS" class="hint">该协议强制启用 TLS</span>
            </el-form-item>
            <template v-if="tlsActive">
              <el-form-item label="SNI">
                <el-input v-model="form.sni" placeholder="server_name" />
              </el-form-item>
              <el-form-item label="跳过校验">
                <el-switch v-model="form.insecure" />
              </el-form-item>
              <el-form-item label="ALPN">
                <el-input v-model="form.alpn" placeholder="如 h2,http/1.1" />
              </el-form-item>
              <el-row :gutter="12">
                <el-col :span="12">
                  <el-form-item label="最低版本">
                    <el-select v-model="form.min_version" clearable style="width: 100%">
                      <el-option v-for="v in presets.tls_versions" :key="v" :label="v" :value="v" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="最高版本">
                    <el-select v-model="form.max_version" clearable style="width: 100%">
                      <el-option v-for="v in presets.tls_versions" :key="v" :label="v" :value="v" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="uTLS 指纹">
                <el-select v-model="form.utls_fp" clearable style="width: 100%" placeholder="不启用">
                  <el-option v-for="f in presets.utls_fingerprints" :key="f" :label="f" :value="f" />
                </el-select>
              </el-form-item>
              <el-divider content-position="left">Reality</el-divider>
              <el-form-item label="启用 Reality">
                <el-switch v-model="form.reality_enabled" />
              </el-form-item>
              <template v-if="form.reality_enabled">
                <el-form-item label="公钥 (pbk)">
                  <el-input v-model="form.reality_pbk" />
                </el-form-item>
                <el-form-item label="Short ID">
                  <el-input v-model="form.reality_sid" />
                </el-form-item>
              </template>
            </template>
          </el-collapse-item>

          <el-collapse-item v-if="supportsMultiplex" title="多路复用" name="mux">
            <el-form-item label="启用">
              <el-switch v-model="form.mux_enabled" />
            </el-form-item>
            <template v-if="form.mux_enabled">
              <el-form-item label="协议">
                <el-select v-model="form.mux_protocol" style="width: 100%">
                  <el-option v-for="p in presets.multiplex_protocols" :key="p" :label="p" :value="p" />
                </el-select>
              </el-form-item>
              <el-form-item label="最大流数">
                <el-input-number v-model="form.mux_max_streams" :min="0" style="width: 100%" />
              </el-form-item>
              <el-form-item label="padding">
                <el-switch v-model="form.mux_padding" />
              </el-form-item>
            </template>
          </el-collapse-item>

          <el-collapse-item title="拨号与链式" name="dial">
            <el-form-item v-if="supportsDetour" label="上游出站">
              <el-select v-model="form.dial_detour" clearable style="width: 100%" placeholder="不设置（直连拨号）">
                <el-option v-for="ob in detourOptions" :key="ob.tag" :label="`${ob.tag}（${ob.type}）`" :value="ob.tag" />
              </el-select>
              <div class="hint">设置后该出站会经由所选出站拨号（链式转发）；注意不要形成环。</div>
            </el-form-item>
            <el-form-item label="绑定网卡">
              <el-select
                v-model="form.dial_bind"
                clearable
                filterable
                allow-create
                style="width: 100%"
                placeholder="不绑定（走系统默认路由）"
              >
                <el-option
                  v-for="nic in nicList"
                  :key="nic.name"
                  :label="`${nic.name}${nic.addresses?.length ? '（' + nic.addresses.join(', ') + '）' : ''}`"
                  :value="nic.name"
                />
              </el-select>
              <div class="hint">只从本机指定网卡拨号；留空表示由系统路由决定。</div>
            </el-form-item>
            <el-form-item label="routing_mark">
              <el-input-number v-model="form.dial_mark" :min="0" style="width: 100%" />
              <div class="hint warn" v-if="isWindows">该字段仅 Linux 支持，Windows 上填写会导致配置校验失败。</div>
            </el-form-item>
            <el-form-item label="TCP Fast Open">
              <el-switch v-model="form.dial_tfo" />
            </el-form-item>
            <el-form-item label="TCP Multi Path">
              <el-switch v-model="form.dial_mp" />
            </el-form-item>
            <el-form-item label="UDP 分片">
              <el-switch v-model="form.dial_udp_fragment" />
            </el-form-item>
            <el-form-item label="域名解析器">
              <el-input v-model="form.dial_resolver" placeholder="如 dns-direct（DNS 服务器 tag）" />
            </el-form-item>
            <el-form-item label="连接超时">
              <el-input v-model="form.dial_timeout" placeholder="如 5s" />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item title="高级（附加 JSON）" name="advanced">
            <el-form-item label="附加字段">
              <el-input
                v-model="form.extraText"
                type="textarea"
                :rows="4"
                placeholder='任意附加字段，例如 {"tcp_multi_path":true}'
              />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item title="配置预览（sing-box 出站）" name="preview">
            <pre class="preview">{{ previewText }}</pre>
          </el-collapse-item>
        </el-collapse>
      </el-form>

      <template #footer>
        <el-button @click="drawer = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-drawer>

    <el-dialog v-model="importDialog" title="从分享链接导入" width="560px">
      <el-input
        v-model="importLink"
        type="textarea"
        :rows="3"
        placeholder="粘贴 vless:// / vmess:// / trojan:// / ss:// / hysteria2:// / tuic:// 链接"
      />
      <template #footer>
        <el-button @click="importDialog = false">取消</el-button>
        <el-button type="primary" @click="doImport">解析并填入表单</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import type { NetInterface, Outbound, PortPool, Presets } from '@/api/types'
import RouteRules from '@/views/RouteRules.vue'

const tab = ref('nodes')
const rows = ref<Outbound[]>([])
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
  outbound_types: [],
  outbound_transports: [],
  vmess_securities: [],
  socks_versions: [],
  tuic_relay_modes: [],
  multiplex_protocols: [],
  utls_fingerprints: [],
})

const drawer = ref(false)
const saving = ref(false)
const ports = ref<PortPool | null>(null)
const active = ref<string[]>(['basic', 'proto'])
const importDialog = ref(false)
const importLink = ref('')
const delays = reactive<Record<number, number>>({})
const testing = reactive<Record<number, boolean>>({})

const isWindows = computed(() => navigator.userAgent.includes('Windows'))
const ssMethods = computed(() => [...presets.value.ss2022_methods, ...presets.value.ss_methods])
const needServer = computed(() => !['direct', 'block'].includes(form.type))
const needTLS = computed(() => ['vless', 'vmess', 'trojan', 'hysteria2', 'skyhorse', 'tuic', 'http'].includes(form.type))
const forceTLS = computed(() => ['hysteria2', 'skyhorse', 'tuic'].includes(form.type))
// tlsActive 表示"实际会启用 TLS"：强制 TLS 的协议即便开关未打开也算启用。
const tlsActive = computed(() => form.tls_enabled || forceTLS.value)
const supportsTransport = computed(() => ['vless', 'vmess', 'trojan'].includes(form.type))
const supportsMultiplex = computed(() => ['vless', 'vmess', 'trojan', 'shadowsocks', 'socks'].includes(form.type))
const detourOptions = computed(() => rows.value.filter((o) => o.tag !== form.tag && o.type !== 'block'))
// direct / block 不支持 detour（内核：detour is not supported in direct context）。
const supportsDetour = computed(() => !['direct', 'block'].includes(form.type))

// 本机网卡列表（用于「绑定网卡」）。
const nicList = computed<NetInterface[]>(() => (ports.value?.interfaces || []).filter((i) => !i.loopback))
// 端口池中当前可用端口（用于 WireGuard 本地监听端口）。
const freePorts = computed<number[]>(() => (ports.value?.free || []).slice(0, 10))
// 远端端口常用快捷（按类型推荐）。
const commonPorts = computed(() => {
  switch (form.type) {
    case 'hysteria2':
    case 'tuic':
    case 'vless':
    case 'vmess':
    case 'trojan':
      return [443, 8443, 2053, 2083]
    case 'shadowsocks':
      return [8388, 443, 8080]
    case 'socks':
      return [1080, 1081]
    case 'http':
      return [8080, 3128, 8888]
    case 'wireguard':
      return [51820, 51821]
    default:
      return [443, 80, 8080]
  }
})

function emptyForm() {
  return {
    id: 0,
    tag: '',
    type: 'vless',
    server: '',
    port: 443,
    remark: '',
    enable: true,
    uuid: '',
    flow: '',
    security: 'auto',
    alter_id: 0,
    password: '',
    method: 'aes-128-gcm',
    plugin: '',
    plugin_opts: '',
    network: '',
    obfs_type: '',
    obfs_password: '',
    up_mbps: 0,
    obfs_rotation: 0,
    down_mbps: 0,
    congestion_control: '',
    udp_relay_mode: '',
    zero_rtt_handshake: false,
    version: '5',
    username: '',
    udp_over_tcp: false,
    wg_private_key: '',
    wg_address: '',
    wg_mtu: 0,
    wg_workers: 0,
    wg_listen_port: 0,
    wg_peer_public_key: '',
    wg_peer_allowed_ips: '0.0.0.0/0, ::/0',
    wg_peer_pre_shared_key: '',
    wg_peer_keepalive: 0,
    wg_peer_reserved: '',
    trans_type: '',
    trans_path: '',
    trans_host: '',
    trans_service: '',
    trans_method: 'GET',
    tls_enabled: false,
    sni: '',
    insecure: false,
    alpn: '',
    min_version: '',
    max_version: '',
    utls_fp: '',
    reality_enabled: false,
    reality_pbk: '',
    reality_sid: '',
    mux_enabled: false,
    mux_protocol: 'smux',
    mux_max_streams: 0,
    mux_padding: false,
    dial_detour: '',
    dial_bind: '',
    dial_mark: 0,
    dial_tfo: false,
    dial_mp: false,
    dial_udp_fragment: false,
    dial_resolver: '',
    dial_timeout: '',
    extraText: '',

    // 批量创建（仅新增时生效）
    batch_enabled: false,
    batch_count: 10,
    batch_step: 1,
  }
}

const form = reactive(emptyForm())

function toList(s: string): string[] {
  return s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
}

function arrText(v: unknown): string {
  return Array.isArray(v) ? (v as string[]).join(',') : ''
}

// ---- 表单 -> 后端 payload ----

function buildConfig(): Record<string, unknown> {
  const cfg: Record<string, unknown> = {}
  switch (form.type) {
    case 'vless':
      cfg.uuid = form.uuid
      if (form.flow) cfg.flow = form.flow
      break
    case 'vmess':
      cfg.uuid = form.uuid
      if (form.security) cfg.security = form.security
      if (form.alter_id) cfg.alter_id = form.alter_id
      break
    case 'trojan':
      cfg.password = form.password
      break
    case 'shadowsocks':
      cfg.method = form.method
      cfg.password = form.password
      if (form.plugin) cfg.plugin = form.plugin
      if (form.plugin_opts) cfg.plugin_opts = form.plugin_opts
      if (form.network) cfg.network = form.network
      break
    case 'hysteria2': {
      if (form.password) cfg.password = form.password
      if (form.obfs_type) {
        const obfs: Record<string, unknown> = { type: form.obfs_type }
        if (form.obfs_password) obfs.password = form.obfs_password
        cfg.obfs = obfs
      }
      if (form.up_mbps) cfg.up_mbps = form.up_mbps
      if (form.down_mbps) cfg.down_mbps = form.down_mbps
      if (form.network) cfg.network = form.network
      break
    }
    case 'skyhorse': {
      cfg.uuid = form.uuid
      if (form.password) cfg.password = form.password
      if (form.obfs_password) cfg.obfs_password = form.obfs_password
      if (form.obfs_rotation) cfg.obfs_rotation_seconds = form.obfs_rotation
      if (form.up_mbps) cfg.up_mbps = form.up_mbps
      if (form.down_mbps) cfg.down_mbps = form.down_mbps
      break
    }
    case 'tuic': {
      cfg.uuid = form.uuid
      if (form.password) cfg.password = form.password
      if (form.congestion_control) cfg.congestion_control = form.congestion_control
      if (form.udp_relay_mode) cfg.udp_relay_mode = form.udp_relay_mode
      if (form.zero_rtt_handshake) cfg.zero_rtt_handshake = true
      if (form.heartbeat) cfg.heartbeat = form.heartbeat
      break
    }
    case 'socks': {
      if (form.version) cfg.version = form.version
      if (form.username) cfg.username = form.username
      if (form.password) cfg.password = form.password
      if (form.network) cfg.network = form.network
      if (form.udp_over_tcp) cfg.udp_over_tcp = { enabled: true, version: 2 }
      break
    }
    case 'http': {
      if (form.username) cfg.username = form.username
      if (form.password) cfg.password = form.password
      break
    }
    case 'wireguard': {
      cfg.private_key = form.wg_private_key
      if (form.wg_address) cfg.address = toList(form.wg_address)
      if (form.wg_mtu) cfg.mtu = form.wg_mtu
      if (form.wg_workers) cfg.workers = form.wg_workers
      if (form.wg_listen_port) cfg.listen_port = form.wg_listen_port
      const peer: Record<string, unknown> = {
        public_key: form.wg_peer_public_key,
        address: form.server,
        port: form.port,
        allowed_ips: toList(form.wg_peer_allowed_ips),
      }
      if (form.wg_peer_pre_shared_key) peer.pre_shared_key = form.wg_peer_pre_shared_key
      if (form.wg_peer_keepalive) peer.persistent_keepalive_interval = form.wg_peer_keepalive
      if (form.wg_peer_reserved) peer.reserved = toList(form.wg_peer_reserved).map(Number)
      cfg.peers = [peer]
      break
    }
  }
  return cfg
}

function buildTLS(): Record<string, unknown> | null {
  if (!tlsActive.value) return null
  const tls: Record<string, unknown> = { enabled: true }
  if (form.sni) tls.server_name = form.sni
  if (form.insecure) tls.insecure = true
  if (form.alpn) tls.alpn = toList(form.alpn)
  if (form.min_version) tls.min_version = form.min_version
  if (form.max_version) tls.max_version = form.max_version
  if (form.utls_fp || form.reality_enabled) {
    tls.utls = { enabled: true, fingerprint: form.utls_fp || 'chrome' }
  }
  if (form.reality_enabled) {
    const r: Record<string, unknown> = { enabled: true }
    if (form.reality_pbk) r.public_key = form.reality_pbk
    if (form.reality_sid) r.short_id = form.reality_sid
    tls.reality = r
  }
  return tls
}

function buildTransport(): Record<string, unknown> | null {
  if (!form.trans_type) return null
  const tr: Record<string, unknown> = { type: form.trans_type }
  if (['ws', 'http', 'httpupgrade'].includes(form.trans_type)) {
    if (form.trans_path) tr.path = form.trans_path
  }
  if (form.trans_type === 'ws') {
    if (form.trans_host) tr.headers = { Host: form.trans_host }
  } else if (form.trans_type === 'grpc') {
    if (form.trans_service) tr.service_name = form.trans_service
  } else if (form.trans_type === 'http') {
    if (form.trans_host) tr.host = toList(form.trans_host)
    if (form.trans_method) tr.method = form.trans_method
  } else if (form.trans_type === 'httpupgrade') {
    if (form.trans_host) tr.host = form.trans_host
  }
  return tr
}

function buildPayload() {
  const mux = form.mux_enabled
    ? {
        enabled: true,
        ...(form.mux_protocol ? { protocol: form.mux_protocol } : {}),
        ...(form.mux_max_streams ? { max_streams: form.mux_max_streams } : {}),
        ...(form.mux_padding ? { padding: true } : {}),
      }
    : null

  const dial: Record<string, unknown> = {}
  if (form.dial_detour) dial.detour = form.dial_detour
  if (form.dial_bind) dial.bind_interface = form.dial_bind
  if (form.dial_mark) dial.routing_mark = form.dial_mark
  if (form.dial_tfo) dial.tcp_fast_open = true
  if (form.dial_mp) dial.tcp_multi_path = true
  if (form.dial_udp_fragment) dial.udp_fragment = true
  if (form.dial_resolver) dial.domain_resolver = form.dial_resolver
  if (form.dial_timeout) dial.connect_timeout = form.dial_timeout

  let extra: unknown = null
  if (form.extraText.trim()) {
    try {
      extra = JSON.parse(form.extraText)
    } catch {
      extra = null
    }
  }

  return {
    id: form.id,
    tag: form.tag,
    type: form.type,
    server: form.server,
    port: form.port,
    remark: form.remark,
    enable: form.enable,
    config: buildConfig(),
    tls: buildTLS(),
    transport: buildTransport(),
    multiplex: mux,
    dial: Object.keys(dial).length ? dial : null,
    extra,
  }
}

const previewText = computed(() => {
  const p = buildPayload()
  const obj: Record<string, unknown> = { type: p.type, tag: p.tag }
  if (!['direct', 'block', 'wireguard'].includes(p.type)) {
    if (p.server) obj.server = p.server
    if (p.port) obj.server_port = p.port
  }
  Object.assign(obj, p.config as Record<string, unknown>)
  if (p.tls) obj.tls = p.tls
  if (p.transport) obj.transport = p.transport
  if (p.multiplex) obj.multiplex = p.multiplex
  if (p.dial) Object.assign(obj, p.dial as Record<string, unknown>)
  if (p.extra) Object.assign(obj, p.extra as Record<string, unknown>)
  return JSON.stringify(obj, null, 2)
})

/** 批量创建预览：前几个标识 + 端口区间，和后端生成规则保持一致。 */
const batchPreview = computed(() => {
  const count = Math.max(2, Math.min(form.batch_count || 2, 200))
  const step = Math.max(1, form.batch_step || 1)
  const baseTag = form.tag.trim()
  const portAt = (i: number) => (needServer.value ? form.port + (i - 1) * step : 0)
  const tagAt = (i: number) => {
    const port = portAt(i)
    if (!baseTag) return port > 0 ? `out-${form.type}-${port}` : `out-${form.type}-${i}`
    return baseTag.includes('{n}') ? baseTag.replaceAll('{n}', String(i)) : `${baseTag}-${i}`
  }
  const shown = [1, 2, 3].slice(0, count).map(tagAt)
  const ellipsis = count > 3 ? ' … ' : ''
  if (!needServer.value) return `${shown.join('、')}${ellipsis}`
  const lastPort = form.port + (count - 1) * step
  return `${shown.join('、')}${ellipsis}（端口 ${form.port}–${lastPort}）`
})

// ---- 后端对象 -> 表单 ----

function fillForm(ob: Outbound) {
  Object.assign(form, emptyForm())
  form.id = ob.id || 0
  form.tag = ob.tag || ''
  form.type = ob.type || 'vless'
  form.server = ob.server || ''
  form.port = ob.port || 0
  form.remark = ob.remark || ''
  form.enable = ob.enable !== false

  const cfg = (ob.config || {}) as Record<string, any>
  form.uuid = cfg.uuid || ''
  form.flow = cfg.flow || ''
  form.security = cfg.security || 'auto'
  form.alter_id = cfg.alter_id || 0
  form.password = cfg.password || ''
  form.method = cfg.method || 'aes-128-gcm'
  form.plugin = cfg.plugin || ''
  form.plugin_opts = cfg.plugin_opts || ''
  form.network = cfg.network || ''
  form.obfs_type = cfg.obfs?.type || ''
  form.obfs_password = cfg.obfs?.password || ''
  form.up_mbps = cfg.up_mbps || 0
  form.down_mbps = cfg.down_mbps || 0
  form.congestion_control = cfg.congestion_control || ''
  form.udp_relay_mode = cfg.udp_relay_mode || ''
  form.zero_rtt_handshake = !!cfg.zero_rtt_handshake
  form.heartbeat = cfg.heartbeat || ''
  form.version = cfg.version || '5'
  form.username = cfg.username || ''
  form.udp_over_tcp = !!cfg.udp_over_tcp
  form.wg_private_key = cfg.private_key || ''
  form.wg_address = arrText(cfg.address)
  form.wg_mtu = cfg.mtu || 0
  form.wg_workers = cfg.workers || 0
  form.wg_listen_port = cfg.listen_port || 0
  const peer = Array.isArray(cfg.peers) ? cfg.peers[0] : null
  if (peer) {
    form.wg_peer_public_key = peer.public_key || ''
    form.wg_peer_allowed_ips = arrText(peer.allowed_ips)
    form.wg_peer_pre_shared_key = peer.pre_shared_key || ''
    form.wg_peer_keepalive = peer.persistent_keepalive_interval || 0
    form.wg_peer_reserved = arrText(peer.reserved)
  }

  const tls = (ob.tls || null) as Record<string, any> | null
  if (tls && tls.enabled) {
    form.tls_enabled = true
    form.sni = tls.server_name || ''
    form.insecure = !!tls.insecure
    form.alpn = arrText(tls.alpn)
    form.min_version = tls.min_version || ''
    form.max_version = tls.max_version || ''
    if (tls.utls) form.utls_fp = tls.utls.fingerprint || 'chrome'
    if (tls.reality) {
      form.reality_enabled = true
      form.reality_pbk = tls.reality.public_key || ''
      form.reality_sid = tls.reality.short_id || ''
    }
  }

  const tr = (ob.transport || null) as Record<string, any> | null
  if (tr && tr.type) {
    form.trans_type = tr.type
    form.trans_path = tr.path || ''
    if (Array.isArray(tr.host)) form.trans_host = tr.host.join(',')
    else if (typeof tr.host === 'string') form.trans_host = tr.host
    else if (tr.headers?.Host) form.trans_host = tr.headers.Host
    form.trans_service = tr.service_name || ''
    form.trans_method = tr.method || 'GET'
  }

  const mux = (ob.multiplex || null) as Record<string, any> | null
  if (mux && mux.enabled) {
    form.mux_enabled = true
    form.mux_protocol = mux.protocol || 'smux'
    form.mux_max_streams = mux.max_streams || 0
    form.mux_padding = !!mux.padding
  }

  const dial = (ob.dial || null) as Record<string, any> | null
  if (dial) {
    form.dial_detour = dial.detour || ''
    form.dial_bind = dial.bind_interface || ''
    form.dial_mark = dial.routing_mark || 0
    form.dial_tfo = !!dial.tcp_fast_open
    form.dial_mp = !!dial.tcp_multi_path
    form.dial_udp_fragment = !!dial.udp_fragment
    form.dial_resolver = dial.domain_resolver || ''
    form.dial_timeout = dial.connect_timeout || ''
  }

  form.extraText = ob.extra ? JSON.stringify(ob.extra, null, 2) : ''
}

// ---- 交互 ----

async function load() {
  rows.value = await api.get<Outbound[]>('/api/outbounds')
}

async function loadPresets() {
  try {
    presets.value = await api.get<Presets>('/api/tools/presets')
  } catch {
    /* 忽略 */
  }
}

async function loadPorts() {
  try {
    ports.value = await api.get<PortPool>('/api/ports')
  } catch {
    /* 忽略 */
  }
}

// assignWgPort 从端口池自动分配一个可用端口给 WireGuard 的本地监听。
function assignWgPort() {
  form.wg_listen_port = ports.value?.suggested || 0
}

// 记住每种类型上次填的远端端口，避免每次新建都要重填。
function lastPort(type: string, def: number): number {
  const v = localStorage.getItem('horseui_out_port_' + type)
  const n = v ? Number(v) : 0
  return n > 0 ? n : def
}

function rememberPort(type: string, port: number) {
  if (port > 0) localStorage.setItem('horseui_out_port_' + type, String(port))
}

// 切换类型时带出该类型上次用过的远端端口。
watch(
  () => form.type,
  (t) => {
    const v = localStorage.getItem('horseui_out_port_' + t)
    if (v && Number(v) > 0) form.port = Number(v)
  },
)

function openCreate() {
  Object.assign(form, emptyForm())
  form.port = lastPort(form.type, form.port)
  active.value = ['basic', 'proto']
  drawer.value = true
}

function openEdit(row: Outbound) {
  fillForm(row)
  active.value = ['basic', 'proto', 'transport', 'tls', 'mux', 'dial', 'advanced']
  drawer.value = true
}

function openImport() {
  importLink.value = ''
  importDialog.value = true
}

async function genUUID() {
  const res = await api.post<{ uuid: string }>('/api/tools/uuid', {})
  form.uuid = res.uuid
}

async function doImport() {
  if (!importLink.value.trim()) {
    ElMessage.warning('请粘贴分享链接')
    return
  }
  try {
    const ob = await api.post<Outbound>('/api/outbounds/parse', { link: importLink.value.trim() })
    fillForm(ob)
    form.id = 0
    importDialog.value = false
    active.value = ['basic', 'proto', 'transport', 'tls', 'dial', 'advanced']
    drawer.value = true
    ElMessage.success('已解析，请核对后保存')
  } catch {
    /* 拦截器已提示 */
  }
}

function onTlsToggle(v: boolean) {
  form.tls_enabled = v
}

async function save() {
  const batch = !form.id && form.batch_enabled && form.batch_count > 1
  if (!form.type || (!form.tag && !batch)) {
    ElMessage.warning(batch ? '请选择类型' : '请填写标识和类型')
    return
  }
  if (form.extraText.trim()) {
    try {
      JSON.parse(form.extraText)
    } catch {
      ElMessage.error('附加 JSON 格式错误')
      return
    }
  }
  const step = Math.max(1, form.batch_step || 1)
  if (batch && form.type === 'wireguard') {
    ElMessage.warning('WireGuard 的 peer 是固定端点，不支持批量创建')
    return
  }
  if (batch && needServer.value && form.port + (form.batch_count - 1) * step > 65535) {
    ElMessage.warning('批量创建的最后一个端口超出 65535，请减小数量或步长')
    return
  }

  saving.value = true
  try {
    const payload = buildPayload()
    if (batch) {
      const res = await api.post<{ count?: number; apply_error?: string }>('/api/outbounds/batch', {
        ...payload,
        count: form.batch_count,
        port_step: step,
      })
      if (res.apply_error) {
        ElMessage.warning(`已创建 ${res.count || form.batch_count} 个出站，但内核应用失败：${res.apply_error}`)
      } else {
        ElMessage.success(`已创建 ${res.count || form.batch_count} 个出站并应用`)
      }
    } else if (form.id) {
      await api.put(`/api/outbounds/${form.id}`, payload)
      ElMessage.success('已保存并应用')
    } else {
      await api.post('/api/outbounds', payload)
      ElMessage.success('已保存并应用')
    }
    rememberPort(form.type, form.port)
    drawer.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function testDelay(row: Outbound) {
  testing[row.id] = true
  try {
    const res = await api.post<{ delay: number }>('/api/outbounds/test', { id: row.id })
    delays[row.id] = res.delay
  } catch {
    delays[row.id] = -1
  } finally {
    testing[row.id] = false
  }
}

async function remove(row: Outbound) {
  await ElMessageBox.confirm(`确认删除出站 ${row.tag}？`, '提示', { type: 'warning' })
  await api.del(`/api/outbounds/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

onMounted(async () => {
  await Promise.all([load(), loadPresets(), loadPorts()])
})
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.toolbar :deep(.el-alert) {
  flex: 1;
}

.btns {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.muted {
  color: #909399;
  font-size: 12px;
}

.hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}

.hint.warn {
  color: #e6a23c;
}

.preview {
  margin: 0;
  padding: 10px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  max-height: 360px;
  overflow: auto;
}

.port-quick {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
  align-items: center;
}

.port-tag {
  cursor: pointer;
}
</style>
