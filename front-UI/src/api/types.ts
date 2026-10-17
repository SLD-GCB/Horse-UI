export interface Inbound {
  id: number
  tag: string
  type: string
  remark: string
  enable: boolean
  listen: string
  port: number
  sniff: boolean
  sniff_override: boolean
  tls: unknown
  reality: unknown
  transport: unknown
  extra: unknown
  method: string
  server_password: string
  up_mbps: number
  down_mbps: number
  obfs_type: string
  obfs_password: string
  congestion_control: string
  masquerade: string
  cert_id: number | null
  insecure: boolean
  used_up: number
  used_down: number
  client_count?: number
  created_at?: string
  updated_at?: string
}

export interface Client {
  id: number
  inbound_id: number
  name: string
  uuid: string
  password: string
  flow: string
  enable: boolean
  quota_bytes: number
  used_up: number
  used_down: number
  expire_at: string | null
  sub_token: string
  remark: string
  inbound_tag?: string
  inbound_type?: string
  inbound_ids?: number[]
  created_at?: string
  updated_at?: string
}

export interface Outbound {
  id: number
  tag: string
  type: string
  server: string
  port: number
  remark: string
  enable: boolean
  config: unknown
  tls: unknown
  transport: unknown
  multiplex: unknown
  dial: unknown
  extra: unknown
}

export interface RouteConfig {
  rules: Record<string, unknown>[]
  final: string
}

export interface CoreStatus {
  installed: boolean
  version: string
  running: boolean
  config_path: string
  apply_error?: string
}

export interface PanelAddress {
  current: string
  domain: string
  listen: string
  scheme: string
  port: string
  urls: string[] | null
}

export interface SysInfo {
  version: string
  os: string
  arch: string
  go_version: string
  cpus: number
  uptime: number
  mem_alloc: number
  mem_sys: number
  goroutines: number
  hostname?: string
  core: CoreStatus
  panel?: PanelAddress
}

export interface Presets {
  ss_methods: string[]
  ss2022_methods: string[]
  inbound_types: string[]
  transports: string[]
  congestion: string[]
  obfs_types: string[]
  tls_versions: string[]
  alpn_presets: { label: string; value: string[] }[]
  cipher_suites: string[]
  client_auth: { label: string; value: string }[]
  ss_networks: string[]
  http_methods: string[]
  bbr_profiles: string[]
  reality_maxdiff: string[]
  outbound_types: string[]
  outbound_transports: string[]
  vmess_securities: string[]
  socks_versions: string[]
  tuic_relay_modes: string[]
  multiplex_protocols: string[]
  utls_fingerprints: string[]
}

export interface ClientLinks {
  link: string
  links: string[]
  nodes: number
  sub_url: string
  /** 三种配置的二维码（PNG data URL）；内容过大时为空串。 */
  qrcodes: {
    general: string
    clash: string
    singbox: string
  }
  clash: unknown
  singbox: unknown
}

export interface ApplyResult {
  apply_error?: string
}

export interface CertFieldSpec {
  key: string
  label: string
  required: boolean
  secret: boolean
}

export interface CertProviderSpec {
  name: string
  label: string
  fields: CertFieldSpec[] | null
}

export interface Certificate {
  id: number
  kind: string
  domain: string
  provider: string
  credentials: unknown
  cert_path: string
  key_path: string
  auto_renew: boolean
  not_before: string | null
  not_after: string | null
}

export interface InboundTraffic {
  tag: string
  up: number
  down: number
  bytes: number
  clients: number
}

export interface StatsSummary {
  mode: string
  per_user: boolean
  split: boolean
  inbounds: InboundTraffic[]
}

export interface NetInterface {
  name: string
  addresses: string[]
  loopback: boolean
}

export interface ListenPort {
  port: number
  proto: string[]
  panel: boolean
}

export interface PanelPort {
  port: number
  tag: string
  type: string
}

export interface PortPool {
  range: { start: number; end: number }
  interfaces: NetInterface[] | null
  listening: ListenPort[] | null
  panel: PanelPort[] | null
  free: number[] | null
  suggested: number
}

export interface LoginResult {
  token: string
  username: string
  version: string
  must_change?: boolean
  weak_password?: boolean
}

export interface Profile {
  username: string
  version: string
  must_change?: boolean
}

export interface AuditLog {
  id: number
  username: string
  ip: string
  method: string
  path: string
  action: string
  status: number
  detail: string
  created_at: string
}

export interface AuditPage {
  total: number
  items: AuditLog[] | null
  page: number
  size: number
}

/** 单条内核参数的体检结果（系统调优）。 */
export interface TuneItem {
  key: string
  label: string
  group: string
  current: string
  suggest: string
  ok: boolean
  safe: boolean
  risk: string
  note: string
}

/** 系统调优体检 / 应用结果。 */
export interface TuneReport {
  supported: boolean
  can_apply: boolean
  applied: boolean
  bbr_available: boolean
  mem_mb: number
  in_container: boolean
  conf_path: string
  items: TuneItem[]
  warnings: string[]
}

/** 单个来源 IP 的连接统计（连接数诊断）。 */
export interface ConnRow {
  source_ip: string
  conns: number
  new_last_min: number
  level: string // ok / watch / high
  top_hosts: string[]
}

/** 连接数诊断结果。 */
export interface ConnReport {
  supported: boolean
  reason: string
  total: number
  has_start: boolean
  mask_on: boolean
  watch_conns: number
  high_conns: number
  updated_at: string
  users: ConnRow[]
  advice: string
}
