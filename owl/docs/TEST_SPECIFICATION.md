# OWLGraph — Test Specification

44 defined tests + 4 benchmarks across all implementation phases. Tests are designed to be written **before** implementation (TDD).

---

## Phase 0: OWL Parser & Reasoner

**Files**: `owl/parser_test.go`, `owl/reasoner_test.go`

### P0-T01: Parse Simple Class Hierarchy

```turtle
:Animal a owl:Class .
:Dog a owl:Class ; rdfs:subClassOf :Animal .
:GoldenRetriever a owl:Class ; rdfs:subClassOf :Dog .
```

**Assert**: 3 classes parsed. `Dog.SuperClasses = [Animal]`. `GoldenRetriever.SuperClasses = [Dog]`.

### P0-T02: Parse Properties with Domain/Range

```turtle
:hasOwner a owl:ObjectProperty, owl:FunctionalProperty ;
    rdfs:domain :Animal ; rdfs:range :Person .
:name a owl:DatatypeProperty ; rdfs:range xsd:string .
```

**Assert**: `hasOwner.IsFunctional = true`. `hasOwner.Domain = [Animal]`. `hasOwner.Range = Person`. `name.IsDatatype = true`.

### P0-T03: Parse Inverse Properties

```turtle
:hasOwner owl:inverseOf :isOwnerOf .
```

**Assert**: `hasOwner.InverseOf = isOwnerOf`. `isOwnerOf.InverseOf = hasOwner` (bidirectional).

### P0-T04: Parse Transitive Properties

```turtle
:locatedIn a owl:TransitiveProperty .
```

**Assert**: `locatedIn.IsTransitive = true`.

### P0-T05: Parse Union and Intersection

```turtle
:Pet owl:equivalentClass [
    owl:intersectionOf ( :Animal [
        a owl:Restriction ; owl:onProperty :hasOwner ;
        owl:someValuesFrom :Person
    ] )
] .
:DomesticAnimal owl:unionOf ( :Dog :Cat :Hamster ) .
```

**Assert**: `Pet.EquivalentClass` is intersection expression. `DomesticAnimal.UnionOf = [Dog, Cat, Hamster]`.

### P0-T06: Subsumption Closure

**Given**: Animal > Mammal > Dog > GoldenRetriever, Animal > Bird > Parrot

**Assert**:
- `reasoner.Subsumes(Animal, GoldenRetriever) = true`
- `reasoner.Subsumes(Dog, Parrot) = false`
- `reasoner.AllSubClasses(Animal) = [Mammal, Dog, GoldenRetriever, Bird, Parrot]`

### P0-T07: Transitive Closure Computation

**Given**: Brooklyn locatedIn NYC, NYC locatedIn NewYork, NewYork locatedIn USA

**Assert**: `reasoner.TransitiveClosure(Brooklyn, locatedIn) = [NYC, NewYork, USA]`

### P0-T08: Domain/Range Inference

**Given**: `hasOwner` domain=Animal, range=Person. Triple: `<fido> <hasOwner> <john>`

**Assert**: `reasoner.InferredTypes(fido) = [Animal]`. `reasoner.InferredTypes(john) = [Person]`.

### P0-T09: Reject Unsupported OWL DL Constructs

**Input**: `owl:cardinality` > 1 in superclass position (not in OWL2 RL)

**Assert**: Parser returns error with clear message about unsupported construct.

### P0-T10: Parse Real-World Ontology (Schema.org subset)

**Input**: 50-class subset of schema.org in Turtle

**Assert**: Parses without error. Class count matches. Property count matches. No orphan properties.

---

## Phase 1: Ontology Storage & Schema Compilation

**Files**: `owl/compiler/dgraph_test.go`, `systest/ontology/load_test.go`

### P1-T01: OWL Class → Dgraph Type

**Input**: `owl:Class :Dog` with properties `name: string`, `breed: string`, `hasOwner: uid`

**Assert**: Compiled `pb.TypeUpdate` has `type_name = "Dog"`, fields include `name`, `breed`, `hasOwner`.

### P1-T02: SubClassOf → Type Includes Parent Fields

**Input**: Animal has `name`. Dog subClassOf Animal, has `breed`.

**Assert**: Dog's `pb.TypeUpdate` includes both `name` and `breed`.

### P1-T03: FunctionalProperty → Non-List Predicate

**Input**: `hasOwner` is `owl:FunctionalProperty`

**Assert**: Compiled `pb.SchemaUpdate` has `List = false`.

### P1-T04: Non-Functional Property → List Predicate

**Input**: `hasTag` is `owl:ObjectProperty` (not functional)

**Assert**: Compiled `pb.SchemaUpdate` has `List = true`.

### P1-T05: Ontology Load Round-Trip

Load ontology via `/ontology` endpoint → query `owl.*` predicates via DQL → verify structure matches original.

### P1-T06: Ontology + Existing Schema Coexistence

**Setup**: Pre-existing Dgraph schema with `User` type. Load ontology with `Animal` type.

**Assert**: Both `User` and `Animal` types present. No conflicts.

### P1-T07: Ontology Validation Rejects Bad Input

