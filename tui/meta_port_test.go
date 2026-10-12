package tui

import "testing"

// 实例端口派生:deepx_01→50529, deepx_02→50530, 依此类推(不复用随机端口)。
func TestWebPortDerived(t *testing.T) {
	cases := []struct {
		n        int
		expected int
	}{
		{1, 50529},
		{2, 50530},
		{3, 50531},
		{10, 50538},
		{999, 51527}, // 50528 + 999
	}
	for _, c := range cases {
		if got := webPortDerived(c.n); got != c.expected {
			t.Errorf("webPortDerived(%d) = %d, want %d", c.n, got, c.expected)
		}
	}
}