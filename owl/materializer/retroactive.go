package materializer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"github.com/dgraph-io/dgraph/v25/owl"
)

// RetroactiveMaterialize scans existing data in the cluster and materializes
// missing ancestor type edges for all nodes that have types declared in the ontology.
//
// This should be called after loading an ontology into a cluster that already has data.
// It queries all nodes with dgraph.type, checks if ancestor types are missing, and
// adds them via mutations.
//
// alphaAddr is the HTTP address of the Alpha node (e.g., "http://localhost:8080").
func RetroactiveMaterialize(alphaAddr string, reasoner *owl.Reasoner, ont *owl.Ontology) error {
	glog.Infof("OWLGraph: Starting retroactive materialization...")

	// For each class in the ontology that has superclasses, find instances missing ancestors
	var totalMaterialized int

	for classIRI := range ont.Classes {
		ancestors := reasoner.AllSuperClasses(classIRI)
		if len(ancestors) == 0 {
			continue
		}

		// Query nodes of this type
		query := fmt.Sprintf(`{ q(func: type(%s)) { uid dgraph.type } }`, classIRI)
		resp, err := http.Post(alphaAddr+"/query", "application/dql", strings.NewReader(query))
		if err != nil {
			glog.Warningf("OWLGraph: retroactive query failed for %s: %v", classIRI, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			Data struct {
				Q []struct {
					UID   string   `json:"uid"`
					Types []string `json:"dgraph.type"`
				} `json:"q"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			continue
		}

		// For each node, check which ancestor types are missing
		var nquads []string
		for _, node := range result.Data.Q {
			existingTypes := make(map[string]bool)
			for _, t := range node.Types {
				existingTypes[t] = true
			}

			for _, anc := range ancestors {
				if !existingTypes[string(anc)] {
					nquads = append(nquads, fmt.Sprintf(`<%s> <dgraph.type> %q .`, node.UID, string(anc)))
					totalMaterialized++
				}
			}
		}

		// Apply missing types in batches
		if len(nquads) > 0 {
			batchSize := 1000
			for i := 0; i < len(nquads); i += batchSize {
				end := i + batchSize
				if end > len(nquads) {
					end = len(nquads)
				}
				batch := strings.Join(nquads[i:end], "\n")
				mutation := fmt.Sprintf(`{"set": [%s]}`,
					convertNQuadsToJSON(nquads[i:end]))

				resp, err := http.Post(alphaAddr+"/mutate?commitNow=true",
					"application/json", strings.NewReader(mutation))
				if err != nil {
					glog.Warningf("OWLGraph: retroactive mutation failed: %v", err)
					continue
				}
				resp.Body.Close()
				_ = batch
			}
		}
	}

	glog.Infof("OWLGraph: Retroactive materialization complete — %d type edges added", totalMaterialized)
	return nil
}

// convertNQuadsToJSON converts RDF NQuads to JSON mutation format.
func convertNQuadsToJSON(nquads []string) string {
	var entries []string
	for _, nq := range nquads {
		// Parse: <uid> <dgraph.type> "TypeName" .
		parts := strings.Fields(nq)
		if len(parts) < 4 {
			continue
		}
		uid := strings.Trim(parts[0], "<>")
		typeName := strings.Trim(parts[2], `"`)
		entry := fmt.Sprintf(`{"uid": %q, "dgraph.type": %q}`, uid, typeName)
		entries = append(entries, entry)
	}
	return strings.Join(entries, ",")
}

// RetroactiveMaterializeAsync runs retroactive materialization in a background goroutine.
func RetroactiveMaterializeAsync(alphaAddr string, reasoner *owl.Reasoner, ont *owl.Ontology) {
	go func() {
		if err := RetroactiveMaterialize(alphaAddr, reasoner, ont); err != nil {
			glog.Warningf("OWLGraph: retroactive materialization failed: %v", err)
		}
	}()
}

// Suppress unused import
var _ = bytes.NewReader
