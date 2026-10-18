@echo off
setlocal

REM ============================================================
REM  HorseUI 一键构建脚本（前端 + 后端） 生成 HorseUI.exe
REM  构建完成后会自动启动面板
REM ============================================================

cd /d "%~dp0"
cd ..
set "ROOT=%CD%"

REM ---------- 版本 ----------
set "DEFVER="
for /f "tokens=2 delims==" %%a in ('%SystemRoot%\System32\findstr.exe /r /c:"^var version" "back-machine\main.go" 2^>nul') do set "DEFVER=%%a"
if defined DEFVER set "DEFVER=%DEFVER:"=%"
if defined DEFVER set "DEFVER=%DEFVER: =%"
if not defined DEFVER set "DEFVER=dev"

set "VER="
set /p "VER=输入版本号（回车默认 %DEFVER%）: "
if not defined VER set "VER=%DEFVER%"
set "VER=%VER: =%"

echo.
echo ============================================================
echo   HorseUI 构建脚本
echo ============================================================
echo.

go version >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 go，请先安装 Go 1.22 或更高版本并加入 PATH
  goto :fail
)

node -v >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 node，请先安装 Node.js 18 或更高版本并加入 PATH
  goto :fail
)

REM ---------- 1/3 前端依赖 ----------
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

REM ---------- 2/3 构建前端 ----------
echo [2/3] 构建前端 ...
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

REM ---------- 3/3 编译后端 ----------
echo [3/3] 编译后端 HorseUI.exe ...
set "GOFLAGS=-mod=mod"
set "GOPROXY=https://goproxy.cn,direct"
set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w -X main.version=%VER%" -o "HorseUI.exe" ".\back-machine"
if errorlevel 1 (
  echo [错误] 后端编译失败
  goto :fail
)

echo.
echo ============================================================
echo   构建完成，正在启动面板 ...
echo.
echo   产物 HorseUI.exe：前端已内嵌，单文件即可部署
echo   面板已在独立窗口启动，浏览器会自动打开
echo   若已在运行，新窗口会提示端口被占用，直接关闭即可
echo   面板默认开启 HTTPS，首次启动自动生成自签证书
echo   初始管理员密码在面板窗口的启动日志中打印，请及时记录
echo   运行数据保存在 exe 同级的 data 目录
echo ============================================================
echo [BUILD-OK]

REM 直接启动面板（-run 跳过管理菜单）；已是启动状态时新窗口会报端口占用
start "" "%ROOT%\HorseUI.exe" -run -data "%ROOT%\data"

echo.
echo 管理面板可用：双击 HorseUI.exe 打开管理菜单（启动/停止/状态/重置密码/内核管理）
pause
exit /b 0

:fail
echo [BUILD-FAILED]
echo.
pause
exit /b 1
