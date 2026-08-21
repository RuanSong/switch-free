Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows you to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
## 
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the wails_tools.nsh file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "my-project" # Default "switch-free"
## !define INFO_COMPANYNAME    "Switch Dev" # Default "Switch Dev"
## !define INFO_PRODUCTNAME    "Switch Dev" # Default "Switch Dev"
## !define INFO_PRODUCTVERSION "0.1.0"     # Default "0.1.0"
## !define INFO_COPYRIGHT      "(c) 2025-2026, Switch Dev Contributors" # Default "© 2025-2026, Switch Dev Contributors"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
# 按用户安装（无需 UAC 提权，装到 %LOCALAPPDATA%\Programs）。
# 这样应用运行时（普通用户权限）对安装目录有写权限，自我更新
# （selfupdate 在同目录写 .exe.new）才不会被 Access denied 拒绝。
# 必须在 !include wails_tools.nsh 之前定义，模板据此设置 RequestExecutionLevel。
!define REQUEST_EXECUTION_LEVEL "user"
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.
# 安装完成页显示「启动 Switch Dev」勾选框（默认勾选），安装结束后自动拉起程序
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "启动 ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
# 按用户安装：装到 %LOCALAPPDATA%\Programs（当前用户可写，无需 UAC）。
# 这样自我更新时 selfupdate 在同目录写 .exe.new 才不会 Access denied。
InstallDir "$LOCALAPPDATA\Programs\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture

   # 旧版（<=0.1.1）是 admin 装到 Program Files 并写 HKLM 卸载项；0.1.2 起改为按用户装到
   # %LOCALAPPDATA%\Programs（写 HKCU）。两者卸载项 key 相同但 hive 不同，不会互相覆盖。
   # 检测到旧版残留时提示用户：避免「应用和功能」里出现两个 Switch Dev、以及 Program Files
   # 下残留旧二进制。仅提示不强制（静默安装 /S 时跳过弹窗）。
   IfSilent +13
   SetRegView 64
   ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallLocation"
   StrCmp $0 "" done_legacy_check
   # 旧版 InstallLocation 指向 Program Files 才提示（新安装位置不在 PF，理论上 HKLM 此 key 仅旧版会写）
   MessageBox MB_ICONEXCLAMATION|MB_OKCANCEL \
     "检测到旧版本 Switch Dev 安装在：$\r$\n$0$\r$\n$\r$\n旧版为管理员安装（Program Files），与本次按用户安装（%LOCALAPPDATA%\Programs）互不冲突，但「应用和功能」里会同时存在两个卸载项。$\r$\n$\r$\n建议：先点「取消」，到「应用和功能」卸载旧版 Switch Dev，再重新运行本安装器；或点「确定」继续安装（旧版残留需自行卸载清理）。$\r$\n$\r$\n注意：若看到 “Error opening file for writing ...\Program Files\...” 报错，说明你运行的是旧版安装器，请改用本次下载的最新安装包。" \
     /SD IDOK IDCANCEL abort_install
   Goto done_legacy_check
   abort_install:
     Abort
   done_legacy_check:
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR
    
    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    # 按用户安装：卸载信息写 HKCU（模板的 wails.writeUninstaller 硬编码 HKLM，
    # user 权限无提权会写失败，导致「应用和功能」里没有卸载项）。
    WriteUninstaller "$INSTDIR\uninstall.exe"
    SetRegView 64
    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" $0
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    # 按用户安装：删除 HKCU 卸载项（对应上面的写入）
    Delete "$INSTDIR\uninstall.exe"
    SetRegView 64
    DeleteRegKey HKCU "${UNINST_KEY}"
SectionEnd
