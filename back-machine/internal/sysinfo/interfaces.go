package sysinfo

import "net"

// Interface 本机网卡及其可用地址。
type Interface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Loopback  bool     `json:"loopback"`
}

// Interfaces 枚举本机网卡与地址，供入站监听地址选择。
func Interfaces() []Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	out := make([]Interface, 0, len(ifaces))
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		loopback := ifi.Flags&net.FlagLoopback != 0

		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		list := make([]string, 0, len(addrs))
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.IsLinkLocalUnicast() {
				continue
			}
			list = append(list, ip.String())
		}
		if len(list) == 0 {
			continue
		}
		out = append(out, Interface{Name: ifi.Name, Addresses: list, Loopback: loopback})
	}
	return out
}
