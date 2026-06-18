param(
  [string]$AarPath = (Join-Path $PSScriptRoot "..\app\libs\mobilegonc.aar"),
  [string]$NdkRoot = $env:ANDROID_NDK_HOME
)

$ErrorActionPreference = "Stop"

$AarPath = [IO.Path]::GetFullPath($AarPath)
if (-not (Test-Path -LiteralPath $AarPath)) {
  throw "AAR not found: $AarPath"
}

if (-not $NdkRoot) {
  $SdkRoot = $env:ANDROID_SDK_ROOT
  if (-not $SdkRoot) {
    $SdkRoot = $env:ANDROID_HOME
  }
  if (-not $SdkRoot) {
    $SdkRoot = "D:\android\sdk"
  }
  $NdkRoot = Join-Path $SdkRoot "ndk\27.2.12479018"
}

$Strip = Join-Path $NdkRoot "toolchains\llvm\prebuilt\windows-x86_64\bin\llvm-strip.exe"
if (-not (Test-Path -LiteralPath $Strip)) {
  throw "llvm-strip not found: $Strip"
}

$Jar = $null
if ($env:JAVA_HOME) {
  $Jar = Join-Path $env:JAVA_HOME "bin\jar.exe"
}
if (-not $Jar -or -not (Test-Path -LiteralPath $Jar)) {
  $Jar = "C:\Program Files\Java\jdk-17\bin\jar.exe"
}
if (-not (Test-Path -LiteralPath $Jar)) {
  throw "jar.exe not found. Set JAVA_HOME to a JDK installation."
}

$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("mobilegonc-aar-strip-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force $WorkDir | Out-Null

try {
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  [IO.Compression.ZipFile]::ExtractToDirectory($AarPath, $WorkDir)

  $Libraries = Get-ChildItem -Path (Join-Path $WorkDir "jni") -Filter "libgojni.so" -Recurse -File
  if ($Libraries.Count -eq 0) {
    throw "No libgojni.so found in AAR: $AarPath"
  }

  foreach ($Library in $Libraries) {
    & $Strip --strip-unneeded $Library.FullName
    if ($LASTEXITCODE -ne 0) {
      throw "llvm-strip failed for $($Library.FullName)"
    }
  }

  $ResDir = Join-Path $WorkDir "res"
  if ((Test-Path -LiteralPath $ResDir) -and -not (Get-ChildItem -LiteralPath $ResDir -Recurse -Force)) {
    Remove-Item -LiteralPath $ResDir -Force
  }

  $BackupPath = "$AarPath.unstripped"
  if (-not (Test-Path -LiteralPath $BackupPath)) {
    Copy-Item -LiteralPath $AarPath -Destination $BackupPath -Force
  }
  $Before = (Get-Item -LiteralPath $AarPath).Length
  Remove-Item -LiteralPath $AarPath -Force

  & $Jar cf $AarPath -C $WorkDir .
  if ($LASTEXITCODE -ne 0) {
    throw "jar failed while repacking $AarPath"
  }

  $After = (Get-Item -LiteralPath $AarPath).Length
  Write-Host ("Stripped {0}: {1:N0} -> {2:N0} bytes" -f $AarPath, $Before, $After)
} finally {
  if (Test-Path -LiteralPath $WorkDir) {
    Remove-Item -LiteralPath $WorkDir -Recurse -Force
  }
}
