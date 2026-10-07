# 本机打一个发布包：双击 build-release.bat 运行它。
# 做的事和 GitHub 的 Release 工作流一样（wails3 task release）：检查版本号、
# 前端检查和测试、构建程序、go vet、Go 测试，最后在 release\<版本>\ 里写出
# zip、THIRD_PARTY_NOTICES.txt 和 SHA256SUMS.txt。版本号只在 build\config.yml 里。
#
#   build-release.bat                 按 build\config.yml 的版本
#   build-release.bat -Tag v0.2.0-rc.1  先核对标签，再按标签命名（可以带预发布后缀）
param(
    [string]$Tag = "",
    # 跑完不等按键，也不打开文件夹（给脚本调用）
    [switch]$NoPause
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

function Finish([int]$code) {
    if (-not $NoPause) {
        Write-Host ''
        Read-Host '按回车关闭' | Out-Null
    }
    exit $code
}

# 双击运行时 PATH 里可能没有 Go 和 wails3。
foreach ($dir in @("$env:ProgramFiles\Go\bin", "$env:USERPROFILE\go\bin")) {
    if ((Test-Path -LiteralPath $dir) -and (($env:Path -split ';') -notcontains $dir)) {
        $env:Path = "$dir;$env:Path"
    }
}
$missing = @('go', 'wails3', 'npm') | Where-Object { -not (Get-Command $_ -ErrorAction SilentlyContinue) }
if ($missing) {
    Write-Host "找不到：$($missing -join '、')。需要 Go 1.27、Node.js 24 和 Wails v3 命令行工具，见 CONTRIBUTING.md。" -ForegroundColor Red
    Finish 1
}

$version = (& go run tools/release/main.go -print-version $(if ($Tag) { '-tag'; $Tag }))
if ($LASTEXITCODE -ne 0 -or -not $version) {
    Write-Host '版本号不对，见上面的说明（版本号写在 build\config.yml 的 info.version）。' -ForegroundColor Red
    Finish 1
}
$version = "$version".Trim()
Write-Host "打包 RDP over Proxy $version，大约要两分钟……" -ForegroundColor Cyan
Write-Host ''

if ($Tag) { & wails3 task release "TAG=$Tag" } else { & wails3 task release }
if ($LASTEXITCODE -ne 0) {
    Write-Host ''
    Write-Host '打包失败，原因见上面第一条报错。' -ForegroundColor Red
    Finish $LASTEXITCODE
}

$out = Join-Path $PSScriptRoot "release\$version"
Write-Host ''
Write-Host "打包完成：$out" -ForegroundColor Green
Get-ChildItem -LiteralPath $out -File | ForEach-Object { '  {0}  ({1:N1} MB)' -f $_.Name, ($_.Length / 1MB) }
if (-not $NoPause) { Start-Process explorer.exe -ArgumentList "`"$out`"" }
Finish 0
