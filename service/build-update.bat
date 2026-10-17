@echo off
setlocal enabledelayedexpansion

REM ============================================================
REM  HorseUI Linux 更新二进制构建脚本
REM  用法: 双击运行，按提示输入版本号
REM        或  build-update.bat [amd64^|arm64] [版本号]
REM  产物: service\dist\horseui.new（用于上传）
REM        service\dist\archive\horseui-update-<版本>-linux-<架构>.new（留档）
REM
REM  为什么只是一个文件、不打包：
REM    服务器上更新走面板菜单「更新面板」，它默认读取
REM      /usr/local/horseui/horseui.new
REM    然后自动停服、把旧程序备份成 horseui.old、替换、重启。
REM    所以上传用的文件必须叫 horseui.new；版号写在二进制里，更新后面板会显示。
REM ============================================================

cd /d "%~dp0"
cd ..
set "ROOT=%CD%"

set "ARCH=%~1"
if "%ARCH%"=="" set "ARCH=amd64"
if /i "%ARCH%"=="both" (
  echo [错误] 更新产物名固定为 horseui.new，一次只能出一个架构
  echo        请分别执行： build-update.bat amd64   和   build-update.bat arm64
  goto :fail
)
if /i not "%ARCH%"=="amd64" if /i not "%ARCH%"=="arm64" (
  echo [错误] 不支持的架构 "%ARCH%"（只支持 amd64 / arm64）
  goto :fail
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

echo.
echo ============================================================
echo   HorseUI 更新文件构建    版本 %VER%
echo   目标架构: linux/%ARCH%   -^>  horseui.new
echo ============================================================
echo.

go version >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 go，请先安装 Go 1.22 或更高版本并加入 PATH
  goto :fail
)

REM ---------- 1/2 构建前端 ----------
if exist "front-UI\node_modules\vite" (
  echo [1/2] 前端依赖已存在，跳过安装
) else (
  echo [1/2] 安装前端依赖 ...
  call npm --prefix "front-UI" install --no-audit --no-fund --registry=https://registry.npmmirror.com
  if errorlevel 1 (
    echo [错误] 前端依赖安装失败
    goto :fail
  )
)
echo [1/2] 构建前端 ...
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

REM ---------- 2/2 编译出 horseui.new ----------
echo [2/2] 交叉编译 linux/%ARCH% -^> horseui.new ...
if not exist "service\dist" mkdir "service\dist"
set "OUT=%ROOT%\service\dist\horseui.new"

set "GOOS=linux"
set "GOARCH=%ARCH%"
set "GOFLAGS=-mod=mod"
set "GOPROXY=https://goproxy.cn,direct"
set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w -X main.version=%VER%" -o "%OUT%" ".\back-machine"
if errorlevel 1 (
  echo [错误] linux/%ARCH% 编译失败
  goto :fail
)

REM ---------- 校验产物（与面板菜单的校验口径一致：存在、够大） ----------
if not exist "%OUT%" (
  echo [错误] 未生成 %OUT%
  goto :fail
)
set "SZ=0"
for %%S in ("%OUT%") do set "SZ=%%~zS"
if %SZ% LSS 1048576 (
  echo [错误] 产物异常：只有 %SZ% 字节，面板自己也会拒绝这种文件
  goto :fail
)

REM ---------- 留一份带版号的副本，方便回滚到指定版本 ----------
set "ARCHDIR=%ROOT%\service\dist\archive"
if not exist "%ARCHDIR%" mkdir "%ARCHDIR%"
set "ARCHIVE=%ARCHDIR%\horseui-update-%VER%-linux-%ARCH%.new"
copy /y "%OUT%" "%ARCHIVE%" >nul

echo.
echo ============================================================
echo   构建完成
echo.
echo   本次版本: %VER%（已写入二进制，更新后面板显示的就是这个版本）
echo.
echo   上传用文件（名字不能改，菜单默认读它）：
echo     service\dist\horseui.new
echo   留档副本（带版号，用于回滚）：
echo     service\dist\archive\horseui-update-%VER%-linux-%ARCH%.new
echo.
echo   服务器上更新（不用解压、不用手动停服，菜单自己处理）：
echo     1. 上传 horseui.new 到  /usr/local/horseui/
echo           scp horseui.new root@服务器IP:/usr/local/horseui/
echo     2. 在服务器执行  sudo horseui   然后选「更新面板」
echo           提示路径时直接回车，用默认的 /usr/local/horseui/horseui.new
echo     3. 更新完按提示退出菜单，再敲一次 horseui 就是新版本
echo.
echo   菜单会：停面板 -^> 旧程序备份成 horseui.old -^> 替换 -^> 重启
echo   注意：只换面板程序本身，sing-box 内核不动
echo.
echo   更新后可用（都在面板里，不需要额外上传文件）：
echo     - 设置 - 行为抹除：全协议多路复用 / UDP 混淆 / BBR 滑块 / 系统调优
echo     - 仪表盘 - 连接数诊断：按来源 IP 看并发连接，判断是否被按连接数限速
echo ============================================================
echo [BUILD-OK]
pause
exit /b 0

:fail
echo [BUILD-FAILED]
pause
exit /b 1
