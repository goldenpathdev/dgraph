# OWLGraph — Implementation Plan

## Overview

OWLGraph is a fork of Dgraph that adds an OWL2 RL reasoning layer, making it a native ontology-aware graph database. OWL ontologies become the schema source of truth, compiling down to Dgraph's existing predicate/type/facet primitives. **No Badger storage engine changes required.**

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Client Layer                       │
│         DQL + extensions  │  GraphQL (auto-gen)      │
├─────────────────────────────────────────────────────┤
│              OWL Reasoning Layer (NEW)               │
│  ┌───────────┐ ┌──────────────┐ ┌────────────────┐  │
│  │ Ontology  │ │ Materializer │ │ Query Rewriter │  │
│  │ Store     │ │ (write-time) │ │ (read-time)    │  │
│  └───────────┘ └──────────────┘ └────────────────┘  │
│              OWL→Schema Compiler (NEW)               │
├─────────────────────────────────────────────────────┤
│             Existing Dgraph Core (minimal changes)   │
├─────────────────────────────────────────────────────┤
│          Badger Storage Engine (unchanged)            │
└─────────────────────────────────────────────────────┘
```

## Target OWL Profile

**OWL2 RL** (Rule Language) — tractable (polynomial time), suitable for materialization.

Supports: subClassOf, equivalentClass, disjointWith, subPropertyOf, inverseOf, transitiveProperty, symmetricProperty, functionalProperty, domain, range, hasValue, someValuesFrom (limited), allValuesFrom (limited), propertyChain.

Does NOT support: full cardinality restrictions in superclass position, complex class expressions as superclasses, nominals, arbitrary unions in superclass position, full OWL DL tableau reasoning.

## Phase 0: OWL Parser & Internal Representation (4-6 weeks)

**Goal**: Standalone Go package to parse OWL ontologies and compute reasoning closures. No Dgraph coupling.

**Risk**: Low — fully isolated.

### New Files

```
owl/
  ir.go              — Go structs: Class, ObjectProperty, DataProperty, Restriction, ClassExpression
  parser_turtle.go   — Turtle format parser
  parser_rdfxml.go   — RDF/XML format parser
  reasoner.go        — OWL2 RL structural reasoner
  profile.go         — OWL2 RL profile validation (reject unsupported constructs)
  parser_test.go     — Parser tests
  reasoner_test.go   — Reasoner tests
```

### Key Deliverables

1. Go structs for OWL2 constructs
2. Turtle parser (primary) + RDF/XML parser
3. Subsumption DAG computation (class hierarchy as adjacency list)
4. Transitive closure computation
5. Domain/range inference
6. OWL2 RL profile validation (clear errors for unsupported constructs)
7. Tests against Pizza, Wine, FOAF, Schema.org ontologies

## Phase 1: Ontology Storage & Schema Compilation (6-8 weeks)

**Goal**: Store OWL ontology in Dgraph and compile it to native Dgraph schema.

**Risk**: Medium — edge cases in OWL-to-flat-predicate mapping.

**Key insight**: Schema is stored via `schema.State().Set()` and `schema.State().SetType()` which write to Badger through existing key encoding. The OWL compiler outputs `[]*pb.SchemaUpdate` and `[]*pb.TypeUpdate` — the same structures as `schema.Parse()`.

### New Files

```
owl/
  compiler/
    dgraph.go        — OWL → pb.SchemaUpdate/pb.TypeUpdate compiler
    dgraph_test.go   — Compiler tests
