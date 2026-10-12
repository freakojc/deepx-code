package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"deepx/instance"

	"charm.land/lipgloss/v2"
)

// instanceDirRe 匹配规范实例目录名 deepx_<数字>(前导零容忍由分配器统一处理)。
var instanceDirRe = regexp.MustCompile(`^deepx_[0-9]+$`)

// instanceMessage 组装 /instance 的输出:当前编号、实例环境根、父根、共享资产、
// 派生端口、web 面板访问地址,以及"以 deepx_ 开头但不符合编号规范"的未识别目录。
// webURL 传入与 TUI 登录横幅完全一致的地址(含 token,见 web.ready),追加在输出尾部;
// 空串表示 web 未启动(监听失败/被禁用)。
func instanceMessage(webURL string) string {
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
	// 尾部追加 web 面板访问地址,与 TUI 登录横幅(web.ready)同源一致。
	// 地址包成 OSC 8 超链接(带下划线高亮),支持点击直达默认浏览器(Windows Terminal 等)。
	b.WriteString("\n")
	if webURL == "" {
		b.WriteString("**本地 Web 面板未启动**\n")
	} else {
		b.WriteString("**本地 Web 面板已就绪**\n\n")
		b.WriteString(linkWebURL(webURL))
		if webURLExposed(webURL) {
			b.WriteString("\n\n⚠️ 该地址对外可达(非回环),请勿分享给不可信方")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// linkWebURL 把 web 面板地址渲染成可点击的 OSC 8 超链接(点击文字直达浏览器)。
func linkWebURL(url string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Underline(true).
		Hyperlink(url).
		Render(url)
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
