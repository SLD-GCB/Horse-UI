@echo off
setlocal enabledelayedexpansion

REM ============================================================
REM  HorseUI 打包脚本（安装包 + 更新文件一次出齐）
REM  用法: 双击运行，按提示输入版本号
REM        或  build-linux.bat [amd64^|arm64^|both] [版本号]
REM
REM  产物:
REM    service\dist\horseui-<版本>-linux-<架构>.zip   安装包（含内核 + install.sh）
REM    service\dist\horseui.new                       更新用二进制（菜单「更新面板」读它）
REM    service\dist\archive\horseui-update-<版本>-linux-<架构>.new   带版号的留档副本
REM
REM  版本号会同时写进二进制（-X main.version），装/更新后面板里显示的就是它。
REM  安装时会自动做一次系统调优（UDP 缓冲 / BBR），可用 HORSEUI_TUNE=0 跳过。
REM ============================================================

cd /d "%~dp0"
cd ..
set "ROOT=%CD%"

set "WHAT=%~1"
if "%WHAT%"=="" set "WHAT=both"
if /i not "%WHAT%"=="amd64" if /i not "%WHAT%"=="arm64" if /i not "%WHAT%"=="both" (
  echo [警告] 无法识别的参数 "%WHAT%"，将按 both 打包
  set "WHAT=both"
)

REM ---------- 版本号 ----------
REM 默认值取自 back-machine\main.go 的 var version；双击运行时手动输入，回车沿用默认值。
REM 用绝对路径调 findstr，避开本机 PATH 异常导致取不到版本号。
set "DEFVER="
for /f "tokens=2 delims==" %%a in ('%SystemRoot%\System32\findstr.exe /r /c:"^var version" "back-machine\main.go" 2^>nul') do set "DEFVER=%%a"
if defined DEFVER set DEFVER=%DEFVER:"=%
if defined DEFVER set DEFVER=%DEFVER: =%
if not defined DEFVER set "DEFVER=dev"

set "VER=%~2"
if not defined VER (
  echo.
  echo ============================================================
  echo   当前源码版本: %DEFVER%
  echo ============================================================
  set /p "VER=请输入本次打包的版本号（直接回车沿用 %DEFVER%）: "
)
if not defined VER set "VER=%DEFVER%"
set "VER=%VER: =%"
if not defined VER set "VER=dev"

REM ---------- 前置检查: 只有 install.sh 必须存在 ----------
REM horseui.service / README-install.txt 缺失时脚本会就地生成，不再因此打包失败。
if not exist "service\install.sh" (
  echo [错误] 缺少 service\install.sh，无法打包（安装脚本是这个仓库的核心逻辑，不做自动生成）
  goto :fail
)

REM ---------- 定位 tar.exe（Win10 1803+ 自带，可写 zip） ----------
set "TAR="
if exist "%SystemRoot%\System32\tar.exe" set "TAR=%SystemRoot%\System32\tar.exe"
if not defined TAR for %%t in (tar.exe) do if not "%%~$PATH:t"=="" set "TAR=%%~$PATH:t"
if not defined TAR (
  echo [错误] 未找到 tar.exe（Windows 10 1803 及以上自带）
  goto :fail
)

echo.
echo ============================================================
echo   HorseUI 打包    版本 %VER%
echo   目标架构: %WHAT%
echo ============================================================
echo.

go version >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 go，请先安装 Go 1.22 或更高版本并加入 PATH
  goto :fail
)

REM ---------- 1/3 构建前端 ----------
if exist "front-UI\node_modules\vite" (
  echo [1/3] 前端依赖已存在，跳过安装
) else (
  echo [1/3] 安装前端依赖 ...
  call npm --prefix "front-UI" install --no-audit --no-fund --registry=https://registry.npmmirror.com
  if errorlevel 1 (
    echo [错误] 前端依赖安装失败
    goto :fail
  )
)
echo [1/3] 构建前端 ...
pushd "front-UI"
call node ".\node_modules\vite\bin\vite.js" build
if errorlevel 1 (
  popd
  echo [错误] 前端构建失败
  goto :fail
)
popd
if not exist "back-machine\internal\webui\dist\index.html" (
  echo [错误] 前端产物未生成，构建中止
  goto :fail
)

REM ---------- 2/3 交叉编译 ----------
set "ARCHLIST=amd64 arm64"
if /i "%WHAT%"=="amd64" set "ARCHLIST=amd64"
if /i "%WHAT%"=="arm64" set "ARCHLIST=arm64"