```

### Changes to Existing Files

- `schema/schema.go` — Add `owl.*` reserved predicates alongside `dgraph.*`
- `edgraph/server.go` — New `POST /ontology` endpoint
- `dgraph/cmd/` — New `dgraph ontology load|validate|diff` CLI command

### Compilation Rules

| OWL Construct | Dgraph Schema Output |
|---|---|
| `owl:Class Foo` | `type Foo { ... }` |
| `rdfs:subClassOf Foo → Bar` | Foo includes all predicates of Bar (flattened) |
| `owl:ObjectProperty hasX, domain A, range B` | `hasX: uid @reverse .` with type constraint |
| `owl:DatatypeProperty name, range xsd:string` | `name: string .` |
| `owl:FunctionalProperty` | `List = false` in SchemaUpdate |
| `owl:TransitiveProperty` | Metadata flag stored in `owl.*` predicates |
| `owl:inverseOf p → q` | Both predicates created |

### Ontology Storage

Store ontology as Dgraph triples using reserved `owl.*` predicates:
```
_:Animal <owl.type> "Class" .
_:Dog <owl.subClassOf> _:Animal .
_:hasOwner <owl.type> "ObjectProperty" .
_:hasOwner <owl.domain> _:Animal .
_:hasOwner <owl.range> _:Person .
```

This uses existing storage — replicated via Raft for free, survives restarts, queryable via DQL.

## Phase 2: Subsumption-Aware Queries (8-10 weeks)

**Goal**: `type(Animal)` returns Dogs, GoldenRetrievers, etc. Transitive property paths work in DQL.

**Risk**: HIGH — critical path. Transitive queries can explode on dense graphs.

### 2a. Write-Time Type Materialization

**Hook point**: `edgraph/server.go:556` — after `ToDirectedEdges()`, before validation.

```go
edges, err := query.ToDirectedEdges(qc.gmuList, newUids)
// NEW: Add inferred type edges
edges, err = owl.MaterializeTypes(ctx, edges)
```

When `dgraph.type = "GoldenRetriever"` is set, the materializer appends edges for `"Dog"`, `"Mammal"`, `"Animal"`. These flow through the existing pipeline unchanged.

**Why materialize vs query-rewrite**: Dgraph's posting list architecture means `type(Animal)` is a single index lookup. Rewriting to `type(Animal) OR type(Dog) OR ...` enumerates the full hierarchy at query time, breaking plan caching. Write amplification is ~50-100 bytes per ancestor type — negligible.

### 2b. DQL Extensions

**Parser changes** (`dql/parser.go`, `dql/state.go`):
- New tokens: `*` (transitive), `*N` (bounded)
- New AST fields on GraphQuery: `TransitivePath bool`, `PathDepth uint64`

**Query execution** (`query/query.go`):
- New handler modeled on `query/recurse.go:19-165` but scoped to a single predicate
- Cycle detection via `reachMap` (same pattern as recurse)
- Depth bounds

**New function**: `exactType()` — direct `eq(dgraph.type, X)` without subsumption expansion.

### New/Modified Files

```
owl/materializer/types.go       — Type hierarchy materializer
owl/materializer/types_test.go  — Tests
dql/parser.go                   — New tokens and AST fields
dql/state.go                    — New lexer tokens
query/transitive.go             — Transitive path execution (modeled on recurse.go)
query/query.go                  — exactType() function, transitive path dispatch
```

## Phase 3: OWL→GraphQL Compiler (6-8 weeks, parallel with Phase 2)

**Goal**: Auto-generate GraphQL schema from OWL ontology.

**Risk**: Medium — OWL multiple inheritance doesn't always map cleanly.

**Key insight**: We generate GraphQL SDL that feeds into the existing `graphql/schema/schemagen.go:NewHandler()` pipeline. No modification to existing GraphQL machinery needed.

### Translation Rules

| OWL | GraphQL |
|---|---|
| `owl:Class` with subclasses | `interface` + `type` |
| `rdfs:subClassOf` | `implements` |
| `owl:inverseOf` | `@hasInverse` directive (already exists!) |
| `owl:FunctionalProperty` | singular field (`T` not `[T]`) |
| Non-functional property | list field (`[T]`) |
| `owl:TransitiveProperty` | field + `*Path` accessor field |
| `owl:unionOf` | GraphQL `union` type |

### Existing Dgraph Features We Leverage

- `graphql/schema/gqlschema.go:738-792` — `expandSchema()` already copies interface fields to implementing types
- `@hasInverse` directive already exists for bidirectional fields
- Filter types, input types, payload types all auto-generated by `completeSchema()`

### New Files

```
owl/graphql/
  generator.go            — OWL IR → GraphQL SDL string
  generator_test.go       — Tests
  introspection.go        — OWLClass, OWLProperty introspection types
  introspection_test.go   — Tests
