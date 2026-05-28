package celengine

import "testing"

func TestIsScanSourceWhitelisted(t *testing.T) {
	cases := map[string]bool{
		// GKE node pool
		"10.170.2.16":  true,
		"10.170.2.1":   true,
		"10.170.2.254": true,
		// 同 /16 但不同 /24，不在白名单
		"10.170.3.16": false,
		"10.170.1.1":  false,
		// 其他内网
		"192.168.1.1": false,
		"172.16.0.1":  false,
		// 公网
		"8.8.8.8": false,
		// 无效 IP
		"":       false,
		"abcdef": false,
	}
	for ip, want := range cases {
		if got := isScanSourceWhitelisted(ip); got != want {
			t.Errorf("isScanSourceWhitelisted(%q) = %v want %v", ip, got, want)
		}
	}
}
