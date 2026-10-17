#!/usr/bin/env bash
#
# HorseUI 一键安装脚本（在仓库根目录执行）
#
#   sudo bash service/install.sh
#
# 安装优先级：
#   1) 若存在 service/dist/horseui-linux-<arch>（预编译二进制），直接安装，服务器无需 Go/Node
#   2) 否则从源码构建（需要服务器上有 go，构建前端还需要 npm）
#
# 可选环境变量：
#   HORSEUI_LISTEN   面板监听地址，默认 0.0.0.0:8080
#   SKIP_FRONTEND    源码构建时设为 1 跳过前端构建
#   HORSEUI_TUNE     设为 0 跳过系统调优（弱网优化，默认执行，可在面板回滚）
#
set -euo pipefail

APP="horseui"
INSTALL_DIR="/usr/local/horseui"
DATA_DIR="/etc/horseui"
SERVICE_FILE="/etc/systemd/system/${APP}.service"
LISTEN="${HORSEUI_LISTEN:-0.0.0.0:8080}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

log()  { echo -e "\033[32m[horseui]\033[0m $*"; }
warn() { echo -e "\033[33m[horseui]\033[0m $*"; }
err()  { echo -e "\033[31m[horseui]\033[0m $*" >&2; }

if [[ "$(id -u)" -ne 0 ]]; then
  err "请使用 root 运行：sudo bash service/install.sh"
  exit 1
fi

# ---------- 识别 CPU 架构（Go 命名） ----------
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv7)  ARCH="armv7" ;;
  *)             ARCH="$(uname -m)" ;;
esac
log "系统架构：$(uname -m) -> ${ARCH}"

# ---------- 1. 准备二进制（优先用预编译） ----------
PREBUILT=""
for cand in "${ROOT_DIR}/service/dist/horseui-linux-${ARCH}" "${ROOT_DIR}/horseui-linux-${ARCH}"; do
  if [[ -f "$cand" ]]; then PREBUILT="$cand"; break; fi
done

mkdir -p "$INSTALL_DIR"

if [[ -n "$PREBUILT" ]]; then
  log "使用预编译二进制：${PREBUILT}"
  install -m 0755 "$PREBUILT" "${INSTALL_DIR}/${APP}.new"
else
  # 预编译包通常只带一个架构；架构不匹配时给出明确提示，而不是跌进源码构建。
  if [[ ! -d "${ROOT_DIR}/back-machine" ]]; then
    err "未找到本机架构（${ARCH}，uname -m = $(uname -m)）的预编译二进制，且本包不含源码，无法继续"
    err "请下载与该架构匹配的安装包，或用源码包在本机构建"
    exit 1
  fi
  warn "未找到 service/dist/horseui-linux-${ARCH}，改为从源码构建"
  command -v go >/dev/null 2>&1 || { err "未找到 go，请安装 Go 1.22+ 或改用预编译二进制"; exit 1; }

  if [[ "${SKIP_FRONTEND:-0}" != "1" ]]; then
    if command -v npm >/dev/null 2>&1; then
      log "构建前端 ..."
      ( cd front-UI && npm install --no-audit --no-fund && npm run build )
    else
      warn "未找到 npm，跳过前端构建（将使用已内嵌的现有产物）"
    fi
  else
    log "按配置跳过前端构建"
  fi

  log "构建后端 ..."
  GOFLAGS=-mod=mod CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "${INSTALL_DIR}/${APP}.new" ./back-machine
fi

# 先停掉旧服务，避免覆盖正在运行的二进制失败（Windows 无此问题，Linux 上运行中的文件也不能被覆盖）
if systemctl is-active --quiet "${APP}.service" 2>/dev/null; then
  log "停止旧服务 ..."
  systemctl stop "${APP}.service" || true
fi
mv -f "${INSTALL_DIR}/${APP}.new" "${INSTALL_DIR}/${APP}"
chmod 0755 "${INSTALL_DIR}/${APP}"

# ---------- 2. 准备数据目录（保留已有数据） ----------
log "准备数据目录 ${DATA_DIR} ..."
mkdir -p "${DATA_DIR}"/{certs,logs,bin}

# ---------- 2.5 安装 sing-box 内核（随包提供则直接装，免去服务器从 GitHub 下载） ----------
# 随包形式：service/dist/sing-box-<版本>-linux-<arch>.tar.gz（官方发行包原样，含 libcronet.so）
shopt -s nullglob
kernels=( "${ROOT_DIR}"/service/dist/sing-box*-linux-"${ARCH}".tar.gz "${ROOT_DIR}"/service/dist/sing-box-linux-"${ARCH}".tar.gz )
shopt -u nullglob
KERNEL_PKG="${kernels[0]:-}"

