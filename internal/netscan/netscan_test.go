package netscan

import "testing"

func TestVendorAndPrivate(t *testing.T) {
	if v := Vendor("e4:82:10:74:2d:7c"); v != "Huawei Technologies" {
		t.Errorf("vendor = %q", v)
	}
	if v := Vendor("1C-98-EC-0F-BA-34"); v != "Hewlett Packard Enterprise" {
		t.Errorf("vendor = %q", v)
	}
	if !PrivateMAC("be:1a:c9:62:bb:c1") || PrivateMAC("1c:98:ec:0f:ba:34") || Vendor("be:1a:c9:62:bb:c1") != "" {
		t.Error("locally administered addresses are private and have no vendor")
	}
	if NormalizeMAC("BE-1A-C9-62-BB-C1") != "be:1a:c9:62:bb:c1" || NormalizeMAC("nope") != "" {
		t.Error("normalize")
	}
}

func TestGuessKind(t *testing.T) {
	cases := map[[4]string]string{
		{"Mayas-iPhone", "", "", ""}:                      "phone",
		{"Bjelke's Apple Watch", "", "", ""}:              "watch",
		{"truenas", "Hewlett Packard Enterprise", "", ""}: "nas",
		{"", "Espressif", "", ""}:                         "smart_home",
		{"", "Huawei Technologies", "", ""}:               "router",
		{"abjelosevic-macbook-pro-m4", "", "", "macOS"}:   "laptop",
		{"living-room-tv", "", "", "Android"}:             "tv",
		{"", "", "", ""}:                                  "",
		{"DESKTOP-7Q2M4", "ASRock Incorporation", "", ""}: "computer",
		{"BRW3C2AF4", "Brother Industries", "", ""}:       "printer",
		{"odin2-portal", "", "", "Android"}:               "console",
	}
	for in, want := range cases {
		if got := GuessKind(in[0], in[1], in[2], in[3]); got != want {
			t.Errorf("GuessKind(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	if CleanName("Mayas-iPhone.local.") != "Mayas-iPhone" || CleanName("_gateway.") != "" || CleanName("nas.lan") != "nas" {
		t.Error("CleanName")
	}
	for n, want := range map[string]bool{"Bjelkes-iPhone": true, "iPhone": false, "android-1a2b3c4d": false, "MAC-391C42": true, "(name not found)": false, "pc": false} {
		if DistinctName(n) != want {
			t.Errorf("DistinctName(%q) != %v", n, want)
		}
	}
	if NameKey("Bjelke's Apple Watch") != NameKey("bjelkes-apple-watch") {
		t.Error("NameKey")
	}
}

func TestParseSubnet(t *testing.T) {
	if _, err := ParseSubnet("192.168.1.0/24"); err != nil {
		t.Error(err)
	}
	if _, err := ParseSubnet("10.0.0.0/8"); err == nil {
		t.Error("a /8 is too big to sweep")
	}
	n, _ := ParseSubnet("10.0.0.0/30")
	if h := hostsOf(n); len(h) != 2 || h[0].String() != "10.0.0.1" {
		t.Errorf("hosts = %v", h)
	}
	if reverseName("192.168.1.20") != "20.1.168.192.in-addr.arpa." {
		t.Error(reverseName("192.168.1.20"))
	}
}
