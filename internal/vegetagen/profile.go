package vegetagen

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Profile describes a load: the methods with their weights and an optional contract filter.
//
//	[methods]
//	eth_call              35.8%
//	eth_getBlockByNumber  15%    full=true
//
//	[contracts]
//	0xdac17f958d2ee523a2206206994597c13d831ec7  USDT
type Profile struct {
	Methods   []MethodSpec
	Contracts []string // lower case
}

type MethodSpec struct {
	Method  string
	Weight  float64 // relative, the weights need not sum to 100
	Options map[string]string
}

var addressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

func LoadProfile(path string) (Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return Profile{}, err
	}
	defer f.Close()
	p, err := ParseProfile(f)
	if err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

func ParseProfile(r io.Reader) (Profile, error) {
	var (
		p       Profile
		section string
		seen    = map[string]bool{}
	)
	sc := bufio.NewScanner(r)
	for lineNum := 1; sc.Scan(); lineNum++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section != "methods" && section != "contracts" {
				return p, fmt.Errorf("line %d: unknown section [%s], want [methods] or [contracts]", lineNum, section)
			}
			continue
		}
		fields := strings.Fields(line)
		switch section {
		case "methods":
			spec, err := parseMethod(fields)
			if err != nil {
				return p, fmt.Errorf("line %d: %w", lineNum, err)
			}
			key := strings.Join(append([]string{fields[0]}, fields[2:]...), " ")
			if seen[key] {
				return p, fmt.Errorf("line %d: duplicated %s", lineNum, key)
			}
			seen[key] = true
			p.Methods = append(p.Methods, spec)
		case "contracts":
			if !addressRe.MatchString(fields[0]) {
				return p, fmt.Errorf("line %d: invalid address %q", lineNum, fields[0])
			}
			p.Contracts = append(p.Contracts, strings.ToLower(fields[0]))
		default:
			return p, fmt.Errorf("line %d: %q is outside a section", lineNum, line)
		}
	}
	if err := sc.Err(); err != nil {
		return p, err
	}
	if len(p.Methods) == 0 {
		return p, errors.New("no methods: the profile needs a [methods] section")
	}
	return p, nil
}

func parseMethod(fields []string) (MethodSpec, error) {
	if len(fields) < 2 {
		return MethodSpec{}, fmt.Errorf("expected \"<method> <weight> [option=value ...]\", got %q", strings.Join(fields, " "))
	}
	weight, err := strconv.ParseFloat(strings.TrimSuffix(fields[1], "%"), 64)
	if err != nil {
		return MethodSpec{}, fmt.Errorf("invalid weight %q", fields[1])
	}
	if weight <= 0 {
		return MethodSpec{}, fmt.Errorf("weight of %s must be positive, got %v", fields[0], weight)
	}
	options := map[string]string{}
	for _, opt := range fields[2:] {
		key, value, ok := strings.Cut(opt, "=")
		if !ok {
			return MethodSpec{}, fmt.Errorf("invalid option %q: want key=value", opt)
		}
		options[key] = value
	}
	if _, err := NewGenerator(fields[0], options); err != nil {
		return MethodSpec{}, err
	}
	return MethodSpec{Method: fields[0], Weight: weight, Options: options}, nil
}

// Specs builds the generators of the profile, named after the method and its options.
func (p Profile) Specs() ([]Spec, error) {
	specs := make([]Spec, 0, len(p.Methods))
	for _, m := range p.Methods {
		gen, err := NewGenerator(m.Method, m.Options)
		if err != nil {
			return nil, err
		}
		parts := []string{m.Method}
		for _, key := range slices.Sorted(maps.Keys(m.Options)) {
			parts = append(parts, key+"="+m.Options[key])
		}
		specs = append(specs, Spec{Name: strings.Join(parts, " "), Gen: gen, Weight: m.Weight})
	}
	return specs, nil
}
