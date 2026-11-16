package client

import (
	"net"

	"github.com/dengyie/mangohub/hub/database/clients"
	"github.com/dengyie/mangohub/hub/internal/config"
	"github.com/dengyie/mangohub/hub/utils/geoip"
)

func getClientIPType(ip net.IP) int {
	// 0:ipv4 1:ipv6 -1:错误的输入
	if ip == nil {
		return -1
	}
	if ip.To4() == nil {
		return 1
	} else {
		return 0
	}
}

// agentBasicInfoColumns is the allowlist of columns an agent may write via
// its basic-info report. The payload used to flow into GORM Updates(map)
// verbatim, so a client token could mass-assign admin-owned columns — flip
// its own hidden flag, rotate its token, spoof name/group/price/region and
// impersonate another node in the public list. Anything outside this set is
// dropped; admins set those columns through the admin API instead.
var agentBasicInfoColumns = map[string]struct{}{
	"cpu_name":           {},
	"cpu_cores":          {},
	"cpu_physical_cores": {},
	"arch":               {},
	"os":                 {},
	"kernel_version":     {},
	"ipv4":               {},
	"ipv6":               {},
	"mem_total":          {},
	"swap_total":         {},
	"disk_total":         {},
	"gpu_name":           {},
	"virtualization":     {},
	"version":            {},
	"miner_configured":   {},
	"miner_controllable": {},
}

func saveClientBasicInfo(rawInfo map[string]interface{}, uuid string, fallbackIP string) error {
	info := make(map[string]interface{}, len(rawInfo)+2)
	for key, value := range rawInfo {
		if _, ok := agentBasicInfoColumns[key]; ok {
			info[key] = value
		}
	}
	info["uuid"] = uuid
	applyFallbackClientIP(info, fallbackIP)
	appendClientRegionFromGeoIP(info)
	return clients.SaveClientInfo(info)
}

func applyFallbackClientIP(info map[string]interface{}, fallbackIP string) {
	if hasClientIP(info) {
		return
	}
	ip := net.ParseIP(fallbackIP)

	switch getClientIPType(ip) {
	case 0:
		info["ipv4"] = fallbackIP
	case 1:
		info["ipv6"] = fallbackIP
	}
}

func hasClientIP(info map[string]interface{}) bool {
	if ipv4, ok := info["ipv4"].(string); ok && ipv4 != "" {
		return true
	}
	if ipv6, ok := info["ipv6"].(string); ok && ipv6 != "" {
		return true
	}
	return false
}

func appendClientRegionFromGeoIP(info map[string]interface{}) {
	cfg, err := config.GetAs[bool](config.GeoIpEnabledKey)
	if err != nil || !cfg {
		return
	}

	for _, key := range []string{"ipv4", "ipv6"} {
		ipStr, ok := info[key].(string)
		if !ok || ipStr == "" {
			continue
		}
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		record, _ := geoip.GetGeoInfo(ip)
		if record == nil {
			continue
		}
		region := geoip.GetRegionUnicodeEmoji(record.ISOCode)
		if region == "" {
			continue
		}
		info["region"] = region
		return
	}
}
