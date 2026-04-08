/*
 * SPDX-FileCopyrightText: © 2026 OWLGraph Contributors
 * SPDX-License-Identifier: Apache-2.0
 */

package dql

import (
	"testing"
)

// P2-T10: DQL Parser Accepts Path Syntax
func TestParseTransitivePath(t *testing.T) {
	query := `{ q(func: uid(0x1)) { locatedIn* { name } } }`
	res, err := Parse(Request{Str: query})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Query) == 0 {
		t.Fatal("expected at least one query")
	}
	gq := res.Query[0]
	if len(gq.Children) == 0 {
		t.Fatal("expected children")
	}

	child := gq.Children[0]
	if child.Attr != "locatedIn" {
		t.Errorf("attr = %q, want locatedIn", child.Attr)
	}
	if !child.TransitivePath {
		t.Error("TransitivePath should be true")
	}
	if child.PathDepth != 0 {
		t.Errorf("PathDepth = %d, want 0 (unlimited)", child.PathDepth)
	}
}

func TestParseBoundedTransitivePath(t *testing.T) {
	query := `{ q(func: uid(0x1)) { locatedIn*3 { name } } }`
	res, err := Parse(Request{Str: query})
	if err != nil {
		t.Fatal(err)
	}
	gq := res.Query[0]
	child := gq.Children[0]

	if child.Attr != "locatedIn" {
		t.Errorf("attr = %q, want locatedIn", child.Attr)
	}
	if !child.TransitivePath {
		t.Error("TransitivePath should be true")
	}
	if child.PathDepth != 3 {
		t.Errorf("PathDepth = %d, want 3", child.PathDepth)
	}
}

func TestParseNonTransitivePath(t *testing.T) {
	query := `{ q(func: uid(0x1)) { locatedIn { name } } }`
	res, err := Parse(Request{Str: query})
	if err != nil {
		t.Fatal(err)
	}
	gq := res.Query[0]
	child := gq.Children[0]

	if child.Attr != "locatedIn" {
		t.Errorf("attr = %q, want locatedIn", child.Attr)
	}
	if child.TransitivePath {
		t.Error("TransitivePath should be false for regular predicate")
	}
}
