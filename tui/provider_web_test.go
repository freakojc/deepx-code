package tui

import (
	"testing"

	"deepx/agent"
	"deepx/config"
	"deepx/web"
)

// TestCurrentProviderNameMatchesSavedProvider 验证 model.yaml → 存档名 的反查判据:
// flash/pro 两条 model id 都相等才算命中;改了任一 id 就查不到(返回 "",下拉显示未选中)。
// 这是 /provider 选择器光标与浏览器提供商下拉共用的判据(见 handleProviderCommand)。
func TestCurrentProviderNameMatchesSavedProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	cfg := savedCfg("openrouter")
	if err := config.SaveProvider("openrouter", cfg); err != nil {
		t.Fatal(err)
	}

	models := agent.ModelConfig{Flash: agent.ModelEntry(cfg.Flash), Pro: agent.ModelEntry(cfg.Pro)}
	if got := currentProviderName(models); got != "openrouter" {
		t.Fatalf("应反查到 openrouter, got %q", got)
	}

	// flash id 变了 → 两份 model id 不再全等,不该命中。
	models.Flash.Model = "other-model"
	if got := currentProviderName(models); got != "" {
		t.Fatalf("flash id 变了不该再匹配, got %q", got)
	}
}

// TestBroadcastProviderStatePushesHub 验证推给 hub 的提供商态:存档列表 + 当前提供商名 +
// 模型名,浏览器「提供商」下拉据此渲染选项与选中项。hub 为 nil(web 关闭)时不该 panic。
func TestBroadcastProviderStatePushesHub(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	cfg := savedCfg("openrouter")
	if err := config.SaveProvider("openrouter", cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveProvider("my-relay", savedCfg("my-relay")); err != nil {
		t.Fatal(err)
	}

	hub := web.NewHub("f", "p", "/tmp/ws", "zh")
	m := model{
		hub:             hub,
		models:          agent.ModelConfig{Flash: agent.ModelEntry(cfg.Flash), Pro: agent.ModelEntry(cfg.Pro)},
		activeModelRole: "flash",
	}
	m.broadcastProviderState()

	s := hub.SnapshotCopy()
	if len(s.Providers) != 2 {
		t.Fatalf("providers 应含两个存档, got %+v", s.Providers)
	}
	if s.Models.Provider != "openrouter" {
		t.Fatalf("当前提供商名应为 openrouter, got %q", s.Models.Provider)
	}
	if s.Models.Flash != cfg.Flash.Model || s.Models.Pro != cfg.Pro.Model {
		t.Fatalf("模型名应同步进快照, got %+v", s.Models)
	}

	// web 关闭(hub==nil)时静默跳过,不 panic。
	off := m
	off.hub = nil
	off.broadcastProviderState()
}
