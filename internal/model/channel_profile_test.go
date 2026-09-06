package model

import "testing"

func TestAccountRegionFlag(t *testing.T) {
	for _, tt := range []struct{ region, want string }{
		{"United States", "🇺🇸"}, {"Japan", "🇯🇵"}, {"Türkiye", "🇹🇷"},
		{"Turkey", "🇹🇷"}, {"Korea", "🇰🇷"}, {" gb ", "🇬🇧"}, {"Europe", ""},
		{"Earth", ""}, {"", ""}, {"United States / Canada", ""},
	} {
		if got := AccountRegionFlag(tt.region); got != tt.want {
			t.Errorf("AccountRegionFlag(%q) = %q, want %q", tt.region, got, tt.want)
		}
	}
}
