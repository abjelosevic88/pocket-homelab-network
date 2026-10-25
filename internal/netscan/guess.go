package netscan

import (
	"regexp"
	"strings"
)

// Kinds are the device types offered in the app, in menu order.
var Kinds = []string{"phone", "tablet", "laptop", "computer", "watch", "tv", "speaker", "console", "printer", "camera", "smart_home", "router", "access_point", "switch", "server", "nas", "car", "other"}

// KindName is the plain name of a kind.
var KindName = map[string]string{
	"phone": "Phone", "tablet": "Tablet", "laptop": "Laptop", "computer": "Computer", "watch": "Watch", "tv": "TV or streamer",
	"speaker": "Speaker", "console": "Game console", "printer": "Printer", "camera": "Camera", "smart_home": "Smart home",
	"router": "Router", "access_point": "Wi-Fi access point", "switch": "Network switch", "server": "Server", "nas": "NAS (storage)",
	"car": "Car", "other": "Other",
}

// ValidKind reports a known kind ("" = not known yet).
func ValidKind(k string) bool {
	if k == "" {
		return true
	}
	_, ok := KindName[k]
	return ok
}

type rule struct {
	re   *regexp.Regexp
	kind string
}

// matched against "hostname | model"; the system only decides when the names say nothing
var nameRules = []rule{
	{regexp.MustCompile(`(?i)truenas|synology|diskstation|qnap|\bnas\b|unraid|freenas|omv`), "nas"},
	{regexp.MustCompile(`(?i)server|proxmox|\bpve\b|esxi|ilo\b|idrac|ipmi|nixos|ubuntu|debian|raspberrypi|rpi|homelab`), "server"},
	{regexp.MustCompile(`(?i)watch`), "watch"},
	{regexp.MustCompile(`(?i)ipad|tablet|galaxy-?tab|\btab[-_ ]?s\d|kindle|fire-?hd`), "tablet"},
	{regexp.MustCompile(`(?i)iphone|android|galaxy|pixel|redmi|xiaomi|oneplus|huawei-?p\d|mate\d|nokia|motorola|moto-|oppo|vivo|realme|phone|\bsm-[a-z]\d`), "phone"},
	{regexp.MustCompile(`(?i)macbook|thinkpad|laptop|notebook|zenbook|vivobook|xps|yoga|surface|chromebook|ideapad|latitude|elitebook|probook|spectre|envy|pavilion|\bx-l-`), "laptop"},
	{regexp.MustCompile(`(?i)imac|mac-?mini|mac-?pro|mac-?studio|desktop|-pc\b|\bpc\b|workstation|tower`), "computer"},
	{regexp.MustCompile(`(?i)apple-?tv|chromecast|roku|fire-?tv|firestick|shield|\btv\b|bravia|webos|tizen|android-?tv|living-room|samsung-?tv|lgtv|tvbox|mibox`), "tv"},
	{regexp.MustCompile(`(?i)homepod|sonos|echo|alexa|google-?home|nest-?(mini|audio|hub)|speaker|soundbar`), "speaker"},
	{regexp.MustCompile(`(?i)playstation|\bps[45]\b|xbox|nintendo|switch-?oled|steam-?deck|odin|retroid|ayn`), "console"},
	{regexp.MustCompile(`(?i)printer|laserjet|officejet|deskjet|envy-?\d|epson|canon|brother|\bhp[a-f0-9]{6}\b|mfc-|ecotank`), "printer"},
	{regexp.MustCompile(`(?i)camera|\bcam\b|ipcam|doorbell|reolink|hikvision|dahua|wyze|tapo-?c|eufy|arlo|ring-`), "camera"},
	{regexp.MustCompile(`(?i)\besp[-_]?[0-9a-f]*|shelly|tasmota|sonoff|tuya|hue|bridge|zigbee|plug|bulb|thermostat|vacuum|roborock|dreame|miio|yeelight|meross|switchbot|homeassistant|home-?assistant`), "smart_home"},
	{regexp.MustCompile(`(?i)\bap\b|access-?point|unifi-?ap|\buap\b|eap\d|deco|orbi|mesh|repeater|extender`), "access_point"},
	{regexp.MustCompile(`(?i)router|gateway|fritz|openwrt|gl-|glinet|mikrotik|edgerouter|usg|udm|opnsense|pfsense|archer`), "router"},
	{regexp.MustCompile(`(?i)switch|gs\d{3}|sg\d{3}|usw`), "switch"},
	{regexp.MustCompile(`(?i)tesla|\bcar\b|vehicle`), "car"},
}

