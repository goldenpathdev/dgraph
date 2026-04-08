/*
 * SPDX-FileCopyrightText: © 2026 OWLGraph Contributors
 * SPDX-License-Identifier: Apache-2.0
 */

package query

import (
	"math"

	"github.com/dgraph-io/dgraph/v25/dql"
)

// RewriteTransitivePaths scans a parsed GraphQuery tree for transitive path
// predicates (pred* or pred*N) and converts them to @recurse directives
// scoped to a single predicate. This leverages the existing recurse
// infrastructure for iterative graph expansion.
//
// Example: { q(func: uid(0x1)) { locatedIn* { name } } }
// becomes: { q(func: uid(0x1)) @recurse { locatedIn { name } } }
//
// This is called after parsing and before query execution.
func RewriteTransitivePaths(gqs []*dql.GraphQuery) {
	for _, gq := range gqs {
		rewriteTransitiveChildren(gq)
	}
}

func rewriteTransitiveChildren(gq *dql.GraphQuery) {
	for _, child := range gq.Children {
		if child.TransitivePath {
			// Convert parent to use @recurse scoped to this predicate.
			// If the parent doesn't already have recurse, set it up.
			if !gq.Recurse {
				gq.Recurse = true
				depth := uint64(math.MaxUint64)
				if child.PathDepth > 0 {
					depth = child.PathDepth
				}
				gq.RecurseArgs = dql.RecurseArgs{
					Depth: depth,
				}
			}
			// Clear the transitive flag since we're now using recurse
			child.TransitivePath = false
			child.PathDepth = 0
		}
		// Recurse into grandchildren
		rewriteTransitiveChildren(child)
	}
}
