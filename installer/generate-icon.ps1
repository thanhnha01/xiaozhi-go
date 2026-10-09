$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$folder = Join-Path (Get-Location).Path 'installer'
New-Item -ItemType Directory -Force -Path $folder | Out-Null
$b = New-Object System.Drawing.Bitmap(256,256)
$g = [System.Drawing.Graphics]::FromImage($b)
$g.SmoothingMode=[System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$g.Clear([System.Drawing.Color]::Transparent)
$rect=New-Object System.Drawing.Rectangle(8,8,240,240)
$bg=New-Object System.Drawing.Drawing2D.LinearGradientBrush($rect,([System.Drawing.Color]::FromArgb(104,75,228)),([System.Drawing.Color]::FromArgb(17,185,221)),45)
$g.FillEllipse($bg, $rect)
$glow=New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(236,244,255))
$g.FillEllipse($glow,50,59,156,149)
$blush=New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(255,163,190))
$g.FillEllipse($blush,69,159,26,12);$g.FillEllipse($blush,161,159,26,12)
$eyes=New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(42,37,91))
$g.FillEllipse($eyes,86,113,21,32);$g.FillEllipse($eyes,151,113,21,32)
$white=New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::White)
$g.FillEllipse($white,91,119,7,10);$g.FillEllipse($white,156,119,7,10)
$pen=New-Object System.Drawing.Pen (([System.Drawing.Color]::FromArgb(70,48,128)),5)
$g.DrawArc($pen,118,144,22,15,0,180)
$headset=New-Object System.Drawing.Pen (([System.Drawing.Color]::FromArgb(15,48,117)),13)
$g.DrawArc($headset,57,51,144,150,195,150)
$phones=New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(37,83,204))
$g.FillEllipse($phones,44,129,33,59);$g.FillEllipse($phones,179,129,33,59)
$g.FillEllipse($white,82,38,22,31);$g.FillEllipse($white,151,38,22,31)
$g.Dispose()
$bg.Dispose();$glow.Dispose();$blush.Dispose();$eyes.Dispose();$white.Dispose();$pen.Dispose();$headset.Dispose();$phones.Dispose()
$png=Join-Path $folder 'xiaozhi.png'
$b.Save($png,[System.Drawing.Imaging.ImageFormat]::Png)
$b.Dispose()
# ICO format with PNG-encoded 256x256 image (Windows Vista and later).
$payload=[IO.File]::ReadAllBytes($png)
$ico=Join-Path $folder 'xiaozhi.ico'
$fs=[IO.File]::Open($ico,[IO.FileMode]::Create,[IO.FileAccess]::Write)
$w=New-Object IO.BinaryWriter($fs)
$w.Write([uint16]0);$w.Write([uint16]1);$w.Write([uint16]1)
$w.Write([byte]0);$w.Write([byte]0);$w.Write([byte]0);$w.Write([byte]0)
$w.Write([uint16]1);$w.Write([uint16]32)
$w.Write([uint32]$payload.Length);$w.Write([uint32]22)
$w.Write($payload)
$w.Dispose();$fs.Dispose()
Write-Host "Generated $ico"
