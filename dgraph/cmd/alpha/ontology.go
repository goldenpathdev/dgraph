/*
 * SPDX-FileCopyrightText: © 2026 OWLGraph Contributors
 * SPDX-License-Identifier: Apache-2.0
 */

package alpha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/golang/glog"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/edgraph"
	"github.com/dgraph-io/dgraph/v25/owl"
	"github.com/dgraph-io/dgraph/v25/owl/compiler"
	"github.com/dgraph-io/dgraph/v25/owl/introspect"
	owlmat "github.com/dgraph-io/dgraph/v25/owl/materializer"
	"github.com/dgraph-io/dgraph/v25/x"
)

var (
	globalInspector *introspect.Inspector
	inspectorMu     sync.RWMutex
)

// ontologyHandler handles POST /ontology requests to load an OWL ontology.
//
// The ontology is parsed from Turtle format, compiled to Dgraph schema,
// and the materializer is initialized for write-time type inference.
//
// Request body: Turtle format ontology
// Content-Type: text/turtle or application/x-turtle
//
// Query parameters:
//   - validate=true: only validate, don't apply
//
// Response: JSON with compiled schema and status
func ontologyHandler(w http.ResponseWriter, r *http.Request) {
	if commonHandler(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		x.SetStatus(w, x.ErrorInvalidRequest, "Only POST method is allowed for /ontology")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		x.SetStatus(w, x.ErrorInvalidRequest, "Failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		x.SetStatus(w, x.ErrorInvalidRequest, "Empty request body")
		return
	}

	glog.Infof("Got ontology load request via HTTP from %s (%d bytes)\n", r.RemoteAddr, len(body))

	// Parse the ontology
	parser := owl.NewParser()
	ont, err := parser.ParseTurtle(body)
	if err != nil {
		x.SetStatus(w, x.Error, "Failed to parse ontology: "+err.Error())
		return
	}

	// Build reasoner
	reasoner := owl.NewReasoner(ont)
	if err := reasoner.Build(); err != nil {
		x.SetStatus(w, x.Error, "Failed to build reasoner: "+err.Error())
		return
	}

	// Compile to Dgraph schema
	comp := compiler.NewDgraphCompiler(ont, reasoner)
	result, err := comp.Compile()
	if err != nil {
		x.SetStatus(w, x.Error, "Failed to compile ontology: "+err.Error())
		return
	}

	schema := compiler.CompileSchemaString(result)

	// Check if validate-only mode
	validateOnly := r.URL.Query().Get("validate") == "true"

	response := map[string]interface{}{
		"classes":          len(ont.Classes),
		"objectProperties": len(ont.ObjectProperties),
		"dataProperties":   len(ont.DataProperties),
		"compiledSchema":   schema,
	}

	if validateOnly {
		response["status"] = "valid"
		response["message"] = "Ontology is valid (not applied)"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Apply schema to Dgraph
	ctx := x.AttachAuthToken(context.Background(), r)
	ctx = x.AttachAccessJwt(ctx, r)
	ctx = x.AttachRemoteIP(ctx, r)

	op := &api.Operation{Schema: schema}
	if _, err := (&edgraph.Server{}).Alter(ctx, op); err != nil {
		x.SetStatus(w, x.Error, "Failed to apply schema: "+err.Error())
		return
	}

	// Persist ontology data so it survives restarts
	persistOntology(ctx, body)

	// Initialize the global materializer and reasoning engine
	m := owlmat.NewTypeMaterializer(reasoner)
	owlmat.SetGlobal(m)

	eng := owlmat.NewEngine(ont, reasoner)
	owlmat.SetGlobalEngine(eng)

	// Initialize introspection
	inspectorMu.Lock()
	globalInspector = introspect.NewInspector(ont, reasoner)
	inspectorMu.Unlock()

	glog.Infof("OWLGraph: Ontology loaded — %d classes, %d object properties, %d data properties. Reasoning engine active.\n",
		len(ont.Classes), len(ont.ObjectProperties), len(ont.DataProperties))

	// Trigger retroactive materialization for existing data
	owlmat.RetroactiveMaterializeAsync("http://localhost:8080", reasoner, ont)

	response["status"] = "success"
	response["message"] = "Ontology loaded and materializer activated"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// persistOntology stores the ontology Turtle data in Dgraph so it survives restarts.
func persistOntology(ctx context.Context, turtleData []byte) {
	// Store as a mutation: <_:owlgraph> <owl.ontologyData> "..." .
	mutation := fmt.Sprintf(`{
		"set": [
			{"uid": "_:owlgraph_meta", "dgraph.type": "OWLGraphMeta", "owl.ontologyData": %q}
		]
	}`, string(turtleData))

	// Use the edgraph server to mutate
	req := &api.Request{
		Mutations: []*api.Mutation{{
			SetJson:   []byte(mutation),
			CommitNow: true,
		}},
	}
	if _, err := (&edgraph.Server{}).Query(ctx, req); err != nil {
		glog.Warningf("OWLGraph: Failed to persist ontology: %v", err)
	} else {
		glog.Infof("OWLGraph: Ontology data persisted for restart recovery")
	}
}

// TryLoadPersistedOntology attempts to load a previously persisted ontology
// on Alpha startup. Call this during initialization.
func TryLoadPersistedOntology() {
	// Query for stored ontology data
	ctx := context.Background()
	req := &api.Request{
		Query: `{ q(func: type(OWLGraphMeta)) { owl.ontologyData } }`,
	}
	resp, err := (&edgraph.Server{}).Query(ctx, req)
	if err != nil {
		glog.V(2).Infof("OWLGraph: No persisted ontology found: %v", err)
		return
	}

	// Parse response to extract ontology data
	var result struct {
		Q []struct {
			Data string `json:"owl.ontologyData"`
		} `json:"q"`
	}
	if err := json.Unmarshal(resp.Json, &result); err != nil || len(result.Q) == 0 || result.Q[0].Data == "" {
		glog.V(2).Infof("OWLGraph: No persisted ontology to load")
		return
	}

	turtleData := []byte(result.Q[0].Data)
	glog.Infof("OWLGraph: Found persisted ontology (%d bytes), loading...", len(turtleData))

	parser := owl.NewParser()
	ont, err := parser.ParseTurtle(turtleData)
	if err != nil {
		glog.Warningf("OWLGraph: Failed to parse persisted ontology: %v", err)
		return
	}

	reasoner := owl.NewReasoner(ont)
	if err := reasoner.Build(); err != nil {
		glog.Warningf("OWLGraph: Failed to build reasoner from persisted ontology: %v", err)
		return
	}

	m := owlmat.NewTypeMaterializer(reasoner)
	owlmat.SetGlobal(m)
	eng := owlmat.NewEngine(ont, reasoner)
	owlmat.SetGlobalEngine(eng)

	inspectorMu.Lock()
	globalInspector = introspect.NewInspector(ont, reasoner)
	inspectorMu.Unlock()

	glog.Infof("OWLGraph: Persisted ontology loaded — %d classes, reasoning engine active", len(ont.Classes))
}

// ontologyIntrospectHandler handles GET /ontology/introspect requests.
//
// Query parameters:
//   - class=Name: get info about a specific class
//   - subclasses=Name: get subclasses of a class (add &transitive=true for transitive)
//   - superclasses=Name: get superclasses of a class
//   - properties=Name: get properties applicable to a class
//
// With no parameters, returns all classes.
func ontologyIntrospectHandler(w http.ResponseWriter, r *http.Request) {
	x.AddCorsHeaders(w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		x.SetStatus(w, x.ErrorInvalidMethod, "Only GET and POST allowed")
		return
	}

	inspectorMu.RLock()
	insp := globalInspector
	inspectorMu.RUnlock()

	if insp == nil {
		x.SetStatus(w, x.Error, "No ontology loaded. POST to /ontology first.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()

	if className := q.Get("class"); className != "" {
		info := insp.GetClass(className)
		if info == nil {
			x.SetStatus(w, x.ErrorInvalidRequest, "Class not found: "+className)
			return
		}
		json.NewEncoder(w).Encode(info)
		return
	}

	if className := q.Get("subclasses"); className != "" {
		transitive := q.Get("transitive") == "true"
		subs := insp.SubClassesOf(className, transitive)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"class":      className,
			"transitive": transitive,
			"subClasses": subs,
		})
		return
	}

	if className := q.Get("superclasses"); className != "" {
		transitive := q.Get("transitive") == "true"
		supers := insp.SuperClassesOf(className, transitive)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"class":        className,
			"transitive":   transitive,
			"superClasses": supers,
		})
		return
	}

	if className := q.Get("properties"); className != "" {
		props := insp.PropertiesOf(className)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"class":      className,
			"properties": props,
		})
		return
	}

	// Default: list all classes
	classes := insp.ListClasses()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"classes": classes,
	})
}
