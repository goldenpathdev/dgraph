# OWLGraph

OWL2 RL ontology-aware graph database, built on [Dgraph](https://dgraph.io).

OWLGraph adds an OWL (Web Ontology Language) reasoning layer on top of Dgraph's
distributed graph storage. Load an ontology in Turtle format, and the system
automatically compiles it to Dgraph schema, materializes type hierarchies at
write time, and enables subsumption queries -- all without modifying the
underlying storage engine.

---

## Table of Contents

- [Quick Start](#quick-start)
- [Concepts](#concepts)
- [Architecture](#architecture)
- [Building from Source](#building-from-source)
- [Loading an Ontology](#loading-an-ontology)
- [Write-Time Materialization](#write-time-materialization)
- [Querying](#querying)
- [Introspection API](#introspection-api)
- [OWL-to-Dgraph Mapping](#owl-to-dgraph-mapping)
- [OWL-to-GraphQL Mapping](#owl-to-graphql-mapping)
- [CLI Reference](#cli-reference)
- [HTTP API Reference](#http-api-reference)
- [DQL Extensions](#dql-extensions)
- [OWL2 RL Profile](#owl2-rl-profile)
- [Testing](#testing)
- [Files Modified in Dgraph Core](#files-modified-in-dgraph-core)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

This walkthrough starts a cluster, loads an ontology, inserts data, and runs a
subsumption query. It assumes you have a built binary and Docker installed.

### 1. Start the dev cluster

```bash
./scripts/build.sh cluster-up
```

This launches one Dgraph Zero and one Dgraph Alpha using the local Docker image.
Wait for the health check to pass:

```bash
curl http://localhost:8080/health
```

### 2. Load an ontology

The test ontology defines an animal taxonomy:

```
Animal > Mammal > Dog > GoldenRetriever, Labrador
Animal > Mammal > Cat
Animal > Bird > Parrot
Person, Place > City, Country
```

Load it:

```bash
curl -X POST http://localhost:8080/ontology \
  -H "Content-Type: text/turtle" \
  --data-binary @owl/testdata/animals.ttl
```

Response:

```json
{
  "classes": 11,
  "objectProperties": 5,
  "dataProperties": 4,
  "compiledSchema": "...",
  "status": "success",
  "message": "Ontology loaded and materializer activated"
}
```

The system has:
- Parsed the Turtle file into OWL internal representation
- Compiled classes to Dgraph types and properties to predicates
- Applied the schema via `/alter`
- Initialized the write-time reasoning engine
- Persisted the ontology for restart recovery

### 3. Insert data

Insert a GoldenRetriever. The materializer automatically adds `Dog`, `Mammal`,
and `Animal` to `dgraph.type`:

```bash
curl -X POST http://localhost:8080/mutate?commitNow=true \
  -H "Content-Type: application/json" \
  -d '{
    "set": [
      {
        "dgraph.type": "GoldenRetriever",
        "name": "Fido",
        "breed": "Golden"
      },
      {
        "dgraph.type": "Cat",
        "name": "Whiskers"
      }
    ]
  }'
```

After this mutation, `Fido` has `dgraph.type` values:
`GoldenRetriever`, `Dog`, `Mammal`, `Animal`.

`Whiskers` has: `Cat`, `Mammal`, `Animal`.

### 4. Query with subsumption

Query all mammals. This returns both Fido and Whiskers because the materializer
ensured they both carry the `Mammal` type:

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/dql" \
  -d '{ mammals(func: type(Mammal)) { name dgraph.type } }'
```

```json
{
  "data": {
    "mammals": [
      { "name": "Fido", "dgraph.type": ["GoldenRetriever", "Dog", "Mammal", "Animal"] },
      { "name": "Whiskers", "dgraph.type": ["Cat", "Mammal", "Animal"] }
    ]
  }
}
```

### 5. Introspect the ontology

```bash
curl "http://localhost:8080/ontology/introspect?class=Dog"
```

```json
{
  "iri": "Dog",
  "superClasses": ["Mammal", "Animal"],
  "subClasses": ["GoldenRetriever", "Labrador"],
  "disjointWith": ["Cat"],
  "properties": [
    {"iri": "hasOwner", "type": "ObjectProperty", "domain": ["Animal"], "range": ["Person"], "isFunctional": true},
    {"iri": "name", "type": "DataProperty", "range": ["string"], "isFunctional": true},
    {"iri": "breed", "type": "DataProperty", "domain": ["Dog"], "range": ["string"], "isFunctional": true}
  ]
}
```

---

## Concepts

### Subsumption

Subsumption is the core reasoning operation. Class A *subsumes* class B when B is
a subclass of A (directly or transitively). In OWLGraph, `type(A)` matches all
nodes typed as B because the materializer writes A onto every B node at insert
time.

### Materialization

Rather than computing type hierarchies at query time, OWLGraph *materializes*
inferred facts during mutation processing. When you write
`dgraph.type = "GoldenRetriever"`, the engine adds `Dog`, `Mammal`, and
`Animal` as additional type edges on the same node -- before the data reaches
Badger. This makes queries fast: `type(Animal)` is a simple index lookup.

### OWL2 RL Profile

OWLGraph implements the OWL2 RL (Rule Language) profile. RL is designed for
materialization-based reasoning with polynomial-time complexity. It supports
class hierarchies, property characteristics, domain/range inference, and
selected class expressions -- but not full OWL DL tableau reasoning.

---

## Architecture

OWL reasoning is a layer on top of Dgraph's existing storage. No Badger changes
were made. The reasoning engine hooks into `edgraph/server.go`'s mutation
pipeline after `ToDirectedEdges` and before validation. Only 93 lines of
existing Dgraph code were modified across 6 files.

### Package Structure

```
owl/                        Core OWL package (no Dgraph dependencies)
  parser.go                 Turtle format parser
  parser_turtle.go          Turtle tokenizer
  ir.go                     Internal representation (Class, Property, Restriction structs)
  reasoner.go               Subsumption DAG, transitive closure, property reasoning
  profile.go                OWL2 RL profile validation
  testdata/animals.ttl      Test ontology

owl/compiler/               OWL-to-Dgraph schema compiler
  dgraph.go                 Classes to types, properties to predicates

owl/materializer/           Write-time inference engine
  types.go                  TypeMaterializer (global singleton, concurrency-safe)
  engine.go                 Full Engine: hierarchy, domain/range, inverse, symmetric,
                            property chains, disjointness, circuit breaker, delete cascades
  retroactive.go            Backfill existing data when ontology is loaded
  integration_test.go       Live cluster integration tests

owl/graphql/                OWL-to-GraphQL SDL generator
  generator.go              subClassOf to implements, inverseOf to @hasInverse

owl/introspect/             Ontology introspection API
  introspect.go             Class info, property lookup, hierarchy traversal
```

### Mutation Pipeline

```
Client mutation
  |
  v
edgraph/server.go: Mutate()
  |
  v
ToDirectedEdges()           Convert JSON/RDF to internal edge representation
  |
  v
owl/materializer: Engine    <-- OWLGraph hook (15 lines in server.go)
  |  1. Type hierarchy       (GoldenRetriever -> add Dog, Mammal, Animal)
  |  2. Domain inference      (hasOwner -> subject gets Animal type)
  |  3. Range inference       (hasOwner -> object gets Person type)
  |  4. Inverse properties    (hasOwner -> create isOwnerOf reverse edge)
  |  5. Symmetric properties  (friendOf -> create reverse friendOf edge)
  |  6. Property chains       (chain axiom match -> create derived edge)
  |  7. Disjointness check    (Dog + Cat on same node -> reject mutation)
  |  0. Delete cascades       (remove GoldenRetriever -> remove Dog, Mammal, Animal)
  |
  v
Validation, indexing, commit to Badger (unchanged Dgraph code)
```

The engine is a global singleton (`materializer.GetGlobalEngine()`) initialized
when an ontology is loaded. If no ontology is loaded, the hook is a no-op.

---

## Building from Source

### Prerequisites

- Go 1.26.1 or later
- GCC (for cgo / jemalloc)
- Docker (for dev cluster)
- jemalloc (auto-built by the Makefile)

### Build commands

```bash
# Full build: compile binary + run all OWL tests
./scripts/build.sh

# Build the dgraph binary only
./scripts/build.sh build

# Alternative: use Make directly
make dgraph

# Compile check + run tests (no binary output -- fast CI gate)
./scripts/build.sh verify

# Run only owl/ package tests
./scripts/build.sh test

# Run all owl/* tests (including compiler, materializer, graphql, introspect)
./scripts/build.sh test-all

# Build local Docker image from the compiled binary
./scripts/build.sh image
# or: make local-image
```

### Dev cluster

```bash
# Start 1 Zero + 1 Alpha
./scripts/build.sh cluster-up

# Stop the cluster
./scripts/build.sh cluster-down

# Check health
./scripts/build.sh cluster-health
```

Endpoints when running:

| Service    | Protocol | Address              | Purpose                  |
|------------|----------|----------------------|--------------------------|
| Alpha HTTP | HTTP     | `localhost:8080`     | Queries, mutations, ontology |
| Alpha gRPC | gRPC     | `localhost:9080`     | dgo client connections   |
| Zero HTTP  | HTTP     | `localhost:6080`     | Cluster administration   |

---

## Loading an Ontology

### Via HTTP

```bash
curl -X POST http://localhost:8080/ontology \
  -H "Content-Type: text/turtle" \
  --data-binary @your-ontology.ttl
```

This single request:

1. **Parses** the Turtle file into OWL internal representation (`owl.Parser`)
2. **Validates** the OWL2 RL profile (circular subclass detection, conflicting property characteristics)
3. **Compiles** classes to Dgraph types and properties to predicates (`owl/compiler`)
4. **Applies** the compiled schema to Dgraph via the internal `/alter` pathway
5. **Persists** the raw Turtle data in Dgraph as an `OWLGraphMeta` node for restart recovery
6. **Initializes** the global reasoning engine (`owl/materializer.Engine`)
7. **Initializes** the introspection inspector (`owl/introspect.Inspector`)
8. **Triggers** retroactive materialization for any pre-existing data

### Validate without applying

```bash
curl -X POST "http://localhost:8080/ontology?validate=true" \
  -H "Content-Type: text/turtle" \
  --data-binary @your-ontology.ttl
```

Returns the compiled schema and class counts without applying changes or
activating the reasoning engine.

### Via CLI

```bash
# Load an ontology into a running cluster
dgraph ontology load animals.ttl --alpha http://localhost:8080

# Validate only (no side effects)
dgraph ontology validate animals.ttl --alpha http://localhost:8080
```

### Ontology persistence

The raw Turtle data is stored in Dgraph as a mutation. On Alpha restart,
`TryLoadPersistedOntology()` queries for the stored ontology and re-initializes
the reasoning engine automatically. No manual reload is needed after a restart.

---

## Write-Time Materialization

Every mutation that passes through the Alpha is intercepted by the reasoning
engine. The engine examines the directed edges and appends additional inferred
edges before the mutation is committed.

### Inference rules (applied in order)

#### Rule 0: Delete cascades

When a type is removed from a node (`dgraph.type DEL "GoldenRetriever"`), the
engine also removes all ancestor types (`Dog`, `Mammal`, `Animal`).

#### Rule 1: Type hierarchy (subClassOf)

When `dgraph.type` is set to `"GoldenRetriever"`, the engine queries the
reasoner for all superclasses and adds type edges for each:

```
Input:   <0x1> <dgraph.type> "GoldenRetriever"
Inferred: <0x1> <dgraph.type> "Dog"          (owl.inferred=true)
          <0x1> <dgraph.type> "Mammal"       (owl.inferred=true)
          <0x1> <dgraph.type> "Animal"       (owl.inferred=true)
```

#### Rule 2: Domain inference

If a property has `rdfs:domain`, using that property on a node infers the
domain type on the subject. For example, `hasOwner` has domain `Animal`:

```
Input:   <0x1> <hasOwner> <0x2>
Inferred: <0x1> <dgraph.type> "Animal"       (owl.inferred=true)
```

Domain inference also materializes ancestor types of the domain class.

#### Rule 3: Range inference

If a property has `rdfs:range`, using that property infers the range type on the
object. For example, `hasOwner` has range `Person`:

```
Input:   <0x1> <hasOwner> <0x2>
Inferred: <0x2> <dgraph.type> "Person"       (owl.inferred=true)
```

#### Rule 4: Inverse properties (inverseOf)

When a property with `owl:inverseOf` is used, the engine creates the reverse edge:

```
Input:   <0x1> <hasOwner> <0x2>     (hasOwner inverseOf isOwnerOf)
Inferred: <0x2> <isOwnerOf> <0x1>    (owl.inferred=true)
```

#### Rule 5: Symmetric properties

When a symmetric property is used, the engine creates the reverse edge with the
same predicate:

```
Input:   <0x1> <friendOf> <0x2>     (friendOf is SymmetricProperty)
Inferred: <0x2> <friendOf> <0x1>     (owl.inferred=true)
```

#### Rule 6: Property chains

When a property chain axiom is declared (e.g., `hasGrandparent = hasParent o hasParent`),
and matching edges exist in the same mutation batch, the engine derives the
chain result:

```
Input:   <0x3> <hasParent> <0x2>
         <0x2> <hasParent> <0x1>
Inferred: <0x3> <hasGrandparent> <0x1>  (owl.inferred=true)
```

Property chain resolution currently handles length-2 chains within a single
mutation batch.

#### Rule 7: Disjointness validation

After all inference is applied, the engine checks that no node carries two types
declared as `owl:disjointWith`. If violated, the entire mutation is rejected with
an error:

```
owl/materializer: disjointness violation on entity 0x1:
  type "Dog" is disjoint with type "Cat"
```

### Inferred edge facet

All materialized edges carry an `owl.inferred=true` facet. This allows
distinguishing asserted facts from inferred facts in queries:

```graphql
{
  q(func: type(Dog)) {
    name
    dgraph.type @facets(owl.inferred)
  }
}
```

### Circuit breaker

If a single mutation would generate more than 10,000 inferred edges, the
materialization is aborted and the mutation is rejected. This prevents runaway
inference in pathological ontologies.

The limit is configurable via `materializer.MaxInferredEdgesPerMutation`.

### Retroactive materialization

When an ontology is loaded into a cluster that already contains data, the system
triggers a background process that scans existing nodes and adds missing ancestor
type edges. This runs asynchronously and does not block the ontology load
response.

---

## Querying

### Subsumption queries with type()

The standard Dgraph `type()` function works as a subsumption query because
ancestor types are materialized:

```graphql
# All animals (Dogs, Cats, Birds, and anything else under Animal)
{ q(func: type(Animal)) { name dgraph.type } }

# All dogs (GoldenRetrievers, Labradors, and any other Dog subclass)
{ q(func: type(Dog)) { name breed } }

# Only golden retrievers
{ q(func: type(GoldenRetriever)) { name breed } }
```

### exactType() -- asserted types only

`exactType(X)` matches nodes where `X` was the directly asserted type, not an
inferred ancestor. A node typed as `GoldenRetriever` matches `type(Dog)` but
does not match `exactType(Dog)`:

```graphql
# Only nodes explicitly typed as Dog (not GoldenRetriever or Labrador)
{ q(func: exactType(Dog)) { name } }
```

Implementation note: `exactType` currently resolves to an `eq` filter on
`dgraph.type`. Future versions will filter on the `owl.inferred` facet for
precise asserted-only semantics.

### Transitive property paths (pred*)

Follow a transitive property until no more edges exist:

```graphql
# Follow locatedIn transitively (City -> Country -> Continent -> ...)
{
  q(func: eq(name, "San Francisco")) {
    name
    locatedIn* {
      name
    }
  }
}
```

Bounded transitive path with a maximum depth:

```graphql
# Follow locatedIn up to 2 hops
{
  q(func: eq(name, "San Francisco")) {
    name
    locatedIn*2 {
      name
    }
  }
}
```

The `*` suffix and `*N` bounded form are parsed in `dql/parser.go` and executed
in `query/query.go`.

---

## Introspection API

The introspection API provides read-only access to the loaded ontology structure.

### HTTP endpoint: GET /ontology/introspect

#### List all classes

```bash
curl http://localhost:8080/ontology/introspect
```

#### Get a specific class

```bash
curl "http://localhost:8080/ontology/introspect?class=Dog"
```

Returns:

```json
{
  "iri": "Dog",
  "superClasses": ["Mammal", "Animal"],
  "subClasses": ["GoldenRetriever", "Labrador"],
  "disjointWith": ["Cat"],
  "properties": [...]
}
```

#### Get subclasses

```bash
# Direct subclasses only
curl "http://localhost:8080/ontology/introspect?subclasses=Animal"

# Transitive subclasses (all descendants)
curl "http://localhost:8080/ontology/introspect?subclasses=Animal&transitive=true"
```

#### Get superclasses

```bash
curl "http://localhost:8080/ontology/introspect?superclasses=GoldenRetriever&transitive=true"
```

#### Get properties for a class

```bash
curl "http://localhost:8080/ontology/introspect?properties=Dog"
```

Returns all properties applicable to Dog, including those inherited from
superclasses (Animal, Mammal).

### CLI

```bash
# List all classes
dgraph ontology introspect --alpha http://localhost:8080

# Inspect a specific class
dgraph ontology introspect --class Dog --alpha http://localhost:8080
```

---

## OWL-to-Dgraph Mapping

The compiler (`owl/compiler`) translates OWL constructs to Dgraph schema
primitives.

| OWL Construct | Dgraph Equivalent | Notes |
|---|---|---|
| `owl:Class` | `type` | Each class becomes a Dgraph type |
| `rdfs:subClassOf` | Fields inherited from parent type | Child type includes all parent fields |
| `owl:ObjectProperty` | `uid` predicate | Links between nodes |
| `owl:DatatypeProperty` | Scalar predicate | `string`, `int`, `float`, `bool`, `datetime` |
| `owl:FunctionalProperty` | `List=false` (singular) | At most one value |
| Non-functional property | `List=true` | Multiple values, enables `@count` |
| `owl:inverseOf` | `@reverse` directive + materialized reverse edges | Both Dgraph reverse index and explicit edge |
| `owl:TransitiveProperty` | Metadata flag | Used by `pred*` query syntax |
| `owl:SymmetricProperty` | Auto-materialized reverse edges | Written at mutation time |
| `owl:disjointWith` | Validation on write | Rejects mutations that create conflicting types |
| `owl:propertyChainAxiom` | Materialized chain edges | Derived edges written at mutation time |

### XSD-to-Dgraph datatype mapping

| XSD Type | Dgraph Type |
|---|---|
| `xsd:string` (or unspecified) | `string` |
| `xsd:integer` | `int` |
| `xsd:float` | `float` |
| `xsd:boolean` | `bool` |
| `xsd:date` | `datetime` |
| `xsd:dateTime` | `datetime` |

### Example compiled schema

Given the animals.ttl ontology, the compiler produces:

```
birthDate: datetime .
breed: string .
friendOf: uid @count .
hasOwner: uid @reverse .
isOwnerOf: uid @count .
livesIn: uid @reverse .
locatedIn: uid @count .
name: string .
weight: float .

type Animal {
  birthDate
  hasOwner
  name
  weight
}

type Dog {
  birthDate
  breed
  hasOwner
  name
  weight
}

type GoldenRetriever {
  birthDate
  breed
  hasOwner
  name
  weight
}
```

Dog inherits `birthDate`, `hasOwner`, `name`, and `weight` from Animal. GoldenRetriever
inherits all of those plus `breed` from Dog.

---

## OWL-to-GraphQL Mapping

The GraphQL generator (`owl/graphql`) produces Dgraph-compatible GraphQL SDL
from the ontology.

| OWL Construct | GraphQL Equivalent | Notes |
|---|---|---|
| `owl:Class` with subclasses | `interface` + `type` | Classes that are superclasses become interfaces |
| `owl:Class` (leaf) | `type` only | No interface generated |
| `rdfs:subClassOf` | `implements` | Child type implements parent interface |
| `owl:ObjectProperty` | Object field | `fieldName: RangeType` or `fieldName: [RangeType]` |
| `owl:DatatypeProperty` | Scalar field | `fieldName: String`, `Int`, `Float`, `Boolean`, `DateTime` |
| `owl:FunctionalProperty` | Singular field | `field: T` (not `[T]`) |
| Non-functional property | List field | `field: [T]` |
| `owl:inverseOf` | `@hasInverse` directive | `field: Type @hasInverse(field: "inverseField")` |

### XSD-to-GraphQL scalar mapping

| XSD Type | GraphQL Scalar |
|---|---|
| `xsd:string` (or unspecified) | `String` |
| `xsd:integer` | `Int` |
| `xsd:float` | `Float` |
| `xsd:boolean` | `Boolean` |
| `xsd:date`, `xsd:dateTime` | `DateTime` |

### Example generated SDL

```graphql
interface Animal {
  id: ID!
  birthDate: DateTime
  hasOwner: Person @hasInverse(field: "isOwnerOf")
  name: String
  weight: Float
}

interface Mammal implements Animal {
  id: ID!
  birthDate: DateTime
  hasOwner: Person @hasInverse(field: "isOwnerOf")
  name: String
  weight: Float
}

type Dog implements Mammal & Animal {
  id: ID!
  birthDate: DateTime
  breed: String
  hasOwner: Person @hasInverse(field: "isOwnerOf")
  name: String
  weight: Float
}

type GoldenRetriever implements Mammal & Animal {
  id: ID!
  birthDate: DateTime
  breed: String
  hasOwner: Person @hasInverse(field: "isOwnerOf")
  name: String
  weight: Float
}

type Cat implements Mammal & Animal {
  id: ID!
  birthDate: DateTime
  hasOwner: Person @hasInverse(field: "isOwnerOf")
  name: String
  weight: Float
}
```

---

## CLI Reference

All CLI commands communicate with a running Alpha node over HTTP.

### dgraph ontology load

```
dgraph ontology load <file.ttl> [flags]

Flags:
  -a, --alpha string   Dgraph Alpha HTTP address (default "http://localhost:8080")
```

Loads an OWL ontology from a Turtle file. Compiles the ontology to Dgraph
schema, applies it, and activates the reasoning engine.

### dgraph ontology validate

```
dgraph ontology validate <file.ttl> [flags]

Flags:
  -a, --alpha string   Dgraph Alpha HTTP address (default "http://localhost:8080")
```

Validates an ontology without applying it. Reports class counts and any profile
violations.

### dgraph ontology introspect

```
dgraph ontology introspect [flags]

Flags:
      --class string   Specific class to inspect
  -a, --alpha string   Dgraph Alpha HTTP address (default "http://localhost:8080")
```

Queries the loaded ontology structure. Without `--class`, lists all classes.

---

## HTTP API Reference

### POST /ontology

Load an OWL ontology.

| Parameter | Location | Description |
|---|---|---|
| Body | Request body | Turtle-format ontology |
| `Content-Type` | Header | `text/turtle` or `application/x-turtle` |
| `validate` | Query string | Set to `true` for validation-only mode |

**Success response** (200):

```json
{
  "classes": 11,
  "objectProperties": 5,
  "dataProperties": 4,
  "compiledSchema": "<full Dgraph schema string>",
  "status": "success",
  "message": "Ontology loaded and materializer activated"
}
```

**Validation-only response** (200):

```json
{
  "classes": 11,
  "objectProperties": 5,
  "dataProperties": 4,
  "compiledSchema": "<full Dgraph schema string>",
  "status": "valid",
  "message": "Ontology is valid (not applied)"
}
```

### GET /ontology/introspect

Query the loaded ontology structure. All parameters are mutually exclusive.

| Parameter | Type | Description |
|---|---|---|
| (none) | -- | List all classes with properties |
| `class` | string | Get full info for a specific class |
| `subclasses` | string | Get subclasses of a class |
| `superclasses` | string | Get superclasses of a class |
| `properties` | string | Get properties applicable to a class |
| `transitive` | bool | When used with `subclasses` or `superclasses`, include transitive results |

Returns 200 with JSON. Returns an error if no ontology is loaded.

---

## DQL Extensions

OWLGraph adds two DQL extensions to the standard Dgraph query language.

### exactType(ClassName)

A filter function that matches nodes whose `dgraph.type` was explicitly asserted
as `ClassName`, excluding nodes that only have `ClassName` via materialization.

```graphql
{
  # Returns only nodes explicitly typed as Dog
  # (not GoldenRetriever or Labrador nodes that inherited Dog)
  dogs(func: exactType(Dog)) {
    name
    breed
  }
}
```

### pred* and pred*N (transitive property paths)

Follow a predicate transitively through multiple hops.

```graphql
# Unbounded: follow locatedIn until no more edges
{
  q(func: eq(name, "San Francisco")) {
    locatedIn* { name }
  }
}

# Bounded: follow locatedIn for at most 3 hops
{
  q(func: eq(name, "San Francisco")) {
    locatedIn*3 { name }
  }
}
```

The parser recognizes the `*` token after a predicate name in the lexer
(`dql/state.go`) and stores it as `TransitivePath=true` with optional
`PathDepth` in the AST (`dql/parser.go`).

---

## OWL2 RL Profile

OWLGraph implements the OWL2 RL (Rule Language) profile, which is designed for
materialization-based reasoning with polynomial-time complexity.

### Supported constructs

| Construct | Category | Implementation |
|---|---|---|
| `rdfs:subClassOf` | Class hierarchy | Reasoner transitive closure + materializer |
| `owl:equivalentClass` | Class equivalence | Stored in IR |
| `owl:disjointWith` | Disjointness | Validated at write time |
| `owl:inverseOf` | Property inversion | Materialized reverse edges |
| `owl:TransitiveProperty` | Transitivity | `pred*` query syntax |
| `owl:SymmetricProperty` | Symmetry | Materialized reverse edges |
| `owl:FunctionalProperty` | Cardinality | Schema: `List=false` |
| `owl:InverseFunctionalProperty` | Cardinality | Metadata flag |
| `rdfs:domain` | Domain | Materialized type edges |
| `rdfs:range` | Range | Materialized type edges |
| `owl:someValuesFrom` | Existential | Stored in IR, class expression |
| `owl:allValuesFrom` | Universal | Stored in IR, class expression |
| `owl:hasValue` | Value restriction | Stored in IR, class expression |
| `owl:propertyChainAxiom` | Property chains | Materialized chain edges |
| `owl:unionOf` | Class union | Stored in IR |
| `owl:intersectionOf` | Class intersection | Stored in IR |

### Not supported

| Construct | Reason |
|---|---|
| Full cardinality restrictions in superclass position | Beyond OWL2 RL scope |
| `owl:oneOf` (nominals) | Not parsed |
| Full OWL DL tableau reasoning | Intractable; OWL2 RL uses materialization instead |

### Profile validation

The profile validator (`owl/profile.go`) checks for:

- Circular `rdfs:subClassOf` chains
- Conflicting property characteristics (Symmetric + Asymmetric, Reflexive + Irreflexive)
- Implicitly referenced classes (auto-created when referenced in domain/range)

Validation runs automatically during ontology loading. Use `?validate=true` or
`dgraph ontology validate` to check without applying.

---

## Testing

The OWL packages contain 66 test functions across 10 test files, totaling
approximately 2,200 lines of test code.

### Test packages

| Package | Tests | What it covers |
|---|---|---|
| `owl/` | Parser, IR, Reasoner, Profile | Turtle parsing, class expression construction, transitive closure, subsumption, profile validation |
| `owl/compiler/` | Dgraph compiler | Class-to-type mapping, property-to-predicate mapping, XSD type conversion, field inheritance |
| `owl/materializer/` | Engine + TypeMaterializer | Type hierarchy, domain/range, inverse, symmetric, property chains, disjointness, circuit breaker, delete cascades, integration tests |
| `owl/graphql/` | GraphQL generator | Interface/type generation, field types, @hasInverse, implements clauses |
| `owl/introspect/` | Inspector | Class lookup, property collection, hierarchy traversal |

### Running tests

```bash
# All OWL tests (fast, no cluster needed)
./scripts/build.sh test-all

# or directly with go test
go test -v ./owl/...

# Individual package
go test -v ./owl/materializer/

# Integration tests (requires running cluster)
./scripts/build.sh cluster-up
go test -v ./owl/materializer/ -run TestIntegration
```

### Test ontology

All tests use `owl/testdata/animals.ttl`, which defines:

```
Classes:
  Animal
    Mammal
      Dog (disjointWith Cat)
        GoldenRetriever
        Labrador
      Cat
    Bird
      Parrot
  Person
  Place
    City
    Country
  DomesticAnimal (unionOf Dog, Cat)

Object Properties:
  hasOwner     (Functional, domain: Animal, range: Person)
  isOwnerOf    (domain: Person, range: Animal, inverseOf: hasOwner)
  locatedIn    (Transitive, domain: Place, range: Place)
  livesIn      (Functional, domain: Person, range: Place)
  friendOf     (Symmetric, domain: Person, range: Person)

Data Properties:
  name         (Functional, range: xsd:string)
  birthDate    (Functional, domain: Animal, range: xsd:date)
  breed        (Functional, domain: Dog, range: xsd:string)
  weight       (domain: Animal, range: xsd:float)
```

---

## Files Modified in Dgraph Core

OWLGraph modifies only 93 lines across 6 existing Dgraph files. Every other
file in the `owl/` package tree is new code.

| File | Lines Changed | What |
|---|---|---|
| `edgraph/server.go` | ~15 | Materializer hook in `Mutate()`: calls `Engine.Materialize()` after `ToDirectedEdges`, appends inferred edges |
| `dgraph/cmd/alpha/run.go` | 2 | Route registrations for `/ontology` and `/ontology/introspect` |
| `dgraph/cmd/root.go` | 2 | Import + `AddCommand` for the `ontology` CLI subcommand |
| `dql/parser.go` | ~40 | `exactType` constant, `TransitivePath` and `PathDepth` AST fields, `*` token handling after predicate names |
| `dql/state.go` | ~10 | Digit lexing after star token (for `pred*3` syntax) |
| `query/query.go` | ~24 | `TransitivePath` parameter propagation, `exactType` function handling (resolves to `eq` on `dgraph.type`) |

---

## Troubleshooting

### "No ontology loaded" error from /ontology/introspect

The ontology must be loaded before introspection is available. POST your Turtle
file to `/ontology` first. If the cluster was restarted, the ontology should
auto-load from persistence -- check Alpha logs for
`"OWLGraph: Persisted ontology loaded"`.

### Disjointness violation on mutation

```
owl/materializer: disjointness violation on entity 0x1:
  type "Dog" is disjoint with type "Cat"
```

A node cannot be both a Dog and a Cat if `Dog owl:disjointWith Cat` is declared.
Check your mutation to ensure you are not assigning conflicting types.

### Circuit breaker triggered

```
owl/materializer: circuit breaker triggered: 15000 inferred edges exceeds limit of 10000
```

A single mutation generated too many inferred edges. This usually indicates a
very deep or wide type hierarchy. Split the mutation into smaller batches, or
increase the limit:

```go
materializer.MaxInferredEdgesPerMutation = 50000
```

### Schema changes not reflected after ontology reload

Loading a new ontology replaces the reasoning engine but does not drop the
existing schema. If you need a clean slate, drop all data first:

```bash
curl -X POST http://localhost:8080/alter -d '{"drop_all": true}'
```

Then reload the ontology.

### Retroactive materialization not working

Retroactive materialization runs asynchronously after ontology load. Check Alpha
logs for `"OWLGraph: Retroactive materialization complete"`. If it reports
failures, verify the Alpha is accessible at `http://localhost:8080` from within
the container.

### exactType returning unexpected results

`exactType` currently resolves to an `eq` filter on `dgraph.type`, which does
not yet filter by the `owl.inferred` facet. This means `exactType(Dog)` may
return nodes that have `Dog` as an inferred type. Full facet-based filtering is
planned for a future release.
