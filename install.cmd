@echo off
setlocal EnableExtensions

rem Kumiho installer wrapper: captures the invoking identity before UAC
rem elevation, because under an over-the-shoulder prompt the elevated process
rem is the admin, not the person who started this file. The identity is passed
rem as -User (the login entry, D11, and the pipe grant, D14, belong to that
rem user) unless the caller already supplied one; every other argument is
rem forwarded unchanged. Exits with install.ps1's exit code.
rem
rem This file must keep CRLF line endings: .gitattributes carries
rem "install.cmd text eol=crlf" for it, because cmd.exe's goto/call label
rem scan can miss a label in an LF-only batch file (a label that straddles a
rem 512-byte read boundary is skipped and the script then dies mid-run).
rem Labels stay at the top level, as :collect and :elevate are here, where
rem the scan finds them reliably; a label inside an if/for block is not safe.
rem
rem Values are embedded in PowerShell single-quoted literals, so apostrophes
rem are doubled first (a profile path such as C:\Users\O'Brien\... stays
rem intact). A literal double quote inside a forwarded argument cannot be
rem represented on a Windows command line and is not supported.

set "INSTALL_USER=%USERDOMAIN%\%USERNAME%"
set "INSTALL_USER=%INSTALL_USER:'=''%"
set "SCRIPT=%~dp0install.ps1"
set "SCRIPT=%SCRIPT:'=''%"
set "USER_GIVEN="
set "PS_ARGS="

:collect
if "%~1"=="" goto elevate
set "ONE=%~1"
if /i "%ONE:~0,5%"=="-User" set "USER_GIVEN=1"
set "ONE=%ONE:'=''%"
set "PS_ARGS=%PS_ARGS%,'%ONE%'"
shift
goto collect

:elevate
if not defined USER_GIVEN set "PS_ARGS=%PS_ARGS%,'-User','%INSTALL_USER%'"

rem Elements are re-quoted when they contain a space: Windows PowerShell 5.1
rem joins -ArgumentList with spaces without adding quotes itself.
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$q=[char]34; $a=@('-NoProfile','-ExecutionPolicy','Bypass','-File','%SCRIPT%'%PS_ARGS%); $a=@($a | ForEach-Object { if ($_ -match ' ') { $q+$_+$q } else { $_ } }); $p=Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -ErrorAction Stop -ArgumentList $a; exit $p.ExitCode"
exit /b %ERRORLEVEL%
