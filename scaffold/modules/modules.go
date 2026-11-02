// Package modules 登记应用已安装的 lynx-mods 模块。
// 本文件由 lynx-mods add 维护（安装清单见仓库根 lynx-mods.json）；锚点注释勿删。
package modules

import (
	"github.com/lynxland/appkit"
	// lynx-mods:module-import（lynx-mods add 注入锚点，勿删）
)

// Installed 返回全部已装模块（静态描述 + 动态装配）。
func Installed() []appkit.InstalledModule {
	return []appkit.InstalledModule{
		// lynx-mods:module-entry（lynx-mods add 注入锚点，勿删）
	}
}