```

## Phase 4: Write-Time Reasoning Engine (8-12 weeks)

**Goal**: Full materialization pipeline triggered on every mutation.

**Risk**: HIGHEST — transaction size explosion, distributed consistency.

### Architecture

```
Mutation arrives
    │
    ▼
Pre-Commit Hook (edgraph/server.go:556)
    │
    ▼
Classify: Does mutation affect OWL-managed predicates?
    │ Yes
    ▼
Inference Engine
  1. Type hierarchy materialization
  2. Inverse property materialization
  3. Transitive closure update
  4. Domain/range inference
  5. Symmetric property
  6. Property chain resolution
  7. Disjointness validation
    │
    ▼
Augmented Mutation (original + inferred edges)
    │
    ▼
Existing pipeline (Raft, posting lists, Badger)
```

### Key Decisions

- **Inferred facet**: `owl.inferred=true` on all materialized triples (uses existing facet storage)
- **Circuit breaker**: Configurable max inferred triples per mutation
- **Retroactive materialization**: Background job modeled on Dgraph's existing background indexing
- **Idempotent**: Safe to re-run materialization

### New Files

```
owl/materializer/
  engine.go        — Rule engine orchestrator
  engine_test.go
  rules.go         — Individual OWL2 RL rules
  rules_test.go
  retroactive.go   — Background rematerialization job
```

## Phase 5: Schema Introspection (3-4 weeks)

**Goal**: Query the ontology itself through DQL.

**Risk**: Low — syntactic sugar over existing `owl.*` predicate queries.

### New DQL Functions

- `owl.subClassOf(className, transitive)` — returns sub/super classes
- `owl.propertiesOf(className)` — returns properties including inherited
- `owl.validate(uid)` — validates instance against declared types

### Changes

- `dql/parser.go` — Recognize `owl.*` namespace functions
- `query/query.go` — Expand to queries against `owl.*` predicates

## Critical Path

```
Phase 0 ──→ Phase 1 ──→ Phase 2 ──→ Phase 4 ──→ Phase 5
 4-6w        6-8w        8-10w       8-12w       3-4w
                          ↕
                      Phase 3 (parallel)
                        6-8w

Total critical path: ~30-40 weeks
```

## Risk Registry

| Risk | Severity | Mitigation |
|---|---|---|
| Transitive closure explosion on dense graphs | Critical | Depth bounds (default 20). Circuit breaker. Lazy materialization option. |
| Write amplification degrades throughput | High | Benchmark per OWL profile. Deferred materialization mode. Batch inference. |
| Retroactive materialization = full-graph scan | High | Incremental: track changed classes/properties. Background job with resume. |
| OWL constructs that don't map cleanly | Medium | Define supported OWL2 RL subset. Reject unsupported at parse time. |
| Distributed consistency of inferred triples | High | All inference in same Raft transaction. No cross-shard inference in v1. |
| Backward compatibility regression | Medium | OWL uses reserved `owl.*` namespace. Existing schemas never modified. |

## Key Dgraph Code Landmarks

| What | Where | Relevance |
|---|---|---|
| `type()` → `eq(dgraph.type, X)` | `query/query.go:333-340` | Subsumption hook |
| `dgraph.type` predicate def | `schema/schema.go:776-781` | String, indexed, list |
| Mutation hook point | `edgraph/server.go:556` | After ToDirectedEdges, before validation |
| Recurse implementation | `query/recurse.go:19-165` | Template for transitive paths |
| GraphQL schema gen | `graphql/schema/schemagen.go:510-755` | OWL→GraphQL plugs in here |
| Schema completion | `graphql/schema/gqlschema.go:940-1056` | Auto-generates queries/mutations |
| Interface field copy | `graphql/schema/gqlschema.go:738-792` | Already handles implements |
| DQL parser entry | `dql/parser.go:615` | Extend for path syntax |
| Schema state | `schema/schema.go:59-67` | Add ontology field |