**Input**: Turtle with syntax errors, undefined property ranges, circular subClassOf.

**Assert**: Appropriate errors returned, no schema changes applied.

### P1-T08: Reserved Predicate Protection

**Input**: Attempt to use `owl.type`, `owl.subClassOf` as user predicates.

**Assert**: Error — reserved namespace.

---

## Phase 2: Subsumption & DQL Extensions

**Files**: `systest/ontology/subsumption_test.go`, `systest/ontology/transitive_test.go`, `dql/parser_path_test.go`

### P2-T01: Type Materialization on Write

```
Ontology: Animal > Dog > GoldenRetriever
Mutation: <_:fido> <dgraph.type> "GoldenRetriever" .
          <_:fido> <name> "Fido" .
```

**Assert**:
- `{ q(func: type(GoldenRetriever)) { name } }` returns Fido
- `{ q(func: type(Dog)) { name } }` returns Fido
- `{ q(func: type(Animal)) { name } }` returns Fido

### P2-T02: exactType Excludes Subtypes

Same data as P2-T01.

**Assert**:
- `{ q(func: exactType(Dog)) { name } }` returns empty
- `{ q(func: exactType(GoldenRetriever)) { name } }` returns Fido

### P2-T03: Inverse Property Materialization

```
Ontology: hasOwner inverseOf isOwnerOf
Mutation: <_:fido> <hasOwner> <_:john> .
```

**Assert**: `{ q(func: uid(john)) { isOwnerOf { name } } }` returns Fido.

### P2-T04: Transitive Path Query

```
Ontology: locatedIn is TransitiveProperty
Data: Brooklyn→NYC→NewYork→USA
Query: { q(func: eq(name, "Brooklyn")) { locatedIn* { name } } }
```

**Assert**: Returns [NYC, NewYork, USA] in order.

### P2-T05: Bounded Transitive Path

Same data as P2-T04.

```
Query: { q(func: eq(name, "Brooklyn")) { locatedIn*2 { name } } }
```

**Assert**: Returns [NYC, NewYork] only (max 2 hops).

### P2-T06: Transitive Path Cycle Detection

```
Data: A→B→C→A (cycle)
Query: { q(func: uid(A)) { link* { name } } }
```

**Assert**: Returns [B, C] — no infinite loop, each node visited once.

### P2-T07: Mixed Type Query with Fragments

```
Ontology: Animal > Dog, Animal > Cat
Data: Fido (Dog), Whiskers (Cat), Rex (Dog)
Query: { q(func: type(Animal)) { name dgraph.type } }
```

**Assert**: Returns all 3. Each has correct dgraph.type values including materialized ancestors.

### P2-T08: Write Amplification Bounds

**Setup**: Ontology with hierarchy depth 10. Insert 1000 nodes at leaf type.

**Assert**: Total `dgraph.type` postings = 1000 * 10 = 10,000. Write latency < 2x of non-OWL insert.

### P2-T09: Retroactive Materialization

1. Insert data: `<fido> dgraph.type "Dog"` (before ontology loaded)
2. Load ontology: Dog subClassOf Animal
3. Trigger retroactive materialization

**Assert**: `type(Animal)` now returns Fido.

### P2-T10: DQL Parser Accepts Path Syntax

```dql
{ q(func: uid(0x1)) { locatedIn* { name } } }
{ q(func: uid(0x1)) { locatedIn*3 { name } } }
```

**Assert**: Parses without error. AST has `TransitivePath=true`, `PathDepth=0` and `PathDepth=3` respectively.

---

## Phase 3: OWL→GraphQL Compiler

**Files**: `owl/graphql/generator_test.go`, `systest/ontology/graphql_test.go`

### P3-T01: SubClassOf → Implements Interface

**Input**: Dog subClassOf Animal

**Assert**: Generated SDL contains `interface Animal { ... }` and `type Dog implements Animal { ... }`.

### P3-T02: Inherited Fields in Implementing Types

**Input**: Animal has `name: String`. Dog subClassOf Animal, has `breed: String`.

**Assert**: `type Dog implements Animal` contains both `name` and `breed`.

### P3-T03: InverseOf → @hasInverse

**Input**: `hasOwner inverseOf isOwnerOf`

**Assert**: Generated SDL has `hasOwner: Person @hasInverse(field: "isOwnerOf")`.

### P3-T04: FunctionalProperty → Singular Field

**Input**: `hasOwner` is FunctionalProperty

**Assert**: Field is `hasOwner: Person` (not `[Person]`).

### P3-T05: Non-Functional → List Field

**Input**: `hasFriend` is ObjectProperty (not functional)

**Assert**: Field is `hasFriend: [Person]`.

### P3-T06: Transitive Property → Path Accessor

**Input**: `locatedIn` is TransitiveProperty

**Assert**: Generated type has both `locatedIn: Place` and `locatedInPath: [Place]`.

### P3-T07: Auto-Generated Queries and Mutations

**Input**: 3-class ontology (Animal, Dog, Person)

**Assert**: Generated SDL has `queryAnimal`, `queryDog`, `queryPerson`, `addDog`, `updateDog`, `deleteDog`, etc.