echo [2/3] 交叉编译 Linux 二进制（版本 %VER%）...
if not exist "service\dist" mkdir "service\dist"
for %%A in (%ARCHLIST%) do (
  echo   - linux/%%A
  set "GOOS=linux"
  set "GOARCH=%%A"
  set "GOFLAGS=-mod=mod"
  set "GOPROXY=https://goproxy.cn,direct"
  set "CGO_ENABLED=0"
  go build -trimpath -ldflags "-s -w -X main.version=%VER%" -o "service\dist\horseui-linux-%%A" ".\back-machine"
  if errorlevel 1 (
    echo [错误] linux/%%A 编译失败
    goto :fail
  )
)

REM ---------- 3/3 打包 ----------
echo [3/3] 打包安装包 ...
set "OUT=%ROOT%\service\dist"
set "PKGTMP=%OUT%\.pkgtmp"
set "MISSING="
REM 保险: 只允许删除自己约定的暂存目录，防止变量异常时误删
if not "%PKGTMP%"=="%OUT%\.pkgtmp" (
  echo [错误] 暂存目录变量异常："%PKGTMP%"，已中止以免误删
  goto :fail
)
if exist "%PKGTMP%" rmdir /s /q "%PKGTMP%"
mkdir "%PKGTMP%"
for %%A in (%ARCHLIST%) do call :pack %%A
rmdir /s /q "%PKGTMP%"

REM ---------- 4/4 同时产出更新用二进制 ----------
REM 菜单「更新面板」默认读 /usr/local/horseui/horseui.new，名字不能改，所以一次只能一个架构：
REM both 时用 amd64，arm64 的留在 archive 里，需要时改名上传。
set "UPDARCH=amd64"
if /i "%WHAT%"=="arm64" set "UPDARCH=arm64"
if exist "%OUT%\horseui-linux-%UPDARCH%" (
  copy /y "%OUT%\horseui-linux-%UPDARCH%" "%OUT%\horseui.new" >nul
  if not exist "%OUT%\archive" mkdir "%OUT%\archive"
  for %%A in (%ARCHLIST%) do (
    copy /y "%OUT%\horseui-linux-%%A" "%OUT%\archive\horseui-update-%VER%-linux-%%A.new" >nul
  )
  echo [4/4] 更新文件已生成：horseui.new（架构 %UPDARCH%）
) else (
  echo [警告] 未找到 %UPDARCH% 的二进制，跳过生成 horseui.new
)

echo.
echo ============================================================
echo   打包完成，产物在 service\dist\ ：
echo.
dir /b "%OUT%\horseui-*.zip"
echo   horseui.new                             ^<- 更新用，上传到 /usr/local/horseui/
echo   archive\horseui-update-%VER%-linux-*.new  ^<- 带版号的留档副本
echo.
if defined MISSING (
  echo   [重要] 以下架构未随包提供 sing-box 内核：%MISSING%
  echo          装好后需要到面板「系统与内核 - 安装/更新内核」里手动安装
  echo          缺内核的包体积会明显偏小，请确认这是你想要的
  echo.
)
echo   本次版本: %VER%（已写入二进制，装/更新后面板显示的就是这个版本）
echo.
echo   A. 全新安装
echo     1. 上传 horseui-^<版本^>-linux-^<架构^>.zip 到服务器
echo     2. unzip 包名.zip     若没装 unzip 就用  python3 -m zipfile -e 包名.zip .
echo     3. cd 解压出来的目录
echo     4. sudo bash service/install.sh
echo      安装时会自动做系统调优（UDP 缓冲 / BBR），跳过用： sudo HORSEUI_TUNE=0 bash service/install.sh
echo.
echo   B. 已有面板升级（只换程序，不动数据和内核）
echo     1. scp horseui.new root@服务器IP:/usr/local/horseui/
echo     2. 服务器上执行 sudo horseui，选「更新面板」，路径直接回车
echo     3. 退出菜单后再敲 horseui 就是新版本
echo.
echo   包内已含面板二进制与 sing-box 内核，服务器无需安装 Go / Node
echo   装完后可在面板里看到：行为抹除、系统调优（含 BBR 滑块）、仪表盘连接数诊断
echo ============================================================
echo [PACK-OK]
pause
exit /b 0

REM ============================================================
REM  子过程: 打包单个架构
REM ============================================================
:pack
set "A=%~1"
set "PKG=horseui-%VER%-linux-%A%"
set "STAGE=%PKGTMP%\%PKG%"
mkdir "%STAGE%\service\dist" 2>nul

