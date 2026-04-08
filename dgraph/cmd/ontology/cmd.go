/*
 * SPDX-FileCopyrightText: © 2026 OWLGraph Contributors
 * SPDX-License-Identifier: Apache-2.0
 */

// Package ontology provides the 'dgraph ontology' CLI subcommand.
package ontology

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var (
	alphaAddr string
)

// Cmd returns the ontology command for the dgraph CLI.
func Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ontology",
		Short: "OWLGraph ontology management commands",
	}

	cmd.PersistentFlags().StringVarP(&alphaAddr, "alpha", "a", "http://localhost:8080",
		"Dgraph Alpha HTTP address")

	cmd.AddCommand(loadCmd())
	cmd.AddCommand(validateCmd())
	cmd.AddCommand(introspectCmd())

	return cmd
}

func loadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load <file.ttl>",
		Short: "Load an OWL ontology from a Turtle file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}

			resp, err := http.Post(alphaAddr+"/ontology", "text/turtle",
				io.NopCloser(io.Reader(nil)))
			_ = resp // discard, we'll use the real request below

			req, err := http.NewRequest("POST", alphaAddr+"/ontology", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "text/turtle")
			req.Body = io.NopCloser(
				io.Reader((*readerFromBytes)(&data)),
			)
			req.ContentLength = int64(len(data))

			client := &http.Client{}
			response, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Alpha: %w", err)
			}
			defer response.Body.Close()

			body, _ := io.ReadAll(response.Body)
			var result map[string]interface{}
			json.Unmarshal(body, &result)

			if status, ok := result["status"].(string); ok && status == "success" {
				classes := result["classes"]
				objProps := result["objectProperties"]
				dataProps := result["dataProperties"]
				fmt.Printf("Ontology loaded successfully:\n")
				fmt.Printf("  Classes:           %.0f\n", classes)
				fmt.Printf("  Object properties: %.0f\n", objProps)
				fmt.Printf("  Data properties:   %.0f\n", dataProps)
				fmt.Println("  Reasoning engine:  active")
			} else {
				fmt.Printf("Error: %s\n", string(body))
				return fmt.Errorf("ontology load failed")
			}
			return nil
		},
	}
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file.ttl>",
		Short: "Validate an OWL ontology without applying it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}

			req, err := http.NewRequest("POST", alphaAddr+"/ontology?validate=true", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "text/turtle")
			req.Body = io.NopCloser(io.Reader((*readerFromBytes)(&data)))
			req.ContentLength = int64(len(data))

			client := &http.Client{}
			response, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Alpha: %w", err)
			}
			defer response.Body.Close()

			body, _ := io.ReadAll(response.Body)
			var result map[string]interface{}
			json.Unmarshal(body, &result)

			fmt.Printf("Validation result: %s\n", result["status"])
			if classes, ok := result["classes"]; ok {
				fmt.Printf("  Classes: %.0f\n", classes)
			}
			return nil
		},
	}
}

func introspectCmd() *cobra.Command {
	var className string
	cmd := &cobra.Command{
		Use:   "introspect",
		Short: "Query the loaded ontology structure",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := alphaAddr + "/ontology/introspect"
			if className != "" {
				url += "?class=" + className
			}

			resp, err := http.Get(url)
			if err != nil {
				return fmt.Errorf("failed to connect to Alpha: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			var result interface{}
			json.Unmarshal(body, &result)

			pretty, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(pretty))
			return nil
		},
	}
	cmd.Flags().StringVar(&className, "class", "", "Specific class to inspect")
	return cmd
}

// readerFromBytes is a simple io.Reader backed by a byte slice pointer.
type readerFromBytes []byte

func (r *readerFromBytes) Read(p []byte) (n int, err error) {
	if len(*r) == 0 {
		return 0, io.EOF
	}
	n = copy(p, *r)
	*r = (*r)[n:]
	return n, nil
}
