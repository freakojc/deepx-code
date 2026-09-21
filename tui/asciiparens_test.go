package tui

import "testing"

func withFold(t *testing.T, on bool, min bool) {
	t.Helper()
	oldOn, oldMin := asciiPunctOn, asciiPunctMin
	t.Cleanup(func() { asciiPunctOn, asciiPunctMin = oldOn, oldMin })
	asciiPunctOn = on
	if min {
		asciiPunctMin = minFolder
	} else {
		asciiPunctMin = nil
	}
}

// TestFoldAll 默认行为:两个区的全角标点全折,汉字与半角字符不动。
func TestFoldAll(t *testing.T) {
	withFold(t, true, false)
	in := "按 karpathy「简单优先」，去掉覆盖（圆角+阴影）：只保留字号；对吗？草坪。"
	want := `按 karpathy"简单优先",去掉覆盖(圆角+阴影):只保留字号;对吗?草坪.`
	if got := foldFullwidthPunct(in); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// TestFoldMin 保守集:折成对括号与冒号,保留 。、，；！？。
func TestFoldMin(t *testing.T) {
	withFold(t, true, true)
	in := "按 karpathy「简单优先」，去掉覆盖（圆角+阴影）：只保留字号；对吗？草坪。"
	want := `按 karpathy"简单优先"，去掉覆盖(圆角+阴影):只保留字号；对吗？草坪。`
	if got := foldFullwidthPunct(in); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// TestFoldOff 关闭时原样返回。
func TestFoldOff(t *testing.T) {
	withFold(t, false, false)
	in := "按 karpathy「简单优先」，覆盖（圆角）："
	if got := foldFullwidthPunct(in); got != in {
		t.Errorf("关闭时应原样返回,got %q", got)
	}
}
