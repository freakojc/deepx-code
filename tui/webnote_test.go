package tui

import (
	"os"
	"strings"
	"testing"

	"deepx/agent"

	"github.com/charmbracelet/x/ansi"
)

// web dashboard 启动失败时,原因应以 System 消息出现在对话流(TUI 内可见),
// 而不是裸写 stderr 污染终端(那是"运行中打字看不见"的假象来源)。
func TestWebNoteShownInChat(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // session.New 落盘隔离
	wd, _ := os.Getwd()
	note := "web dashboard 未启动(监听 127.0.0.1:50530 失败): bind 失败测试"
	m := initialModel(agent.ModelConfig{}, false, "v", nil, nil, "", note)
	m.width, m.height = 120, 40
	m.chatViewport.SetWidth(100)
	m.chatViewport.SetHeight(20)
	m.refreshViewport() // 让 chatContent(welcome + webNote)进 viewport
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "web dashboard 未启动") {
		t.Fatalf("webNote 应显示在对话流。View:\n%s", out)
	}
	if !strings.Contains(out, wd) {
		t.Logf("注意:工作目录 %s 未见(不阻塞断言)", wd)
	}
}
