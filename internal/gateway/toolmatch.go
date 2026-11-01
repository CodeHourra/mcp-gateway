package gateway

import (
	"fmt"
	"slices"
	"strings"
)

// One proxied tool is written in three ways depending on where the caller
// learned its name: the aggregate entry lists "server__tool", the progressive
// entry addresses the same tool as "server:tool", and a caller that used the
// MCP server before it was proxied only knows the bare "tool". Matching all
// three against the same candidate list is what keeps a call on target when the
// gateway changes the surface.
//
// Resolution never invents a match: every tier stops at one candidate, and a
// name that could mean several tools is reported as ambiguous so the caller
// picks instead of the gateway calling the wrong tool.
func resolveToolName(want string, candidates []string) (resolved string, ambiguous []string) {
	want = strings.TrimSpace(want)
	if want == "" || len(candidates) == 0 {
		return "", nil
	}
	if slices.Contains(candidates, want) {
		return want, nil
	}

	// Tier 1: the same tool written with the other gateway separator, or with
	// a namespace the caller remembers from before the server was renamed.
	if server, tool, ok := splitToolName(want); ok {
		if matches := selectTools(candidates, func(name string) bool {
			return slices.Contains(separatorForms(want), name)
		}); len(matches) == 1 {
			return matches[0], nil
		} else if len(matches) > 1 {
			return "", matches
		}
		// The listed name carries no namespace (one server's own catalog), or
		// carries the namespace the caller used.
		matches := selectTools(candidates, func(name string) bool {
			if toolPart(name) != tool {
				return false
			}
			candidateServer, _, qualified := splitToolName(name)
			return !qualified || candidateServer == server
		})
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return "", matches
		}
	}

	// Tier 2: a bare tool name, usable only when one server exposes it.
	if _, _, ok := splitToolName(want); !ok {
		matches := selectTools(candidates, func(name string) bool { return toolPart(name) == want })
		if len(matches) == 0 {
			matches = selectTools(candidates, func(name string) bool { return strings.EqualFold(toolPart(name), want) })
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return "", matches
		}
	}

	// Tier 3: a unique spelling difference (whitespace already trimmed, case
	// still significant upstream).
	matches := selectTools(candidates, func(name string) bool {
		if strings.EqualFold(name, want) {
			return true
		}
		return slices.ContainsFunc(separatorForms(want), func(form string) bool { return strings.EqualFold(name, form) })
	})
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", matches
	}
	return "", nil
}

func selectTools(candidates []string, keep func(string) bool) []string {
	matches := []string{}
	for _, name := range candidates {
		if keep(name) && !slices.Contains(matches, name) {
			matches = append(matches, name)
		}
	}
	return matches
}

// splitToolName splits "server:tool" or "server__tool" at the first separator
// of either kind. A name without a separator, or with an empty side, is not a
// qualified tool id.
func splitToolName(name string) (server, tool string, ok bool) {
	separator, width := -1, 0
	for _, candidate := range []string{":", "__"} {
		if index := strings.Index(name, candidate); index >= 0 && (separator < 0 || index < separator) {
			separator, width = index, len(candidate)
		}
	}
	if separator <= 0 || separator+width >= len(name) {
		return "", "", false
	}
	return name[:separator], name[separator+width:], true
}

// separatorForms returns the qualified name in both gateway spellings.
func separatorForms(name string) []string {
	server, tool, ok := splitToolName(name)
	if !ok {
		return nil
	}
	return []string{server + ":" + tool, server + "__" + tool}
}

// toolPart returns the bare tool name of a possibly qualified one.
func toolPart(name string) string {
	if _, tool, ok := splitToolName(name); ok {
		return tool
	}
	return name
}

func unresolvedToolError(want string, candidates []string) error {
	if len(candidates) > 1 {
		listed := candidates
		if len(listed) > 6 {
			listed = listed[:6]
		}
		return fmt.Errorf("工具 %q 匹配到多个候选，请使用完整名称：%s", want, strings.Join(listed, "、"))
	}
	return fmt.Errorf("当前目录中没有工具 %q，请刷新目录", want)
}
