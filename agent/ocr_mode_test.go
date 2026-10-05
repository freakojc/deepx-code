package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 一张 1x1 合法 PNG(最小文件头 + IHDR + IDAT + IEND)。
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func writeTinyPNG(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(p, tinyPNG, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// OcrMode=on(forceOCR=true):即使模型支持视觉,带图消息也必须渲染成"路径 + forcedOCRReminder",
// 不产出任何 image part —— 强制走内置 OCR 的核心。
func TestRenderConvoImages_ForceOCR(t *testing.T) {
	img := writeTinyPNG(t)
	msg := ChatMessage{Role: "user", Content: "请识别 [Image #1]", ImagePaths: []string{img}}
	out := renderConvoImages([]ChatMessage{msg}, true /*vision*/, true /*forceOCR*/)[0]
	if len(out.ContentParts) != 0 {
		t.Fatalf("forceOCR 不该产出 image part,got %d parts", len(out.ContentParts))
	}
	if !strings.Contains(out.Content, img) {
		t.Errorf("forceOCR 应把占位符替换成图片路径,got: %s", out.Content)
	}
	if !strings.Contains(out.Content, forcedOCRReminder) {
		t.Errorf("forceOCR 应追加 forcedOCRReminder,got: %s", out.Content)
	}
	if strings.Contains(out.Content, nonVisionReminder) {
		t.Errorf("forceOCR 不该用 nonVisionReminder(那是\"你不是视觉模型\"的假话),got: %s", out.Content)
	}
}

// auto + 视觉模型(forceOCR=false, vision=true):维持现状 —— base64 内联、无需 OCR 提醒。
func TestRenderConvoImages_AutoVision(t *testing.T) {
	img := writeTinyPNG(t)
	msg := ChatMessage{Role: "user", Content: "请识别 [Image #1]", ImagePaths: []string{img}}
	out := renderConvoImages([]ChatMessage{msg}, true, false)[0]
	imgParts := 0
	for _, p := range out.ContentParts {
		if p.Type == "image_url" {
			imgParts++
			if !strings.HasPrefix(p.ImageURL.URL, "data:image/png;base64,") {
				t.Errorf("image part 应为 base64 data URL,got: %.40s", p.ImageURL.URL)
			}
		}
	}
	if imgParts != 1 {
		t.Fatalf("auto+视觉应产出 1 个 image_url part,got %d (%#v)", imgParts, out.ContentParts)
	}
}

// auto + 非视觉模型(forceOCR=false, vision=false):维持现状 —— 路径 + nonVisionReminder。
func TestRenderConvoImages_AutoNonVision(t *testing.T) {
	img := writeTinyPNG(t)
	msg := ChatMessage{Role: "user", Content: "请识别 [Image #1]", ImagePaths: []string{img}}
	out := renderConvoImages([]ChatMessage{msg}, false, false)[0]
	if len(out.ContentParts) != 0 {
		t.Fatalf("auto+非视觉不该产出 image part,got %d parts", len(out.ContentParts))
	}
	if !strings.Contains(out.Content, nonVisionReminder) {
		t.Errorf("auto+非视觉应追加 nonVisionReminder,got: %s", out.Content)
	}
}

// ocrOffError:只包装 off 模式下"端点拒收图片"的错误,其余情况原样透传。
func TestOcrOffError(t *testing.T) {
	rejected := errors.New(`HTTP 404 No endpoints found that support image input`)
	other := errors.New(`HTTP 500 boom`)

	got := ocrOffError("off", "mimo", rejected)
	if !strings.Contains(got.Error(), "OCR 模式为 off") {
		t.Errorf("off+拒收应包装成可读中文,got: %v", got)
	}

	// 非拒收错误(网络/5xx)不该被包装成 OCR 模式问题。
	if got := ocrOffError("off", "mimo", other); got != other {
		t.Errorf("off+非拒收错误应原样透传,got: %v", got)
	}
	// auto/on 撞拒收也不包装(自愈/拦截各自处理,不是用户的强制选择)。
	if got := ocrOffError("auto", "mimo", rejected); got != rejected {
		t.Errorf("auto+拒收应原样透传,got: %v", got)
	}
	if got := ocrOffError("on", "mimo", rejected); got != rejected {
		t.Errorf("on+拒收应原样透传,got: %v", got)
	}
	if got := ocrOffError("off", "mimo", nil); got != nil {
		t.Errorf("nil 错误应原样返回,got: %v", got)
	}
}
