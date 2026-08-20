package goed2k

import (
	"strings"
	"testing"
)

func TestSanitizeDownloadFilenameMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		goos string
		want string
	}{
		{name: "普通名不变", in: "movie.avi", goos: "linux", want: "movie.avi"},
		{name: "普通名在 Windows 也不变", in: "movie.avi", goos: "windows", want: "movie.avi"},
		{name: "方括号合法", in: "Show[1080p].bin", goos: "windows", want: "Show[1080p].bin"},
		{name: "Unix 保留冒号", in: "foo:bar.bin", goos: "linux", want: "foo:bar.bin"},
		{name: "Unix 保留竖线问号", in: "foo|bar?.bin", goos: "darwin", want: "foo|bar?.bin"},
		{name: "Windows 替换冒号", in: "foo:bar.bin", goos: "windows", want: "foo_bar.bin"},
		{name: "Windows 非法字符", in: `a<b>c:d"e|f?g*h.txt`, goos: "windows", want: "a_b_c_d_e_f_g_h.txt"},
		{name: "Windows 控制字符", in: "foo\x01bar\x1f.bin", goos: "windows", want: "foo_bar_.bin"},
		{name: "Unix 不改控制字符以外的合法名", in: "foo\x01bar.bin", goos: "linux", want: "foo\x01bar.bin"},
		{name: "去掉 NUL", in: "foo\x00bar.bin", goos: "linux", want: "foobar.bin"},
		{name: "正斜杠穿越", in: "../etc/passwd", goos: "linux", want: "passwd"},
		{name: "反斜杠穿越", in: `..\..\windows\system.ini`, goos: "linux", want: "system.ini"},
		{name: "混合分隔", in: `foo/../bar\\baz.txt`, goos: "windows", want: "baz.txt"},
		{name: "仅两点", in: "..", goos: "linux", want: "_"},
		{name: "仅一点", in: ".", goos: "windows", want: "_"},
		{name: "空名", in: "", goos: "linux", want: "_"},
		{name: "仅正斜杠", in: "/", goos: "linux", want: "_"},
		{name: "仅反斜杠", in: `\`, goos: "windows", want: "_"},
		{name: "一串分隔符", in: `///\\`, goos: "linux", want: "_"},
		{name: "盘符路径", in: `C:\Windows\system.ini`, goos: "windows", want: "system.ini"},
		{name: "保留名 CON", in: "CON", goos: "windows", want: "CON_"},
		{name: "保留名大小写", in: "con", goos: "windows", want: "con_"},
		{name: "保留名带扩展", in: "NUL.txt", goos: "windows", want: "NUL_.txt"},
		{name: "保留名 PRN", in: "PRN.log", goos: "windows", want: "PRN_.log"},
		{name: "保留名 AUX", in: "aux", goos: "windows", want: "aux_"},
		{name: "保留名 COM0", in: "COM0", goos: "windows", want: "COM0_"},
		{name: "保留名 COM1", in: "COM1.dat", goos: "windows", want: "COM1_.dat"},
		{name: "保留名 LPT0", in: "lpt0.txt", goos: "windows", want: "lpt0_.txt"},
		{name: "保留名 LPT9", in: "LPT9", goos: "windows", want: "LPT9_"},
		{name: "COM10 不是保留名", in: "COM10.bin", goos: "windows", want: "COM10.bin"},
		{name: "Unix 不改 CON", in: "CON", goos: "linux", want: "CON"},
		{name: "尾随点", in: "file.", goos: "windows", want: "file"},
		{name: "尾随空格", in: "file ", goos: "windows", want: "file"},
		{name: "尾随点空格", in: "file.  ", goos: "windows", want: "file"},
		{name: "Unix 保留尾随点", in: "file.", goos: "linux", want: "file."},
		{name: "仅点空格", in: " . ", goos: "windows", want: "_"},
		{name: "相对穿越后是保留名", in: `../CON`, goos: "windows", want: "CON_"},
		{name: "重复映射到同一名字", in: "foo:bar", goos: "windows", want: "foo_bar"},
		{name: "重复映射另一非法字符", in: "foo?bar", goos: "windows", want: "foo_bar"},
		{name: "截断后去掉尾随空格", in: strings.Repeat("a", 200) + strings.Repeat(" ", 60) + "x", goos: "windows", want: strings.Repeat("a", 200)},
		{name: "截断后重现保留名", in: "CON" + strings.Repeat(" ", 300) + "x", goos: "windows", want: "CON_"},
		{name: "截断后重现 NUL", in: "nul" + strings.Repeat(" ", 300) + "x", goos: "windows", want: "nul_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeDownloadFilename(tt.in, tt.goos)
			if got != tt.want {
				t.Fatalf("sanitizeDownloadFilename(%q, %q)=%q, want %q", tt.in, tt.goos, got, tt.want)
			}
		})
	}
}

func TestSanitizeDownloadFilenameCollisionMapping(t *testing.T) {
	t.Parallel()
	a := sanitizeDownloadFilename("foo:bar.bin", "windows")
	b := sanitizeDownloadFilename("foo|bar.bin", "windows")
	if a != "foo_bar.bin" || b != "foo_bar.bin" {
		t.Fatalf("expected collision mapping, got %q and %q", a, b)
	}
}

func TestSanitizeDownloadFilenameTruncatesLongName(t *testing.T) {
	t.Parallel()
	longBase := strings.Repeat("a", 300)
	got := sanitizeDownloadFilename(longBase+".avi", "linux")
	if len(got) > downloadFilenameMaxBytes {
		t.Fatalf("length %d exceeds %d", len(got), downloadFilenameMaxBytes)
	}
	if !strings.HasSuffix(got, ".avi") {
		t.Fatalf("expected to keep extension, got %q", got)
	}
	if got != strings.Repeat("a", downloadFilenameMaxBytes-4)+".avi" {
		t.Fatalf("unexpected truncated name %q", got)
	}

	winGot := sanitizeDownloadFilename(longBase+":bad.avi", "windows")
	if len(winGot) > downloadFilenameMaxBytes {
		t.Fatalf("windows length %d exceeds %d", len(winGot), downloadFilenameMaxBytes)
	}
	if !strings.HasSuffix(winGot, ".avi") {
		t.Fatalf("expected windows name to keep extension, got %q", winGot)
	}
}

func TestSanitizeDownloadFilenamePublicUsesRuntimeGOOS(t *testing.T) {
	t.Parallel()
	if got := SanitizeDownloadFilename("movie.avi"); got != "movie.avi" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeDownloadFilename("../x.bin"); got != "x.bin" {
		t.Fatalf("traversal: got %q", got)
	}
	if got := SanitizeDownloadFilename("/"); got != "_" {
		t.Fatalf("slash-only: got %q", got)
	}
}
