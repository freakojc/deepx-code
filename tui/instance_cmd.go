package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"deepx/instance"
)

// instanceDirRe 匹配规范实例目录名 deepx_<数字>(前导零容忍由分配器统一处理)。
var instanceDirRe = regexp.MustCompile(`^deepx_[0-9]+$`)

// instanceMessage 组装 /instance 的输出:当前编号、实例环境根、父根、共享资产、
// 派生端口,以及"以 deepx_ 开头但不符合编号规范"的未识别目录(用户手建/改名,不参与自动分配)。
func instanceMessage() string {
	var b strings.Builder
	b.WriteString("**实例信息**\n\n")
	if wd, err := os.Getwd(); err == nil {
		b.WriteString(fmt.Sprintf("- 工作目录(项目):`%s`\n", wd))
	}
	if id := instance.InstanceID(); id != "" {
		b.WriteString(fmt.Sprintf("- 实例编号:`%s`(web 端口 `%d`)\n", id, webPort()))
	} else {
		b.WriteString("- 实例编号:未初始化(此进程未经 main 启动)\n")
	}
	b.WriteString(fmt.Sprintf("- 实例环境根:`%s`\n", instance.Home()))
	b.WriteString(fmt.Sprintf("- 父根:`%s`\n", instance.BaseRoot()))
	b.WriteString(fmt.Sprintf("- 共享资产:`%s`、`%s`\n", instance.SharedDir("ocr"), instance.SharedDir("router")))
	b.WriteString("- 实例-项目绑定登记表:`" + registryPath() + "`\n")
	b.WriteString("- 环境变量:`DEEPX_HOME`(父根)、`DEEPX_INSTANCE=<n>`(固定编号,不登记)\n")
	if unk := unknownInstanceDirs(); len(unk) > 0 {
		b.WriteString(fmt.Sprintf("- ⚠️ 未识别的 `deepx_*` 目录(不参与编号分配):%s\n", strings.Join(unk, "、 ")))
	}
	return b.String()
}

// registryPath 返回登记表绝对路径(父根/deepx_registry.json)。
func registryPath() string {
	base := instance.BaseRoot()
	if base == "" {
		return ""
	}
	return base + string(os.PathSeparator) + "deepx_registry.json"
}

// unknownInstanceDirs 列出父根下名字以 deepx_ 开头、但不是 deepx_<数字> 的目录。
func unknownInstanceDirs() []string {
	base := instance.BaseRoot()
	if base == "" {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "deepx_") && !instanceDirRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}
