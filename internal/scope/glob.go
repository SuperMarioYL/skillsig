package scope

import "strings"

// This file is the single, boundary-aware definition of "does declaration D
// cover candidate C?" for the path-shaped axes (fs_write paths and
// network_egress hosts). Both the lock-aware verify path (scanner.go) and the
// `diff` path (diff.go) call covers* here so they can never disagree on what
// "broader" means.
//
// Why it exists: v0.2.0 made the cross-version diff "glob-aware" by stripping a
// trailing "*"/"**" off a declared entry and testing a raw strings.HasPrefix on
// the remainder. That is over-permissive in two ways that let a re-signed skill
// ESCALATE scope without being flagged SCOPE-DRIFTED — the exact attack skillsig
// exists to catch:
//
//  1. Prefix confusion. A declared egress glob "api.github.com*" raw-prefixes
//     the newly added host "api.github.com.attacker.net", and an fs_write glob
//     "/workspace/foo*" raw-prefixes "/workspace/foobar-evil". A raw HasPrefix
//     reports both as "already covered", so scopeGrowth returns empty and the
//     escalation is silent. The fix is a SEGMENT BOUNDARY: a glob prefix only
//     covers a candidate when the remainder after the prefix is empty, or begins
//     at a separator for that axis ('/' for fs paths, '.' for hosts).
//
//  2. "*" vs "**" collapse. v0.2.0 treated a trailing single "*" and "**"
//     identically (both reduced to a raw prefix), so "${WORKSPACE}/*" (intended:
//     direct children only) silently covered the deep path
//     "${WORKSPACE}/a/b/secret" and even a traversal "${WORKSPACE}/../etc".
//     The fix honors the difference: "**" is a recursive prefix; a single "*"
//     matches WITHIN ONE segment only (no further separator in the remainder),
//     and a ".." traversal segment is never covered.

// pathSep / hostSep are the segment separators for the two path-shaped axes.
const (
	pathSep = '/'
	hostSep = '.'
)

// fsPathCovered reports whether an fs_write candidate path is covered by any
// declared fs_write entry, with single-"*" / "**" semantics and a '/' segment
// boundary. A bare (non-glob) declaration covers only an exact match.
func fsPathCovered(declared []string, candidate string) bool {
	return anyCovers(declared, strings.TrimSpace(candidate), pathSep)
}

// hostCovered reports whether a network_egress candidate host is covered by any
// declared host entry, with single-"*" / "**" semantics and a '.' segment
// boundary so a look-alike host ("api.github.com.attacker.net") is NOT swallowed
// by a declared "api.github.com*".
func hostCovered(declared []string, candidate string) bool {
	return anyCovers(declared, strings.TrimSpace(candidate), hostSep)
}

func anyCovers(declared []string, candidate string, sep byte) bool {
	if candidate == "" {
		return true // an empty candidate adds nothing; never reported as growth
	}
	for _, d := range declared {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if covers(d, candidate, sep) {
			return true
		}
	}
	return false
}

// covers is the boundary-aware coverage predicate for one declared entry.
//
//   - No glob suffix: covers iff equal.
//   - "**" suffix (recursive): covers when candidate == prefix-without-trailing-sep,
//     or candidate starts with prefix AND (prefix already ends at a separator OR
//     the candidate's next char is the separator). No ".." segment is ever covered.
//   - single "*" suffix (one segment): same prefix/boundary rule, AND the matched
//     remainder must not cross another separator — i.e. "${WORKSPACE}/*" covers
//     "${WORKSPACE}/child" but NOT "${WORKSPACE}/child/grandchild".
func covers(declared, candidate string, sep byte) bool {
	prefix, kind := globKind(declared)
	switch kind {
	case globNone:
		return declared == candidate
	case globRecursive, globSingle:
		if !boundaryPrefix(prefix, candidate, sep) {
			return false
		}
		// The "parent itself" case (candidate == prefix without its trailing
		// separator) is covered with no remainder to inspect — candidate may be
		// shorter than prefix here, so guard the slice.
		if len(candidate) <= len(prefix) {
			return true
		}
		rem := candidate[len(prefix):]
		if hasTraversal(rem, sep) {
			return false // never let a ".." remainder count as covered
		}
		if kind == globSingle {
			// One segment only: the remainder, minus a single leading separator
			// it may share with the prefix, must contain no further separator.
			r := rem
			if len(r) > 0 && r[0] == sep {
				r = r[1:]
			}
			if strings.IndexByte(r, sep) >= 0 {
				return false
			}
		}
		return true
	}
	return false
}

// boundaryPrefix reports whether candidate is covered by prefix at a segment
// boundary: candidate equals prefix (modulo a trailing separator on the prefix),
// or candidate starts with prefix and the join point is a separator on one side.
func boundaryPrefix(prefix, candidate string, sep byte) bool {
	if prefix == "" {
		return true // "*" / "**" alone — declared everything on this axis
	}
	if candidate == prefix {
		return true
	}
	// Allow "dir/" prefix to cover "dir" exactly (the parent itself).
	if strings.TrimRight(prefix, string(sep)) == candidate {
		return true
	}
	if !strings.HasPrefix(candidate, prefix) {
		return false
	}
	// Join must be a real segment boundary: either the prefix already ends with
	// the separator, or the candidate's next char after the prefix is one.
	if prefix[len(prefix)-1] == sep {
		return true
	}
	return candidate[len(prefix)] == sep
}

// hasTraversal reports whether rem contains a ".." path/host segment, which must
// never be treated as a narrowing refinement of a declared scope.
func hasTraversal(rem string, sep byte) bool {
	for _, seg := range strings.Split(rem, string(sep)) {
		if seg == ".." {
			return true
		}
	}
	return false
}

type globKindT int

const (
	globNone globKindT = iota
	globSingle
	globRecursive
)

// globKind strips a trailing "**" or "*" off a declared path/host glob and
// reports which kind it was. "${WORKSPACE}/**" → ("${WORKSPACE}/", recursive);
// "${WORKSPACE}/*" → ("${WORKSPACE}/", single); "~/.claude/config" → (…, none).
func globKind(s string) (prefix string, kind globKindT) {
	switch {
	case strings.HasSuffix(s, "**"):
		return strings.TrimSuffix(s, "**"), globRecursive
	case strings.HasSuffix(s, "*"):
		return strings.TrimSuffix(s, "*"), globSingle
	default:
		return s, globNone
	}
}