KERNEL_OK=0
if [[ -n "$KERNEL_PKG" ]]; then
  log "安装 sing-box 内核：$(basename "$KERNEL_PKG")"
  # 清掉旧内核再解压，避免残留不匹配的版本
  rm -f "${DATA_DIR}/bin/sing-box" "${DATA_DIR}/bin/libcronet.so"
  if tar -xzf "$KERNEL_PKG" -C "${DATA_DIR}/bin" --strip-components=1 2>/dev/null; then
    chmod 0755 "${DATA_DIR}/bin/sing-box" 2>/dev/null || true
    [[ -x "${DATA_DIR}/bin/sing-box" ]] && KERNEL_OK=1
  fi
  if [[ "$KERNEL_OK" != "1" ]]; then
    warn "内核包解压失败，请改用面板「系统与内核 → 安装/更新内核」"
  fi
elif [[ -x "${DATA_DIR}/bin/sing-box" ]]; then
  log "沿用已有内核：${DATA_DIR}/bin/sing-box"
  KERNEL_OK=1
else
  warn "未随包提供 sing-box 内核，装完后请在面板「系统与内核 → 安装/更新内核」中安装"
fi

# ---------- 2.6 系统调优（弱网优化，可跳过） ----------
# 只改内核参数（UDP 缓冲 / BBR / 连接队列），写入 /etc/sysctl.d/99-horseui.conf，
# 面板「设置 → 系统调优」或管理菜单「11」都能一键回滚。
#   HORSEUI_TUNE=0  跳过本步骤
# 容器 / 只读 /proc/sys 等场景失败属正常，绝不中断安装。
if [[ "${HORSEUI_TUNE:-1}" != "0" ]]; then
  log "系统调优（弱网优化）..."
  if "${INSTALL_DIR}/${APP}" -tune apply -data "${DATA_DIR}" 2>&1 | sed 's/^/  /'; then
    log "系统调优完成（如需还原：面板「系统调优 → 回滚」或菜单选 11）"
  else
    warn "系统调优未完全成功，已跳过（不影响安装，可稍后在面板里重试）"
  fi
fi

# ---------- 3. 安装并启动 systemd 服务 ----------
log "安装 systemd 服务 ..."
sed "s#-listen 0.0.0.0:8080#-listen ${LISTEN}#" "$ROOT_DIR/service/${APP}.service" > "$SERVICE_FILE"
systemctl daemon-reload
systemctl enable "${APP}.service" >/dev/null
systemctl restart "${APP}.service"

# ---------- 3.5 安装 horseui 管理命令（敲 horseui 即出数字菜单） ----------
log "安装管理命令 /usr/local/bin/${APP} ..."
cat > "/usr/local/bin/${APP}" <<EOF
#!/usr/bin/env bash
# HorseUI 管理菜单（由 install.sh 生成，勿手工修改）
BIN="${INSTALL_DIR}/${APP}"
DATA="${DATA_DIR}"
if [[ \$# -eq 0 ]]; then
  exec "\$BIN" -menu -data "\$DATA"
fi
exec "\$BIN" -data "\$DATA" "\$@"
EOF
chmod 0755 "/usr/local/bin/${APP}"

# ---------- 4. 结果 ----------
sleep 2
PORT="${LISTEN##*:}"
if systemctl is-active --quiet "${APP}.service"; then
  log "安装完成，服务已启动 ✅"
else
  err "服务启动失败，请查看： journalctl -u ${APP} -n 80 --no-pager"
  exit 1
fi

echo
log "面板地址： https://<服务器IP>:${PORT}"
log "  面板默认开启 HTTPS，首次启动会自动生成自签证书，浏览器提示证书不受信任属正常现象"
log "  有域名后可在「证书管理」申请 ACME 证书替换，并在「面板设置 → 安全设置」调整"
log "  如需改端口，在「面板设置 → 面板监听地址」填写后重启服务"
echo
log "管理员账号： admin"
log "初始密码：首次启动随机生成，仅打印一次，用下面命令查看："
log "  journalctl -u ${APP} --no-pager | grep -i '密码'"
echo
if [[ "$KERNEL_OK" == "1" ]]; then
  log "sing-box 内核：已就位（${DATA_DIR}/bin/sing-box）"
else
  warn "sing-box 内核：尚未安装，请先到面板「系统与内核」中安装，否则新建入站无法生效"
fi
echo
log "节点端口：在「入站管理」新建入站时会自动从端口池分配，记得在防火墙/安全组放行"
log "  ufw 示例：  ufw allow ${PORT}/tcp  并放行各节点端口（TCP+UDP）"
log "  firewalld： firewall-cmd --permanent --add-port=${PORT}/tcp && firewall-cmd --reload"
echo
log "服务管理： systemctl status|restart|stop ${APP}"
log "查看日志： journalctl -u ${APP} -f"
log "管理菜单： 直接输入  horseui  即可（启动/停止/状态/查看账号密码/重置密码/内核管理/更新/卸载）"
