package goed2k

import (
	"testing"

	serverproto "github.com/goed2k/core/protocol/server"
)

func TestSessionLoginRequestOptions(t *testing.T) {
	tests := []struct {
		name         string
		enableCrypt  bool
		requireCrypt bool
		wantEnable   bool
		wantRequired bool
		wantBits     uint32
	}{
		{name: "默认关闭"},
		{
			name:        "启用混淆",
			enableCrypt: true,
			wantEnable:  true,
			wantBits:    serverproto.CapableSupportCrypt | serverproto.CapableRequestCrypt,
		},
		{
			name:         "仅要求混淆",
			requireCrypt: true,
			wantRequired: true,
			wantBits:     serverproto.CapableSupportCrypt | serverproto.CapableRequireCrypt,
		},
		{
			name:         "启用且要求",
			enableCrypt:  true,
			requireCrypt: true,
			wantEnable:   true,
			wantRequired: true,
			wantBits:     serverproto.CapableSupportCrypt | serverproto.CapableRequestCrypt | serverproto.CapableRequireCrypt,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := NewSettings()
			st.UDPPort = 4672
			st.ObfuscationTCPPort = 4665
			st.EnableCryptLayer = tc.enableCrypt
			st.CryptLayerRequired = tc.requireCrypt
			session := NewSession(st)
			opts := session.loginRequestOptions()
			if opts.EnableCryptLayer != tc.wantEnable {
				t.Fatalf("EnableCryptLayer=%v want %v", opts.EnableCryptLayer, tc.wantEnable)
			}
			if opts.CryptLayerRequired != tc.wantRequired {
				t.Fatalf("CryptLayerRequired=%v want %v", opts.CryptLayerRequired, tc.wantRequired)
			}
			login := newSessionLoginRequest(session)
			if len(login.Properties) != 4 {
				t.Fatalf("Login 标签数=%d want 4", len(login.Properties))
			}
			var flags uint32
			for _, tag := range login.Properties {
				switch tag.ID {
				case 0x20:
					flags = tag.UInt32
				case 0x21, 0x24, 0x97:
					t.Fatalf("Login 不得写入非官方标签 0x%02x", tag.ID)
				}
			}
			cryptBits := flags & (serverproto.CapableSupportCrypt | serverproto.CapableRequestCrypt | serverproto.CapableRequireCrypt)
			if cryptBits != tc.wantBits {
				t.Fatalf("混淆能力位不符: got 0x%x want 0x%x", cryptBits, tc.wantBits)
			}
		})
	}
}
