package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: genopenapi <input.yaml> <output.json>")
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "parse yaml: %v\n", err)
		os.Exit(1)
	}

	normalized := normalize(doc)
	out, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal json: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	if err := os.WriteFile(os.Args[2], out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
}

func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = normalize(v)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[fmt.Sprint(k)] = normalize(v)
		}
		return m
	case []any:
		for i, v := range x {
			x[i] = normalize(v)
		}
		return x
	default:
		return x
	}
}
