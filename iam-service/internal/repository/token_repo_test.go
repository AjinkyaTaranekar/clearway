package repository

import "testing"

func TestNormalizeIPAddress(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "ipv4 host and port", raw: "10.0.1.6:42172", want: "10.0.1.6"},
		{name: "ipv4 plain", raw: "10.0.1.6", want: "10.0.1.6"},
		{name: "ipv6 host and port", raw: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "ipv6 plain", raw: "2001:db8::1", want: "2001:db8::1"},
		{name: "forwarded chain", raw: "10.0.1.6, 10.0.1.1", want: "10.0.1.6"},
		{name: "quoted value", raw: "\"10.0.1.6:8080\"", want: "10.0.1.6"},
		{name: "invalid value", raw: "unknown", want: ""},
		{name: "empty value", raw: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeIPAddress(tt.raw)
			if got != tt.want {
				t.Fatalf("normalizeIPAddress(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNullableINET(t *testing.T) {
	if got := nullableINET("10.0.1.6:42172"); got != "10.0.1.6" {
		t.Fatalf("nullableINET valid input = %#v, want %q", got, "10.0.1.6")
	}

	if got := nullableINET("not-an-ip"); got != nil {
		t.Fatalf("nullableINET invalid input = %#v, want nil", got)
	}
}
