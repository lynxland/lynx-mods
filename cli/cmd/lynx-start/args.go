package main

import "strings"

// reorderArgs 把位置参数挪到旗标之后（Go flag 的解析要求），保持
// 「值型旗标 + 值」的相邻关系。用户可以自然地写 `lynx-start init app --module x`。
func reorderArgs(valueFlags map[string]bool, args []string) []string {
	var flags, pos []string
	expectValue := false
	for _, a := range args {
		if expectValue {
			flags = append(flags, a)
			expectValue = false
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if idx := strings.Index(name, "="); idx >= 0 {
				continue // --x=v 自带值
			}
			if valueFlags[name] {
				expectValue = true
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}
