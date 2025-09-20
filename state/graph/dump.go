package graph

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Dump converts a graph to a Mermaid string representation for state diagrams.
func Dump[S any, E Event](g *Graph[S, E]) string {
	return mermaidDumper[S, E]{}.Dump(g.InitialNode.State, g.getEdges())
}

// wildState represents a wildcard state in diagrams.
type wildState struct{}

// Name returns the wildcard symbol.
func (w wildState) Name() string {
	return "*"
}

// eventName converts an event to a human-readable name.
// For reflect.Type, it returns the type name; for others, it uses string formatting.
func eventName[E Event](event E) string {
	switch t := any(event).(type) {
	case reflect.Type:
		// Handle pointer types by getting the underlying element type
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return t.Name()
	default:
		return fmt.Sprintf("%v", event)
	}
}

// mermaidDumper implements the dumper interface for Mermaid state diagram format.
type mermaidDumper[S any, E Event] struct{}

// Dump converts a graph to Mermaid state diagram format.
// Returns a string that can be rendered as a Mermaid diagram.
func (d mermaidDumper[S, E]) Dump(init S, edges []Edge[S, E]) string {
	// Start with Mermaid state diagram header and initial state
	headers := []string{"stateDiagram-v2"}
	headers = append(headers, fmt.Sprintf("[*] --> %s", asStringer(init)))

	// Convert all edges to Mermaid format
	var lines []string
	for _, edge := range edges {
		var from fmt.Stringer
		if any(edge.From) == nil {
			// Wildcard transitions use "*" as the source state
			from = namerStringer{Namer: wildState{}}
		} else {
			from = asStringer(edge.From)
		}
		to := asStringer(edge.To)
		lines = append(lines, fmt.Sprintf("%s --> %s: %s", from, to, eventName(edge.Event)))
	}
	// Sort for consistent output
	sort.Strings(lines)

	return strings.Join(append(headers, lines...), "\n  ")
}
