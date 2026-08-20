package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestParseHelloTagList(t *testing.T) {
	var dst RemotePeerInfo
	props := protocol.TagList{
		protocol.NewStringTag(0x01, "nick"),
		protocol.NewStringTag(0x55, "eMule"),
		protocol.NewUInt32Tag(0x11, 0x3c),
		protocol.NewUInt32Tag(0xFB, uint32((3<<24)|((5)<<17)|((2)<<10)|((1)<<7))),
	}
	parseHelloTagList(&dst, &props)
	if dst.NickName != "nick" {
		t.Fatalf("nick: got %q", dst.NickName)
	}
	if dst.ModName != "eMule" {
		t.Fatalf("mod: got %q", dst.ModName)
	}
	if dst.Version != 0x3c {
		t.Fatalf("version: got %d", dst.Version)
	}
	if dst.ModVersion != "5.2.1" {
		t.Fatalf("mod composite str: got %q", dst.ModVersion)
	}
}

func TestDecodeHelloModCompositeZero(t *testing.T) {
	if s := decodeHelloModComposite(0); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}

func TestMiscOptionsSingleFieldRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value MiscOptions
		mask  uint32
	}{
		{name: "AICH", value: MiscOptions{AICHVersion: 5}, mask: 5 << 29},
		{name: "Unicode", value: MiscOptions{UnicodeSupport: 1}, mask: 1 << 28},
		{name: "UDP", value: MiscOptions{UDPVer: 10}, mask: 10 << 24},
		{name: "压缩", value: MiscOptions{DataCompVer: 3}, mask: 3 << 20},
		{name: "安全身份", value: MiscOptions{SupportSecIdent: 4}, mask: 4 << 16},
		{name: "来源交换", value: MiscOptions{SourceExchange1Ver: 5}, mask: 5 << 12},
		{name: "扩展请求", value: MiscOptions{ExtendedRequestsVer: 6}, mask: 6 << 8},
		{name: "评论", value: MiscOptions{AcceptCommentVer: 7}, mask: 7 << 4},
		{name: "隐藏共享文件", value: MiscOptions{NoViewSharedFiles: 1}, mask: 1 << 2},
		{name: "多包", value: MiscOptions{MultiPacket: 1}, mask: 1 << 1},
		{name: "预览", value: MiscOptions{SupportsPreview: 1}, mask: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uint32(tt.value.IntValue()); got != tt.mask {
				t.Fatalf("编码位错误: got %#08x, want %#08x", got, tt.mask)
			}
			var decoded MiscOptions
			decoded.Assign(int(tt.mask))
			if decoded != tt.value {
				t.Fatalf("解码不对称: got %+v, want %+v", decoded, tt.value)
			}
		})
	}
}

func TestMiscOptions2SingleCapabilityRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		set      func(*MiscOptions2)
		supports func(MiscOptions2) bool
	}{
		{name: "大文件", offset: largeFileOffset, set: (*MiscOptions2).SetLargeFiles, supports: MiscOptions2.SupportLargeFiles},
		{name: "扩展多包", offset: multipOffset, set: (*MiscOptions2).SetExtMultipacket, supports: MiscOptions2.SupportExtMultipacket},
		{name: "来源交换二", offset: srcExtOffset, set: (*MiscOptions2).SetSourceExt2, supports: MiscOptions2.SupportSourceExt2},
		{name: "验证码", offset: captchaOffset, set: (*MiscOptions2).SetCaptcha, supports: MiscOptions2.SupportCaptcha},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var options MiscOptions2
			if tt.supports(options) {
				t.Fatal("零值不应声明能力")
			}
			tt.set(&options)
			want := 1 << tt.offset
			if options.Value != want {
				t.Fatalf("设置器污染其他位: got %#08x, want %#08x", options.Value, want)
			}
			if !tt.supports(options) {
				t.Fatal("设置后读取器未识别能力")
			}

			var decoded MiscOptions2
			decoded.Assign(want)
			if !tt.supports(decoded) {
				t.Fatal("从线值解码后未识别能力")
			}
		})
	}
}

func TestPrepareHelloAnswerAdvertisesOnlyImplementedCapabilities(t *testing.T) {
	session := NewSession(NewSettings())
	connection := NewPeerConnection(session, protocol.Endpoint{}, nil, nil)
	hello := connection.PrepareHelloAnswer()

	var misc1Raw, misc2Raw uint32
	for _, tag := range hello.Properties {
		if tag.Type != protocol.TagTypeUint32 {
			continue
		}
		switch tag.ID {
		case helloTagMisc1:
			misc1Raw = tag.UInt32
		case helloTagMisc2:
			misc2Raw = tag.UInt32
		}
	}
	wantMisc1 := uint32((1 << aichVersionOffset) |
		(1 << unicodeSupportOffset) |
		(session.GetCompressionVersion() << dataCompressionOffset) |
		(1 << sourceExchange1Offset) |
		(1 << noViewSharedFilesOffset))
	if misc1Raw != wantMisc1 {
		t.Fatalf("Hello MiscOptions: got %#08x, want %#08x", misc1Raw, wantMisc1)
	}

	var remote RemotePeerInfo
	parseHelloTagList(&remote, &hello.Properties)

	if remote.Misc1.AICHVersion != 1 ||
		remote.Misc1.UnicodeSupport != 1 ||
		remote.Misc1.DataCompVer != session.GetCompressionVersion() ||
		remote.Misc1.SourceExchange1Ver != 1 ||
		remote.Misc1.NoViewSharedFiles != 1 {
		t.Fatalf("Hello 缺少已实现的 MiscOptions 能力: %+v", remote.Misc1)
	}
	if remote.Misc1.UDPVer != 0 || remote.Misc1.MultiPacket != 0 || remote.Misc1.SupportsPreview != 0 {
		t.Fatalf("Hello 声明了尚未完整实现的 MiscOptions 能力: %+v", remote.Misc1)
	}

	wantMisc2 := (1 << largeFileOffset) | (1 << srcExtOffset)
	if misc2Raw != uint32(wantMisc2) || remote.Misc2.Value != wantMisc2 {
		t.Fatalf("Hello MiscOptions2: raw %#08x, decoded %#08x, want %#08x", misc2Raw, remote.Misc2.Value, wantMisc2)
	}
	if !remote.Misc2.SupportLargeFiles() || !remote.Misc2.SupportSourceExt2() {
		t.Fatal("Hello 缺少已实现的大文件或来源交换二能力")
	}
	if remote.Misc2.SupportCaptcha() || remote.Misc2.SupportExtMultipacket() {
		t.Fatal("Hello 声明了尚未完整实现的验证码或扩展多包能力")
	}
	if remote.SourceExchange2Ver == 0 {
		t.Fatal("Hello 缺少来源交换二版本")
	}
}
