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

// mermaidDumper renders a graph as a Mermaid state diagram.
type mermaidDumper[S any, E Event] struct{}

func (d mermaidDumper[S, E]) Dump(init S, edges []Edge[S, E]) string {
	headers := []string{"stateDiagram-v2"}
	headers = append(headers, fmt.Sprintf("[*] --> %s", asStringer(init)))

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
