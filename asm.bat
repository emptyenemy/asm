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

>&2 echo Error: Unknown command "%~1". Run asm help.
exit /b 1

:help_command
if "%~2"=="" goto help
if /i "%~2"=="list" goto list_help
if /i "%~2"=="ls" goto list_help
if /i "%~2"=="search" goto search_help
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

:unexpected_arguments
>&2 echo Error: Unexpected arguments. Run asm help.
exit /b 1