copy /y "service\install.sh" "%STAGE%\service\install.sh" >nul
if errorlevel 1 (
  echo   [错误] 复制 install.sh 失败
  goto :fail
)

REM systemd 单元：优先用仓库里的，缺失时按内置模板生成（纯 ASCII，避免编码问题）
if exist "service\horseui.service" (
  copy /y "service\horseui.service" "%STAGE%\service\horseui.service" >nul
) else (
  echo   ! 未找到 service\horseui.service，已按内置模板生成
  call :genService "%STAGE%\service\horseui.service"
)

REM 说明文件：优先用仓库里的，缺失时生成一份精简版
if exist "service\README-install.txt" (
  copy /y "service\README-install.txt" "%STAGE%\README-install.txt" >nul
) else (
  echo   ! 未找到 service\README-install.txt，已生成精简说明
  call :genReadme "%STAGE%\README-install.txt"
)

copy /y "%OUT%\horseui-linux-%A%" "%STAGE%\service\dist\horseui-linux-%A%" >nul
if errorlevel 1 (
  echo   [错误] 缺少 %OUT%\horseui-linux-%A%
  goto :fail
)

REM 内核包：复制后必须确认真的进了暂存目录，否则记入 MISSING 并在最后大声提示
set "GOTK="
if exist "%OUT%\sing-box*-linux-%A%.tar.gz" (
  for %%K in ("%OUT%\sing-box*-linux-%A%.tar.gz") do (
    copy /y "%%~fK" "%STAGE%\service\dist\" >nul
    set "GOTK=1"
    echo   + 内核 %%~nxK
  )
)
if not defined GOTK (
  set "MISSING=%MISSING% %A%"
  echo   ! 警告: 未找到 %A% 的内核包
)
if not exist "%STAGE%\service\dist\sing-box*.tar.gz" (
  set "MISSING=%MISSING% %A%"
  echo   ! 警告: %A% 的内核包未成功放入暂存目录
)

REM 用 bsdtar 的 -a 按扩展名自动选 zip 格式，避免额外依赖
"%TAR%" -a -cf "%OUT%\%PKG%.zip" -C "%PKGTMP%" "%PKG%"
if errorlevel 1 (
  echo   [错误] 打包 %PKG% 失败
  goto :fail
)
echo   = %PKG%.zip
goto :eof

REM ============================================================
REM  子过程: 生成 systemd 单元（ASCII，UTF-8 安全）
REM ============================================================
:genService
(
echo [Unit]
echo Description=HorseUI sing-box proxy panel
echo After=network.target nss-lookup.target
echo.
echo [Service]
echo Type=simple
echo WorkingDirectory=/etc/horseui
echo ExecStart=/usr/local/horseui/horseui -run -data /etc/horseui -listen 0.0.0.0:8080
echo Restart=on-failure
echo RestartSec=3
echo LimitNOFILE=1048576
echo LimitNPROC=1048576
echo # panel needs to manage the sing-box child process and listening ports
echo AmbientCapabilities=CAP_NET_BIND_SERVICE
echo CapabilityBoundingSet=CAP_NET_BIND_SERVICE
echo.
echo [Install]
echo WantedBy=multi-user.target
) > "%~1"
goto :eof

REM ============================================================
REM  子过程: 生成精简安装说明（纯 ASCII，UTF-8 安全）
REM ============================================================
:genReadme
(
echo HorseUI - Linux install notes
echo =============================
echo.
echo 1. Install ^(run from the extracted package root^):
echo      sudo bash service/install.sh
echo    The script installs the panel binary, the bundled sing-box kernel,
echo    the systemd unit, and applies kernel tuning ^(UDP buffers / BBR^).
echo    Skip tuning with:  sudo HORSEUI_TUNE=0 bash service/install.sh
echo.
echo 2. Panel address:
echo      https://^<server-ip^>:8080      user: admin
echo    HTTPS is on by default with a self-signed cert ^(browser warning is normal^).
echo    The initial password is printed once at first start:
echo      journalctl -u horseui --no-pager ^| grep -i password
echo.
echo 3. Open the panel port in your firewall / cloud security group,
echo    plus every node port you create later ^(TCP and UDP^).
echo.
echo 4. Daily management ^(numeric menu^):
echo      horseui
echo    start / stop / restart / status / show credentials / reset password /
echo    kernel / logs / update / uninstall / system tuning
echo.
echo 5. The package already contains the panel binary and sing-box kernel;
echo    Go / Node are not required on the server.
) > "%~1"
goto :eof

:fail
echo [PACK-FAILED]
pause
exit /b 1
