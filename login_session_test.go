package goed2k

import (
	"testing"

	serverproto "github.com/goed2k/core/protocol/server"
)

func TestSessionLoginRequestOptions(t *testing.T) {
	tests := []struct {
		name         string
		udpPort      int
		obfuPort     int
		enableCrypt  bool
		requireCrypt bool
		wantUDP      int
		wantObfu     int
		wantEnable   bool
		wantRequired bool
	}{
		{
			name:    "无UDP无混淆",
			udpPort: 0,
		},
		{
			name:    "有UDP无混淆",
			udpPort: 4672,
			wantUDP: 4672,
		},
		{
			name:        "无UDP有混淆端口",
			udpPort:     0,
			obfuPort:    4665,
			enableCrypt: true,
			wantObfu:    4665,
			wantEnable:  true,
		},
		{
			name:        "有UDP有混淆端口",
			udpPort:     4672,
			obfuPort:    4665,
			enableCrypt: true,
			wantUDP:     4672,
			wantObfu:    4665,
			wantEnable:  true,
		},
		{
			name:       "未启用混淆不上报端口",
			udpPort:    4672,
			obfuPort:   4665,
			wantUDP:    4672,
			wantObfu:   0,
			wantEnable: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := NewSettings()
			st.UDPPort = tc.udpPort
			st.ObfuscationTCPPort = tc.obfuPort
			st.EnableCryptLayer = tc.enableCrypt
			st.CryptLayerRequired = tc.requireCrypt
			session := NewSession(st)
			opts := session.loginRequestOptions()
			if opts.UDPPort != tc.wantUDP {
				t.Fatalf("UDPPort=%d want %d", opts.UDPPort, tc.wantUDP)
			}
			if opts.ObfuscationTCPPort != tc.wantObfu {
				t.Fatalf("ObfuscationTCPPort=%d want %d", opts.ObfuscationTCPPort, tc.wantObfu)
			}
			if opts.EnableCryptLayer != tc.wantEnable {
				t.Fatalf("EnableCryptLayer=%v want %v", opts.EnableCryptLayer, tc.wantEnable)
			}
			if opts.CryptLayerRequired != tc.wantRequired {
				t.Fatalf("CryptLayerRequired=%v want %v", opts.CryptLayerRequired, tc.wantRequired)
			}
			login := newSessionLoginRequest(session)
			assertLoginWirePorts(t, login, tc.wantUDP, tc.wantObfu, tc.wantEnable || tc.wantRequired)
		})
	}
}

func assertLoginWirePorts(t *testing.T, login serverproto.LoginRequest, udpPort, obfuPort int, cryptEnabled bool) {
	t.Helper()
	var gotUDP, gotObfu uint32
	var hasUDP, hasObfu bool
	for _, tag := range login.Properties {
		switch tag.ID {
		case 0x21:
			hasUDP, gotUDP = true, tag.UInt32
		case 0x97:
			hasObfu, gotObfu = true, tag.UInt32
		case 0x24:
			t.Fatal("Login 不得写入 ET_COMMENTS/0x24")
		}
	}
	if udpPort > 0 {
		if !hasUDP || int(gotUDP) != udpPort {
			t.Fatalf("线上 UDP 标签不符: has=%v port=%d want=%d", hasUDP, gotUDP, udpPort)
		}
	} else if hasUDP {
		t.Fatal("无 UDP 配置时不得写入 0x21")
	}
	if cryptEnabled && obfuPort > 0 {
		if !hasObfu || int(gotObfu) != obfuPort {
			t.Fatalf("线上混淆端口标签不符: has=%v port=%d want=%d", hasObfu, gotObfu, obfuPort)
		}
	} else if hasObfu {
		t.Fatal("未启用或端口为 0 时不得写入混淆端口标签")
	}
}
