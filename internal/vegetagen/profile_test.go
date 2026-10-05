package vegetagen

import (
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	const text = `# a provider-like load
[methods]
eth_call                35.8%
eth_getBlockByNumber    15%   full=true
eth_getBlockByNumber    4.9   full=false
net_listening           3.42%

[contracts]
0xdAC17F958D2ee523a2206206994597C13D831ec7   USDT
0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48
`
	p, err := ParseProfile(strings.NewReader(text))
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	if len(p.Methods) != 4 {
		t.Fatalf("got %d methods, want 4", len(p.Methods))
	}
	if m := p.Methods[0]; m.Method != "eth_call" || m.Weight != 35.8 {
		t.Errorf("first method %+v", m)
	}
	if m := p.Methods[1]; m.Method != "eth_getBlockByNumber" || m.Weight != 15 || m.Options["full"] != "true" {
		t.Errorf("second method %+v", m)
	}
	if m := p.Methods[2]; m.Weight != 4.9 || m.Options["full"] != "false" {
		t.Errorf("third method %+v", m)
	}
	want := []string{"0xdac17f958d2ee523a2206206994597c13d831ec7", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"}
	if strings.Join(p.Contracts, ",") != strings.Join(want, ",") {
		t.Errorf("contracts %v, want %v (lower case)", p.Contracts, want)
	}
}

func TestParseProfileSingleMethod(t *testing.T) {
	p, err := ParseProfile(strings.NewReader("[methods]\neth_call 100%\n"))
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	if len(p.Methods) != 1 || p.Methods[0].Method != "eth_call" || len(p.Contracts) != 0 {
		t.Errorf("profile %+v", p)
	}
}

func TestParseProfileErrors(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"no methods", "[contracts]\n0xdac17f958d2ee523a2206206994597c13d831ec7\n", "no methods"},
		{"line outside a section", "eth_call 1\n", "line 1"},
		{"unknown section", "[foo]\n", "unknown section"},
		{"unknown method", "[methods]\neth_foo 1\n", "unsupported method"},
		{"duplicated method", "[methods]\neth_call 1\neth_call 2\n", "duplicated"},
		{"bad weight", "[methods]\neth_call x\n", "invalid weight"},
		{"zero weight", "[methods]\neth_call 0\n", "positive"},
		{"bad option", "[methods]\neth_getBlockByNumber 1 full=maybe\n", "full"},
		{"unknown option", "[methods]\neth_call 1 foo=bar\n", "option"},
		{"bad address", "[methods]\neth_call 1\n[contracts]\n0x1234\n", "address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseProfile(strings.NewReader(tt.text))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
}
