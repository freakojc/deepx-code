package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// openWebConfigModal 给 /web-config:弹单行输入框设置 web 面板绑定 IP。
// 端口由实例编号自动派生(50528+N),不在输入里设置;预填当前 host 方便在原值上改。
func (m *model) openWebConfigModal() {
	m.showWebConfig = true
	m.webConfigErr = ""
	cur := metaGet()
	val := strings.TrimSpace(cur.WebHost)
	m.webConfigInput.SetValue(val)
	m.webConfigInput.Focus()
	m.input.Blur()
}

// submitWebConfig 解析 "IP [端口]",校验后只写 host 到 meta.json,并热重绑 web 服务、
// 立刻显示新地址(端口仍按实例编号派生)。端口输入兼容旧习惯但不再生效。
// 成功关弹窗;失败留 webConfigErr 重试。
func (m *model) submitWebConfig() {
	fields := strings.Fields(m.webConfigInput.Value())
	host := ""
	if len(fields) >= 1 {
		host = fields[0]
	}
	// 端口输入(兼容旧习惯)仍校验合法性,但忽略——端口由实例编号派生。
	if len(fields) >= 2 {
		p, err := strconv.Atoi(fields[1])
		if err != nil || p < 0 || p > 65535 {
			m.webConfigErr = T("web.config.err.port")
			return
		}
	}
	// host 允许:空(=默认 127.0.0.1 仅本机)、localhost、合法 IP(含 0.0.0.0)。
	if host != "" && host != "localhost" && net.ParseIP(host) == nil {
		m.webConfigErr = T("web.config.err.host")
		return
	}

	metaUpdate(func(mm *meta) {
		mm.WebHost = host
	})

	m.showWebConfig = false
	m.webConfigErr = ""
	m.webConfigInput.Blur()
	m.input.Focus()

	shownHost := host
	if shownHost == "" {
		shownHost = "127.0.0.1"
	}
	portText := strconv.Itoa(webPort()) // 实例派生端口
	m.appendChat("System", fmt.Sprintf(T("web.config.saved"), shownHost, portText))

	// web 面板开着 → 热重绑到新 host,立刻显示新 URL;关着 → 仅保存,下次启动生效。
	if m.srv != nil {
		if url, err := m.srv.Relisten(host, webPort()); err == nil {
			m.webURL = url
			m.appendChat("System", fmt.Sprintf(T("web.ready"), url))
			if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
				m.appendChat("System", T("web.ready.lan"))
			}
		} else {
			m.appendChat("System", fmt.Sprintf(T("web.config.relisten_failed"), err.Error()))
		}
	}
}

// webConfigModalBlock 渲染 /web-config 弹窗。
func (m model) webConfigModalBlock() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(highlightColor).Render(T("web.config.title"))
	hint := lipgloss.NewStyle().Foreground(subtleColor).Render(T("web.config.hint"))
	inputBlock := lipgloss.NewStyle().Foreground(dimColor).Render("IP / Port:") + "\n  " + m.webConfigInput.View()

	parts := []string{title, "", hint, "", inputBlock}
	if m.webConfigErr != "" {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("✗ "+m.webConfigErr))
	}
	parts = append(parts, "", lipgloss.NewStyle().Foreground(dimColor).Render(T("web.config.footer")))
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	w := 64
	if maxW := m.width - 4; w > maxW {
		w = maxW
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(highlightColor).Padding(1, 2).Width(w).Render(content)
}