var vendorRules = []rule{
	{regexp.MustCompile(`(?i)espressif|tuya|shelly|sonoff|itead|signify|philips lighting|lifx|ecobee|tado|netatmo|xiaomi.*iot|beijing xiaomi|roborock|dreame|aqara`), "smart_home"},
	{regexp.MustCompile(`(?i)raspberry|supermicro|hewlett packard enterprise|dell emc|asrock rack`), "server"},
	{regexp.MustCompile(`(?i)synology|qnap|western digital|seagate|asustor|terramaster`), "nas"},
	{regexp.MustCompile(`(?i)ubiquiti|mikrotik|tp-link|netgear|zyxel|avm|gl technologies|cisco|juniper|aruba|ruckus|cambium|tenda|linksys|huawei technologies|zte`), "router"},
	{regexp.MustCompile(`(?i)sonos|bose|harman|denon|yamaha|bang & olufsen|amazon technologies`), "speaker"},
	{regexp.MustCompile(`(?i)roku|lg electronics|vizio|hisense|tcl|sony|panasonic|sharp|philips`), "tv"},
	{regexp.MustCompile(`(?i)nintendo|sony interactive|microsoft.*xbox|valve`), "console"},
	{regexp.MustCompile(`(?i)brother|canon|epson|seiko epson|lexmark|kyocera|ricoh|xerox|konica`), "printer"},
	{regexp.MustCompile(`(?i)hikvision|dahua|reolink|axis communications|amcrest|wyze|ring llc|arlo`), "camera"},
	{regexp.MustCompile(`(?i)samsung|oneplus|oppo|vivo|realme|motorola|huawei device|honor|nothing technology`), "phone"},
	{regexp.MustCompile(`(?i)intel|asustek|asrock|gigabyte|micro-star|msi|lenovo|dell|hp|acer|apple`), "computer"},
}

// GuessKind guesses what a device is from its names, vendor, model and OS
// ("" when there is nothing to go on).
func GuessKind(hostname, vendor, model, os string) string {
	text := strings.Join([]string{hostname, model}, " | ")
	if strings.TrimSpace(strings.Trim(text, " |")) != "" {
		for _, r := range nameRules {
			if r.re.MatchString(text) {
				return r.kind
			}
		}
	}
	switch strings.ToLower(os) {
	case "ios":
		return "phone"
	case "ipados":
		return "tablet"
	case "android":
		return "phone"
	case "macos", "windows":
		return "laptop"
	case "linux", "freebsd":
		return "server"
	case "tvos":
		return "tv"
	}
	if vendor != "" {
		for _, r := range vendorRules {
			if r.re.MatchString(vendor) {
				return r.kind
			}
		}
	}
	return ""
}

// genericNames say nothing about which device it is (many devices share them).
var genericNames = regexp.MustCompile(`(?i)^(iphone|ipad|android|android-[0-9a-f]+|galaxy|localhost|unknown|esp_?[0-9a-f]*|espressif|mac|macbook|macbook-pro|macbook-air|windows|desktop|laptop|pc|host|device|client|\(name not found\)|<unknown>|name not found|\d+\.\d+\.\d+\.\d+)$`)

// DistinctName reports a hostname specific enough to recognise a device by
// it when its MAC changes ("Bjelkes-iPhone" yes, "iPhone" no).
func DistinctName(n string) bool {
	n = strings.TrimSpace(n)
	return len(n) >= 3 && !genericNames.MatchString(n)
}

// NameKey is the form names are compared in.
func NameKey(n string) string {
	n = strings.ToLower(CleanName(n))
	n = strings.NewReplacer("’", "", "'", "", " ", "-", "_", "-").Replace(n)
	return strings.Trim(n, "-")
}
