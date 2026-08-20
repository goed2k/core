package server

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestNewLoginRequestKeepsBaseTags(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	login := NewLoginRequest(hash, 4662, "goed2k")
	if login.Point.Port() != 4662 {
		t.Fatalf("endpoint 端口应为 ListenPort: got %d", login.Point.Port())
	}
	if login.Point.IP() != 0 {
		t.Fatalf("登录 IP 未确定时必须为 0，不得谎报 ReportedIP: got %d", login.Point.IP())
	}
	assertOfficialLoginTagsOnly(t, login)
	gotFlags, ok := loginUInt32Tag(login.Properties, ctServerFlags)
	if !ok {
		t.Fatal("缺少 CT_SERVER_FLAGS")
	}
	wantFlags := uint32(CapableAuxPort | CapableNewTags | CapableUnicode | CapableLargeFile | CapableZlib)
	if gotFlags != wantFlags {
		t.Fatalf("未启用扩展时能力位必须保持原值: got 0x%x want 0x%x", gotFlags, wantFlags)
	}
	if gotFlags&CapableIPInLogin != 0 {
		t.Fatal("未携带真实 IP 时不得声明 CapableIPInLogin")
	}
}

func TestLoginRequestCryptCapabilityBits(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	tests := []struct {
		name          string
		opts          LoginRequestOptions
		wantCryptBits uint32
	}{
		{
			name: "无混淆",
			opts: LoginRequestOptions{},
		},
		{
			name:          "启用混淆",
			opts:          LoginRequestOptions{EnableCryptLayer: true},
			wantCryptBits: CapableSupportCrypt | CapableRequestCrypt,
		},
		{
			name:          "仅要求混淆",
			opts:          LoginRequestOptions{CryptLayerRequired: true},
			wantCryptBits: CapableSupportCrypt | CapableRequireCrypt,
		},
		{
			name:          "启用且要求混淆",
			opts:          LoginRequestOptions{EnableCryptLayer: true, CryptLayerRequired: true},
			wantCryptBits: CapableSupportCrypt | CapableRequestCrypt | CapableRequireCrypt,
		},
	}

	baseFlags := uint32(CapableAuxPort | CapableNewTags | CapableUnicode | CapableLargeFile | CapableZlib)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			login := NewLoginRequestWith(hash, 4662, "goed2k", tc.opts)
			assertOfficialLoginTagsOnly(t, login)
			flags, ok := loginUInt32Tag(login.Properties, ctServerFlags)
			if !ok {
				t.Fatal("缺少 CT_SERVER_FLAGS")
			}
			if flags&baseFlags != baseFlags {
				t.Fatalf("既有能力位被改动: flags=0x%x", flags)
			}
			cryptBits := flags & (CapableSupportCrypt | CapableRequestCrypt | CapableRequireCrypt)
			if cryptBits != tc.wantCryptBits {
				t.Fatalf("混淆能力位不符: got 0x%x want 0x%x", cryptBits, tc.wantCryptBits)
			}
			if flags&CapableIPInLogin != 0 {
				t.Fatal("不得声明 CapableIPInLogin")
			}
		})
	}
}

func TestLoginRequestRoundTripPreservesCryptFlags(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	original := NewLoginRequestWith(hash, 4662, "goed2k", LoginRequestOptions{
		EnableCryptLayer:   true,
		CryptLayerRequired: true,
	})
	var buf bytes.Buffer
	if err := original.Put(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != original.BytesCount() {
		t.Fatalf("BytesCount 与编码长度不一致: count=%d raw=%d", original.BytesCount(), buf.Len())
	}

	var restored LoginRequest
	if err := restored.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !restored.Hash.Equal(original.Hash) || restored.Point != original.Point {
		t.Fatalf("往返后头字段变化: %+v", restored)
	}
	assertOfficialLoginTagsOnly(t, restored)
	flags, _ := loginUInt32Tag(restored.Properties, ctServerFlags)
	want := uint32(CapableSupportCrypt | CapableRequestCrypt | CapableRequireCrypt)
	if flags&want != want {
		t.Fatalf("往返后混淆能力位丢失: 0x%x", flags)
	}

	combiner := NewPacketCombiner()
	raw, err := combiner.Pack("server.LoginRequest", &original)
	if err != nil {
		t.Fatal(err)
	}
	_, packet, err := combiner.UnpackFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	packed, ok := packet.(*LoginRequest)
	if !ok {
		t.Fatalf("类型断言失败: %T", packet)
	}
	assertOfficialLoginTagsOnly(t, *packed)
	flags, _ = loginUInt32Tag(packed.Properties, ctServerFlags)
	if flags&want != want {
		t.Fatalf("组包往返后混淆能力位丢失: 0x%x", flags)
	}
}

func assertOfficialLoginTagsOnly(t *testing.T, login LoginRequest) {
	t.Helper()
	if len(login.Properties) != 4 {
		t.Fatalf("aMule/eMule Login 固定 4 个标签: got %d", len(login.Properties))
	}
	seen := map[byte]int{}
	for _, tag := range login.Properties {
		seen[tag.ID]++
		switch tag.ID {
		case etUDPPort:
			t.Fatal("Login 不得写入 Hello ET_UDPPORT/0x21")
		case etComments:
			t.Fatal("Login 不得写入 ET_COMMENTS/0x24")
		case stTCPObfuPort:
			t.Fatal("Login 不得写入 server.met ST_TCPPORTOBFUSCATION/0x97")
		}
	}
	for _, id := range []byte{ctVersion, ctServerFlags, ctName, ctEMuleVersion} {
		if seen[id] != 1 {
			t.Fatalf("缺少或重复官方 Login 标签 0x%02x: %d", id, seen[id])
		}
	}
}

func loginUInt32Tag(tags protocol.TagList, id byte) (uint32, bool) {
	for _, tag := range tags {
		if tag.ID == id {
			return tag.UInt32, true
		}
	}
	return 0, false
}
