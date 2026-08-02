package goed2k

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
)

const defaultIPFilterLevel = 127

// IPFilterRange 表示 ipfilter.dat 中的一条 IP 范围规则。
type IPFilterRange struct {
	Start       net.IP
	End         net.IP
	AccessLevel int
	Description string
}

// IPFilter 维护 eMule 风格 IP 过滤规则；命中且 AccessLevel < FilterLevel 时拒绝连接。
type IPFilter struct {
	ranges      []IPFilterRange
	filterLevel int
	// legacy 简单拒绝列表（无 access level），始终拒绝。
	networks []*net.IPNet
	singles  []net.IP
}

func NewIPFilter() *IPFilter {
	return &IPFilter{filterLevel: defaultIPFilterLevel}
}

// FilterLevel 返回当前过滤级别（默认 127）。
func (f *IPFilter) FilterLevel() int {
	if f == nil || f.filterLevel == 0 {
		return defaultIPFilterLevel
	}
	return f.filterLevel
}

// SetFilterLevel 设置过滤级别：AccessLevel < level 的区间将被拒绝。
func (f *IPFilter) SetFilterLevel(level int) {
	if f == nil {
		return
	}
	f.filterLevel = level
}

// LoadIPFilter 从文件加载 IP 过滤规则，自动识别 eMule ipfilter.dat 或简单文本格式。
func LoadIPFilter(path string) (*IPFilter, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseIPFilterBytes(raw)
}

// ParseIPFilterBytes 根据内容自动选择解析器。
func ParseIPFilterBytes(raw []byte) (*IPFilter, error) {
	text := string(raw)
	if looksLikeEmuleIPFilter(text) {
		return ParseEmuleIPFilter(text, defaultIPFilterLevel)
	}
	return ParseIPFilter(text)
}

func looksLikeEmuleIPFilter(text string) bool {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, " - ") && strings.Contains(line, ",") {
			return true
		}
		if strings.Contains(line, ":") && strings.Contains(line, " - ") {
			return true
		}
		return false
	}
	return false
}

// ParseEmuleIPFilter 解析 eMule/aMule PeerGuardian 格式 ipfilter.dat。
// 格式：RangeStart - RangeEnd , AccessLevel , Description
// 或 AntiP2P：Description : RangeStart - RangeEnd（AccessLevel 视为 0）
func ParseEmuleIPFilter(text string, filterLevel int) (*IPFilter, error) {
	if filterLevel <= 0 {
		filterLevel = defaultIPFilterLevel
	}
	filter := &IPFilter{filterLevel: filterLevel}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rng, err := parseEmuleIPFilterLine(line)
		if err != nil {
			continue
		}
		filter.ranges = append(filter.ranges, rng)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return filter, nil
}

func parseEmuleIPFilterLine(line string) (IPFilterRange, error) {
	if idx := strings.Index(line, ":"); idx > 0 && strings.Contains(line, " - ") {
		desc := strings.TrimSpace(line[:idx])
		rest := strings.TrimSpace(line[idx+1:])
		start, end, err := parseIPRange(rest)
		if err != nil {
			return IPFilterRange{}, err
		}
		return IPFilterRange{Start: start, End: end, AccessLevel: 0, Description: desc}, nil
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return IPFilterRange{}, errInvalidIPFilterLine
	}
	start, end, err := parseIPRange(strings.TrimSpace(parts[0]))
	if err != nil {
		return IPFilterRange{}, err
	}
	levelStr := strings.TrimSpace(parts[1])
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		return IPFilterRange{}, err
	}
	desc := ""
	if len(parts) >= 3 {
		desc = strings.TrimSpace(strings.Join(parts[2:], ","))
		desc = strings.Trim(desc, `"`)
	}
	return IPFilterRange{Start: start, End: end, AccessLevel: level, Description: desc}, nil
}

var errInvalidIPFilterLine = strconv.ErrSyntax

func parseIPRange(spec string) (net.IP, net.IP, error) {
	spec = strings.TrimSpace(spec)
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		ip := parseEmuleIP(strings.TrimSpace(spec))
		if ip == nil {
			return nil, nil, &net.AddrError{Err: "invalid IP", Addr: spec}
		}
		return ip, ip, nil
	}
	start := parseEmuleIP(strings.TrimSpace(left))
	end := parseEmuleIP(strings.TrimSpace(strings.TrimPrefix(right, " ")))
	if start == nil || end == nil {
		return nil, nil, &net.AddrError{Err: "invalid IP range", Addr: spec}
	}
	if bytesCompareIP(start, end) > 0 {
		start, end = end, start
	}
	return start, end, nil
}

func parseEmuleIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.To16()
	}
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return nil
	}
	var octets [4]byte
	for i, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return nil
		}
		octets[i] = byte(n)
	}
	return net.IPv4(octets[0], octets[1], octets[2], octets[3]).To16()
}

func bytesCompareIP(a, b net.IP) int {
	a = a.To16()
	b = b.To16()
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func ipInRange(ip, start, end net.IP) bool {
	ip = ip.To16()
	start = start.To16()
	end = end.To16()
	if ip == nil || start == nil || end == nil {
		return false
	}
	return bytesCompareIP(ip, start) >= 0 && bytesCompareIP(ip, end) <= 0
}

// ParseIPFilter 解析简单文本过滤规则（每行 CIDR 或单 IP，无条件拒绝）。
func ParseIPFilter(text string) (*IPFilter, error) {
	lines := strings.Split(text, "\n")
	filter := NewIPFilter()
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "/") {
			_, network, err := net.ParseCIDR(line)
			if err != nil {
				return nil, err
			}
			filter.networks = append(filter.networks, network)
			continue
		}
		ip := net.ParseIP(line)
		if ip == nil {
			return nil, &net.AddrError{Err: "invalid IP filter entry", Addr: line}
		}
		filter.singles = append(filter.singles, ip)
	}
	return filter, nil
}

func (f *IPFilter) IsEmpty() bool {
	return f == nil || (len(f.ranges) == 0 && len(f.networks) == 0 && len(f.singles) == 0)
}

func (f *IPFilter) Contains(ip net.IP) bool {
	if f == nil || ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
	}
	level := f.FilterLevel()
	for _, rng := range f.ranges {
		if ipInRange(ip, rng.Start, rng.End) {
			return rng.AccessLevel < level
		}
	}
	for _, single := range f.singles {
		if single.Equal(ip) {
			return true
		}
	}
	for _, network := range f.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Ranges 返回 eMule 格式规则副本。
func (f *IPFilter) Ranges() []IPFilterRange {
	if f == nil {
		return nil
	}
	out := make([]IPFilterRange, len(f.ranges))
	copy(out, f.ranges)
	return out
}
