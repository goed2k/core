package server

import (
	"bytes"
	"encoding/binary"
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
	gotFlags, ok := loginUInt32Tag(login.Properties, ctServerFlags)
	if !ok {
		t.Fatal("缺少 CT_SERVER_FLAGS")
	}
	wantFlags := uint32(CapableAuxPort | CapableNewTags | CapableUnicode | CapableLargeFile | CapableZlib)
	if gotFlags != wantFlags {
		t.Fatalf("未启用扩展时能力位必须保持原值: got 0x%x want 0x%x", gotFlags, wantFlags)
	}
	if _, ok := loginUInt32Tag(login.Properties, ctUDPPort); ok {
		t.Fatal("未提供 UDP 端口时不得写入 ET_UDPPORT")
	}
	if _, ok := loginUInt32Tag(login.Properties, ctObfuscationTCPPort); ok {
		t.Fatal("未启用混淆时不得写入混淆 TCP 端口标签")
	}
	if _, ok := loginUInt32Tag(login.Properties, etComments); ok {
		t.Fatal("0x24 是 ET_COMMENTS，Login 不得把它当作混淆端口")
	}
	if gotFlags&CapableIPInLogin != 0 {
		t.Fatal("未携带真实 IP 时不得声明 CapableIPInLogin")
	}
}

func TestLoginRequestExtendedTags(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	const listenPort = 4662
	tests := []struct {
		name           string
		opts           LoginRequestOptions
		wantUDP        bool
		wantUDPPort    uint32
		wantObfu       bool
		wantObfuPort   uint32
		wantCryptBits  uint32
		forbidCryptBit bool
	}{
		{
			name:           "无UDP无混淆",
			opts:           LoginRequestOptions{},
			forbidCryptBit: true,
		},
		{
			name:        "有UDP无混淆",
			opts:        LoginRequestOptions{UDPPort: 4672},
			wantUDP:     true,
			wantUDPPort: 4672,
		},
		{
			name:          "无UDP有混淆端口",
			opts:          LoginRequestOptions{ObfuscationTCPPort: 4665, EnableCryptLayer: true},
			wantObfu:      true,
			wantObfuPort:  4665,
			wantCryptBits: CapableSupportCrypt | CapableRequestCrypt,
		},
		{
			name:          "有UDP有混淆端口",
			opts:          LoginRequestOptions{UDPPort: 4672, ObfuscationTCPPort: 4665, EnableCryptLayer: true},
			wantUDP:       true,
			wantUDPPort:   4672,
			wantObfu:      true,
			wantObfuPort:  4665,
			wantCryptBits: CapableSupportCrypt | CapableRequestCrypt,
		},
		{
			name:          "仅要求混淆且端口存在",
			opts:          LoginRequestOptions{ObfuscationTCPPort: 4700, CryptLayerRequired: true},
			wantObfu:      true,
			wantObfuPort:  4700,
			wantCryptBits: CapableSupportCrypt | CapableRequireCrypt,
		},
		{
			name:           "UDP端口0不谎报",
			opts:           LoginRequestOptions{UDPPort: 0, EnableCryptLayer: true, ObfuscationTCPPort: 4665},
			wantObfu:       true,
			wantObfuPort:   4665,
			wantCryptBits:  CapableSupportCrypt | CapableRequestCrypt,
			forbidCryptBit: false,
		},
		{
			name:           "混淆端口0不谎报",
			opts:           LoginRequestOptions{UDPPort: 4672, EnableCryptLayer: true, ObfuscationTCPPort: 0},
			wantUDP:        true,
			wantUDPPort:    4672,
			wantCryptBits:  CapableSupportCrypt | CapableRequestCrypt,
			forbidCryptBit: false,
		},
		{
			name:           "未启用混淆却配置端口",
			opts:           LoginRequestOptions{UDPPort: 4672, ObfuscationTCPPort: 4665},
			wantUDP:        true,
			wantUDPPort:    4672,
			forbidCryptBit: true,
		},
		{
			name:          "非法端口不上报",
			opts:          LoginRequestOptions{UDPPort: 70000, ObfuscationTCPPort: -1, EnableCryptLayer: true},
			wantCryptBits: CapableSupportCrypt | CapableRequestCrypt,
		},
	}

	baseFlags := uint32(CapableAuxPort | CapableNewTags | CapableUnicode | CapableLargeFile | CapableZlib)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			login := NewLoginRequestWith(hash, listenPort, "goed2k", tc.opts)
			gotUDP, hasUDP := loginUInt32Tag(login.Properties, ctUDPPort)
			if hasUDP != tc.wantUDP {
				t.Fatalf("UDP 标签出现条件不符: has=%v want=%v", hasUDP, tc.wantUDP)
			}
			if tc.wantUDP && gotUDP != tc.wantUDPPort {
				t.Fatalf("UDP 端口不符: got %d want %d", gotUDP, tc.wantUDPPort)
			}
			gotObfu, hasObfu := loginUInt32Tag(login.Properties, ctObfuscationTCPPort)
			if hasObfu != tc.wantObfu {
				t.Fatalf("混淆端口标签出现条件不符: has=%v want=%v", hasObfu, tc.wantObfu)
			}
			if tc.wantObfu && gotObfu != tc.wantObfuPort {
				t.Fatalf("混淆端口不符: got %d want %d", gotObfu, tc.wantObfuPort)
			}
			if _, ok := loginUInt32Tag(login.Properties, etComments); ok {
				t.Fatal("不得写入 ET_COMMENTS/0x24")
			}
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
			if tc.forbidCryptBit && cryptBits != 0 {
				t.Fatalf("未启用混淆时不得声明 crypt 能力: 0x%x", cryptBits)
			}
			if flags&CapableIPInLogin != 0 {
				t.Fatal("不得声明 CapableIPInLogin")
			}
		})
	}
}