### P3-T08: Subsumption-Aware GraphQL Query

Load ontology, insert GoldenRetriever via GraphQL `addGoldenRetriever`.

**Assert**: `queryAnimal { name }` returns it. `queryDog { name }` returns it.

### P3-T09: Ontology Introspection via GraphQL

```graphql
query { owlClasses { name subClassOf { name } properties { name range } } }
```

**Assert**: Returns full class hierarchy with properties.

### P3-T10: Multiple Inheritance (Diamond)

**Input**: FlyingFish subClassOf Fish, FlyingFish subClassOf FlyingAnimal

**Assert**: `type FlyingFish implements Fish & FlyingAnimal { ... }` with fields from both.

---

## Phase 4: Write-Time Reasoning

**Files**: `systest/ontology/reasoning_test.go`, `owl/materializer/engine_test.go`

### P4-T01: Domain Inference

**Ontology**: `hasOwner` domain=Animal. **Mutation**: `<_:x> <hasOwner> <_:y>`.

**Assert**: `_:x` gets `dgraph.type = "Animal"` materialized.

### P4-T02: Range Inference

**Ontology**: `hasOwner` range=Person. **Mutation**: `<_:x> <hasOwner> <_:y>`.

**Assert**: `_:y` gets `dgraph.type = "Person"` materialized.

### P4-T03: Symmetric Property

**Ontology**: `friendOf` is SymmetricProperty. **Mutation**: `<a> <friendOf> <b>`.

**Assert**: `<b> <friendOf> <a>` exists.

### P4-T04: Property Chain

**Ontology**: `hasGrandparent` = `hasParent o hasParent` (property chain)
**Data**: `<c> hasParent <b>`, `<b> hasParent <a>`

**Assert**: `<c> hasGrandparent <a>` materialized.

### P4-T05: Inferred Facet Distinguishes Asserted vs Derived

After type materialization:

**Assert**:
- `<fido> dgraph.type "GoldenRetriever"` has no `owl.inferred` facet
- `<fido> dgraph.type "Animal"` has `owl.inferred = true`

### P4-T06: Disjointness Violation

**Ontology**: `Dog disjointWith Cat`.
**Mutation**: `<_:x> dgraph.type "Dog" . <_:x> dgraph.type "Cat" .`

**Assert**: Mutation rejected with disjointness error.

### P4-T07: Materialization Circuit Breaker

Configure max 1000 inferred triples per mutation. Mutation triggers >1000 inferences.

**Assert**: Mutation rejected with clear error, not silently truncated.

### P4-T08: Concurrent Mutations with Inference

10 goroutines each inserting 100 nodes with type materialization simultaneously.

**Assert**: All nodes have correct materialized types. No data corruption. No deadlocks.

### P4-T09: Ontology Update Triggers Rematerialization

1. Load ontology: Dog subClassOf Animal. Insert dogs.
2. Update ontology: add Mammal between Dog and Animal.

**Assert**: After rematerialization, all dogs have `dgraph.type = "Mammal"`.

### P4-T10: Delete Cascades to Inferred Triples

Delete `<fido> dgraph.type "GoldenRetriever"`.

**Assert**: Materialized ancestor types (`Dog`, `Animal`) also removed (only the inferred ones).

---

## Integration / End-to-End Tests

**File**: `systest/ontology/e2e_test.go`

### E2E-T01: Full Lifecycle

1. Start Dgraph cluster
2. Load OWL ontology (Turtle)
3. Verify GraphQL schema auto-generated
4. Insert data via GraphQL mutations
5. Query via GraphQL — subsumption works
6. Query via DQL — transitive paths work
7. Introspect ontology via GraphQL
8. Update ontology — rematerialization works

### E2E-T02: Backward Compatibility

1. Load existing Dgraph dataset (non-OWL)
2. All existing queries return same results
3. Load OWL ontology alongside existing schema
4. Existing data unaffected
5. New OWL-typed data works correctly

### E2E-T03: LUBM Benchmark Subset

Load LUBM ontology (14 classes, 25 properties). Insert 10K triples. Run standard LUBM queries 1-7. Verify correct results against known ground truth.

### E2E-T04: Dgraph Existing Test Suite Passes

All existing tests in `systest/`, `query/`, `graphql/test/`, `schema/` continue to pass with OWL code present but no ontology loaded.

---

## Performance Benchmarks

**File**: `systest/ontology/bench_test.go`

### BENCH-01: Write Throughput with Materialization

Insert 10K nodes with type materialization at hierarchy depths 1, 5, and 10. Measure ops/sec and compare to baseline (no ontology).

### BENCH-02: Subsumption Query Latency

`type(Animal)` with 1K, 10K, 100K instances across a 5-level hierarchy. Measure p50/p99 latency.

### BENCH-03: Transitive Path Performance

`locatedIn*` on chains of length 10, 100, 1000. Measure latency and memory usage.

### BENCH-04: GraphQL Polymorphic Query

`queryAnimal` with type fragments on 10K instances across 5 subtypes. Measure latency.
