@echo off
setlocal enabledelayedexpansion

REM ============================================================
REM  HorseUI Linux 安装包打包脚本
REM  用法: 双击运行，或  build-linux.bat [amd64^|arm64^|both]
REM  产物: service\dist\horseui-<版本>-linux-<架构>.zip
REM
REM  包内含：面板二进制（前端已内嵌）、sing-box 内核、install.sh。
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

REM ---------- 版本号: 取自 back-machine\main.go 的 var version ----------
REM 用绝对路径调 findstr，避开本机 PATH 异常导致取不到版本号（否则会打成 horseui-dev-*.zip）
set "VER="
for /f "tokens=2 delims==" %%a in ('%SystemRoot%\System32\findstr.exe /r /c:"^var version" "back-machine\main.go" 2^>nul') do set "VER=%%a"
if defined VER set VER=%VER:"=%
if defined VER set VER=%VER: =%
if not defined VER set "VER=dev"

REM ---------- 前置检查: 安装包要带的东西必须都在 ----------
for %%F in (service\install.sh service\horseui.service service\README-install.txt) do (
  if not exist "%%F" (
    echo [错误] 缺少 %%F，无法打包安装包
    goto :fail
  )
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
echo   HorseUI Linux 打包    版本 %VER%
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

echo [2/3] 交叉编译 Linux 二进制 ...
if not exist "service\dist" mkdir "service\dist"
for %%A in (%ARCHLIST%) do (
  echo   - linux/%%A
  set "GOOS=linux"
  set "GOARCH=%%A"
  set "GOFLAGS=-mod=mod"
  set "GOPROXY=https://goproxy.cn,direct"
  set "CGO_ENABLED=0"
  go build -trimpath -ldflags "-s -w" -o "service\dist\horseui-linux-%%A" ".\back-machine"
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

echo.
echo ============================================================
echo   打包完成，产物在 service\dist\ ：
echo.
dir /b "%OUT%\horseui-*.zip"
echo.
if defined MISSING (
  echo   [重要] 以下架构未随包提供 sing-box 内核：%MISSING%
  echo          装好后需要到面板「系统与内核 - 安装/更新内核」里手动安装
  echo          缺内核的包体积会明显偏小，请确认这是你想要的
  echo.
)
echo   服务器上安装：
echo     1. 上传该 zip 到服务器
echo     2. unzip 包名.zip     若没装 unzip 就用  python3 -m zipfile -e 包名.zip .
echo     3. cd 解压出来的目录
echo     4. sudo bash service/install.sh
echo.
echo   包内已含面板二进制与 sing-box 内核，服务器无需安装 Go / Node
echo   安装时会自动做系统调优（UDP 缓冲 / BBR），跳过用： sudo HORSEUI_TUNE=0 bash service/install.sh
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

copy /y "service\install.sh"         "%STAGE%\service\install.sh"        >nul
copy /y "service\horseui.service"    "%STAGE%\service\horseui.service"   >nul
copy /y "service\README-install.txt" "%STAGE%\README-install.txt"        >nul
copy /y "%OUT%\horseui-linux-%A%"    "%STAGE%\service\dist\horseui-linux-%A%" >nul
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

:fail
echo [PACK-FAILED]
pause
exit /b 1
