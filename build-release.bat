@echo off
rem Double-click to build a release into release\<version>. The work and its messages are in build-release.ps1.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-release.ps1" %*
