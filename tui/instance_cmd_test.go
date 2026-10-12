package tui

import (
	"strings"
	"testing"
)

// /instance 输出尾部应追加与 TUI 登录横幅一致的 web 面板访问地址,并带 OSC 8 超链接。
func TestInstanceMessageIncludesWebURL(t *testing.T) {
	url := "http://127.0.0.1:50529/?t=abc123"
	out := instanceMessage(url)

	if !strings.Contains(out, "**本地 Web 面板已就绪**") {
		t.Errorf("应显示已就绪标题:\n%s", out)
	}
	if !strings.Contains(out, url) {
		t.Errorf("输出应包含访问地址 %s:\n%s", url, out)
	}
	// OSC 8 超链接:ESC]8;…URL ESC\…ESC]8;;ESC\ ,点击直达浏览器
	if !strings.Contains(out, "\x1b]8;") {
		t.Errorf("访问地址应包成 OSC 8 可点击超链接:\n%s", out)
	}
	if !strings.Contains(out, "本地 Web 面板已就绪") && strings.Contains(out, "未启动") {
		t.Errorf("不应出现未启动提示:\n%s", out)
	}
}

// web 未启动(URL 空)时 /instance 应如实显示未启动,不带空链接。
func TestInstanceMessageWebUnavailable(t *testing.T) {
	out := instanceMessage("")
	if !strings.Contains(out, "本地 Web 面板未启动") {
		t.Errorf("webURL 为空应显示未启动:\n%s", out)
	}
	if strings.Contains(out, "\x1b]8;") {
		t.Errorf("未启动时不应有 OSC 8 链接:\n%s", out)
	}
}
