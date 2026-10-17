HorseUI Linux 安装说明
======================

一、安装
  解压后进入目录，执行：
      sudo bash service/install.sh
  脚本会自动：装面板二进制、装 sing-box 内核、写 systemd 服务并启动。

  注意：请用 bash 调用脚本（不要依赖文件的执行权限，zip 解压后通常没有 x 位）。
  若服务器没有 unzip，可用： python3 -m zipfile -e 包名.zip .

二、查看初始密码（面板随机生成，只打印一次）
      journalctl -u horseui --no-pager | grep -i '密码'

三、访问面板
      https://<服务器IP>:8080       账号 admin
  面板默认开启 HTTPS（首次启动自动生成自签证书），浏览器提示"证书不受信任"属正常现象。
  有域名后可在「证书管理」申请 ACME 证书替换。

四、放行端口
      ufw allow 8080/tcp
  云服务器还需要在控制台的安全组里放行；节点端口按需放行 TCP+UDP（TUIC / Hysteria2 走 UDP）。

五、日常管理（数字菜单：启动 / 停止 / 重启 / 状态 / 查看账号密码 / 重置密码 / 内核管理 / 日志 / 更新 / 卸载 / 系统调优）
      horseui

六、说明
  - 本包已包含面板二进制与 sing-box 内核，服务器无需安装 Go / Node
  - 若安装时提示未随包提供内核，可在面板「系统与内核 → 安装/更新内核」中安装
  - 更新面板：把新的二进制放到任意路径，然后在菜单里选「9. 更新面板」
  - 弱网优化：安装时默认会调优内核参数（UDP 缓冲 / BBR），写入
      /etc/sysctl.d/99-horseui.conf。想跳过加 HORSEUI_TUNE=0；
      想回滚用面板「设置 → 系统调优」或菜单「11」，不会锁死面板。
      注意：Hysteria2 / TUIC 走 QUIC，BBR 对它们无效，真正有用的是 UDP 缓冲
      与入站里的「忽略客户端带宽 + 带宽值」（服务端 Brutal，丢包不减速）。
