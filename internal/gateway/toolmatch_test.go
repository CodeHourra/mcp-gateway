package gateway

import (
	"slices"
	"testing"
)

func TestResolveToolNameAcrossGatewaySurfaces(t *testing.T) {
	// The same echo tool lives on two servers: the aggregate surface lists it
	// as "server__tool", a caller that only knew it before proxying says
	// "echo", and the progressive surface ids it as "server:echo".
	catalog := []string{"catalog00__lookup_00", "catalog01__lookup_00", "catalog00__create_01"}

	cases := []struct {
		name       string
		want       string
		resolved   string
		ambiguous  []string
		unresolved bool
	}{
		{name: "aggregate name still resolves", want: "catalog00__lookup_00", resolved: "catalog00__lookup_00"},
		{name: "progressive id resolves to aggregate name", want: "catalog00:lookup_00", resolved: "catalog00__lookup_00"},
		{name: "renamed server hits the same tool", want: "catalog01:lookup_00", resolved: "catalog01__lookup_00"},
		{name: "unique bare name resolves", want: "create_01", resolved: "catalog00__create_01"},
		{name: "same tool name on two servers is ambiguous", want: "lookup_00", ambiguous: []string{"catalog00__lookup_00", "catalog01__lookup_00"}},
		{name: "unknown tool stays unresolved", want: "missing", unresolved: true},
		{name: "empty name stays unresolved", want: "  ", unresolved: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			resolved, ambiguous := resolveToolName(test.want, catalog)
			if resolved != test.resolved {
				t.Fatalf("resolved %q, want %q", resolved, test.resolved)
			}
			if !slices.Equal(ambiguous, test.ambiguous) {
				t.Fatalf("ambiguous %v, want %v", ambiguous, test.ambiguous)
			}
		})
	}
}

func TestResolveToolNameToleratesSpellingAndCase(t *testing.T) {
	catalog := []string{"stdio-probe__echo", "stdio-probe__Echo_Tool"}
	resolved, ambiguous := resolveToolName("STDIO-PROBE:ECHO", catalog)
	if resolved != "stdio-probe__echo" || ambiguous != nil {
		t.Fatalf("case and separator changes failed: %q %v", resolved, ambiguous)
	}
	if resolved, _ := resolveToolName("echo_tool", catalog); resolved != "stdio-probe__Echo_Tool" {
		t.Fatalf("bare name failed: %q", resolved)
	}
	// A shorter name that only prefixes another tool must not fall back to it.
	if resolved, _ := resolveToolName("echo_x", catalog); resolved != "" {
		t.Fatalf("invented a match: %q", resolved)
	}
}