func TestLoginRequestRoundTripAndEndian(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	original := NewLoginRequestWith(hash, 4662, "goed2k", LoginRequestOptions{
		UDPPort:            0x1234,
		ObfuscationTCPPort: 0x2345,
		EnableCryptLayer:   true,
	})
	var buf bytes.Buffer
	if err := original.Put(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != original.BytesCount() {
		t.Fatalf("BytesCount 与编码长度不一致: count=%d raw=%d", original.BytesCount(), buf.Len())
	}
	assertLittleEndianPortTag(t, buf.Bytes(), ctUDPPort, 0x1234)
	assertLittleEndianPortTag(t, buf.Bytes(), ctObfuscationTCPPort, 0x2345)

	var restored LoginRequest
	if err := restored.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !restored.Hash.Equal(original.Hash) || restored.Point != original.Point {
		t.Fatalf("往返后头字段变化: %+v", restored)
	}
	gotUDP, _ := loginUInt32Tag(restored.Properties, ctUDPPort)
	gotObfu, _ := loginUInt32Tag(restored.Properties, ctObfuscationTCPPort)
	if gotUDP != 0x1234 || gotObfu != 0x2345 {
		t.Fatalf("往返后端口标签变化 udp=%d obfu=%d", gotUDP, gotObfu)
	}
	flags, _ := loginUInt32Tag(restored.Properties, ctServerFlags)
	if flags&(CapableSupportCrypt|CapableRequestCrypt) != CapableSupportCrypt|CapableRequestCrypt {
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
	gotUDP, _ = loginUInt32Tag(packed.Properties, ctUDPPort)
	gotObfu, _ = loginUInt32Tag(packed.Properties, ctObfuscationTCPPort)
	if gotUDP != 0x1234 || gotObfu != 0x2345 {
		t.Fatalf("组包往返后端口标签变化 udp=%d obfu=%d", gotUDP, gotObfu)
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

func assertLittleEndianPortTag(t *testing.T, raw []byte, id byte, port uint32) {
	t.Helper()
	// new-style uint32 标签：type|0x80, id, value LE
	pattern := []byte{protocol.TagTypeUint32 | 0x80, id}
	idx := bytes.Index(raw, pattern)
	if idx < 0 || idx+6 > len(raw) {
		t.Fatalf("未找到标签 0x%02x 的小端编码头", id)
	}
	got := binary.LittleEndian.Uint32(raw[idx+2 : idx+6])
	if got != port {
		t.Fatalf("标签 0x%02x 字节序错误: got 0x%x want 0x%x", id, got, port)
	}
}
