package goed2k

import (
	"net"
	"os"
	"strings"
)

// IPFilter 维护拒绝列表：命中 CIDR 或单 IP 的地址将被 policy 在拨号前过滤。
type IPFilter struct {
	networks []*net.IPNet
	singles  []net.IP
}

func NewIPFilter() *IPFilter {
	return &IPFilter{}
}

// LoadIPFilter 从文本文件加载过滤规则，每行一个 IPv4/IPv6 地址或 CIDR，# 开头为注释。
func LoadIPFilter(path string) (*IPFilter, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseIPFilter(string(raw))
}

// ParseIPFilter 解析过滤规则文本。
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
	return f == nil || (len(f.networks) == 0 && len(f.singles) == 0)
}

func (f *IPFilter) Contains(ip net.IP) bool {
	if f == nil || ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
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
