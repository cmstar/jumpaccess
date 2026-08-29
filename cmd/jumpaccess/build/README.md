# 桌面构建资源

本目录保存 JumpAccess 桌面应用的平台资源和构建输出。

- `appicon.svg`：图标的唯一设计源，使用“终端 chevron 穿过跳板网关”标记；只包含纯色几何路径。
- `appicon.png`：由 `appicon.svg` 以 1024×1024、透明背景渲染的构建输入；macOS 图标和 Windows 多尺寸图标都以它为源。
- `bin/`：本机构建输出。
- `darwin/`：macOS `Info.plist` 等资源。
- `windows/icon.ico`：Windows 256、128、64、48、32、16 像素图标。
- `windows/info.json`：Windows 版本信息。
- `windows/wails.exe.manifest`：Windows 应用 manifest。
- `windows/installer/`：NSIS 安装包资源。

更新 `appicon.svg` 后，先重新渲染 `appicon.png`，再移除旧的 `windows/icon.ico` 并执行：

```powershell
wails build
```

Wails 会重新生成 `windows/icon.ico` 并把图标、manifest 和版本信息嵌入 Windows EXE。用于发布或人工验收的 Windows 构建不要使用 `-nopackage`；该参数会跳过平台资源生成，产出的裸 EXE 没有应用图标和版本资源。
