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
echo asm - AIR SDK Manager
echo.
echo Usage: asm COMMAND
echo.
echo Commands:
echo   help [COMMAND]  Show help.
echo   --version    Show the asm version. Alias: -v.
echo   list         List SDKs from AIR SDK Manager settings. Alias: ls.
echo   search [VERSION]  Search available stable AIR SDK versions.
echo   update [VERSION] [--all] [--check]  Check or apply SDK updates.
echo   install VERSION  Install a stable branch or a specific SDK build.
echo.
echo Options:
echo   -h, --help   Show help.
exit /b 0

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
echo Usage: asm list
echo Alias: asm ls
echo.
echo Reads AIR_SDKS from "%USERPROFILE%\.airsdk\airsdkmanager.cfg".
echo Lists SDKs in the configured directory's immediate subfolders.
echo Output: SDK version and its absolute directory.
exit /b 0

:search
if /i "%~2"=="--help" goto search_help
if /i "%~2"=="-h" goto search_help
if not "%~3"=="" goto unexpected_arguments
set "ASM_SEARCH_VERSION=%~2"
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0sdk.ps1" search
exit /b %errorlevel%

:search_help
if not "%~3"=="" goto unexpected_arguments
echo Usage: asm search [VERSION]
echo.
echo Searches stable SDK releases in the official AIR SDK announcement archive.
echo VERSION can be a branch such as 51.4 or a full build such as 51.4.1.1.
echo Uses the manager's cached catalog if the release source is unavailable.
exit /b 0


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
echo Usage: asm update [VERSION] [--all] [--check] [--accept-license]
echo.
echo No arguments: show installed SDK updates and a new SDK branch, if available.
echo --all: update all installed SDKs that have a newer build.
echo VERSION: update matching installed SDKs. Example: asm update 51.3.
echo --check: only show updates, including when VERSION or --all is given.
echo --accept-license: accept the AIR SDK license for this operation.
echo Updates keep each SDK's three-component version and existing path.
echo New branches are announced with an asm install command.
exit /b 0


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
echo Usage: asm install VERSION [--accept-license]
echo.
echo VERSION: a branch such as 51.4, a full build such as 51.4.1.1, or latest.
echo Installs into AIR_SDKS from AIR SDK Manager settings.
echo An already installed build is kept and reported.
echo --accept-license: accept the AIR SDK license for this operation.
exit /b 0

:unexpected_arguments
>&2 echo Error: Unexpected arguments. Run asm help.
exit /b 1
