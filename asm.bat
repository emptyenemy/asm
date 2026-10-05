@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "ASM_VERSION=1.0.0"

if "%~1"=="" goto help
if /i "%~1"=="--help" goto help
if /i "%~1"=="-h" goto help
if /i "%~1"=="help" goto help_command
if /i "%~1"=="--version" goto version
if /i "%~1"=="-v" goto version
if /i "%~1"=="list" goto list
if /i "%~1"=="ls" goto list
if /i "%~1"=="search" goto search
if /i "%~1"=="update" goto update
if /i "%~1"=="install" goto install

>&2 echo Error: Unknown command "%~1". Run asm help.
exit /b 1

:help_command
if "%~2"=="" goto help
if /i "%~2"=="list" goto list_help
if /i "%~2"=="ls" goto list_help
if /i "%~2"=="search" goto search_help
if /i "%~2"=="update" goto update_help
if /i "%~2"=="install" goto install_help
>&2 echo Error: Unknown help topic "%~2".
exit /b 1

:help
if not "%~2"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" help
exit /b %errorlevel%

:version
if not "%~2"=="" goto unexpected_arguments
echo %ASM_VERSION%
exit /b 0

:list
if /i "%~2"=="--help" goto list_help
if /i "%~2"=="-h" goto list_help
if not "%~2"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1"
exit /b %errorlevel%

:list_help
if not "%~3"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" help list
exit /b %errorlevel%

:search
if /i "%~2"=="--help" goto search_help
if /i "%~2"=="-h" goto search_help
if not "%~3"=="" goto unexpected_arguments
set "ASM_SEARCH_VERSION=%~2"
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" search
exit /b %errorlevel%

:search_help
if not "%~3"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" help search
exit /b %errorlevel%


:update
if /i "%~2"=="--help" goto update_help
if /i "%~2"=="-h" goto update_help
set "ASM_UPDATE_VERSION="
set "ASM_UPDATE_ALL="
set "ASM_UPDATE_CHECK="
set "ASM_ACCEPT_LICENSE="
shift /1

:update_arguments
if "%~1"=="" goto update_run
if /i "%~1"=="--all" (
    set "ASM_UPDATE_ALL=1"
) else if /i "%~1"=="--check" (
    set "ASM_UPDATE_CHECK=1"
) else if /i "%~1"=="--accept-license" (
    set "ASM_ACCEPT_LICENSE=1"
) else (
    if defined ASM_UPDATE_VERSION goto unexpected_arguments
    set "ASM_UPDATE_VERSION=%~1"
)
shift /1
goto update_arguments

:update_run
if defined ASM_UPDATE_ALL if defined ASM_UPDATE_VERSION goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" update
exit /b %errorlevel%

:update_help
if not "%~3"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" help update
exit /b %errorlevel%


:install
if /i "%~2"=="--help" goto install_help
if /i "%~2"=="-h" goto install_help
set "ASM_INSTALL_VERSION="
set "ASM_ACCEPT_LICENSE="
shift /1

:install_arguments
if "%~1"=="" goto install_run
if /i "%~1"=="--accept-license" (
    set "ASM_ACCEPT_LICENSE=1"
) else (
    if defined ASM_INSTALL_VERSION goto unexpected_arguments
    set "ASM_INSTALL_VERSION=%~1"
)
shift /1
goto install_arguments

:install_run
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" install
exit /b %errorlevel%

:install_help
if not "%~3"=="" goto unexpected_arguments
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" help install
exit /b %errorlevel%

:unexpected_arguments
>&2 echo Error: Unexpected arguments. Run asm help.
exit /b 1
