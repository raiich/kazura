package graph_test

import (
	"fmt"
	"reflect"
	"testing"
)

// BenchmarkGraph_FindNext looks up the last of the events leaving the initial
// node, and an event that only a wildcard edge has, so the lookup scans all of
// the node's events.
func BenchmarkGraph_FindNext(b *testing.B) {
	type (
		Hub   struct{}
		Spoke struct{}
	)
	// Array types of distinct lengths are distinct reflect.Types.
	eventOf := func(i int) reflect.Type {
		return reflect.ArrayOf(i, reflect.TypeFor[int]())
	}
	// build returns the graph in which the events 0 to n-1 lead from Hub to
	// Spoke and the wildcard event n leads to Spoke as well.
	build := func(b *testing.B, n int) *Graph {
		edges := []Edge{{From: nil, Event: eventOf(n), To: Spoke{}}}
		for i := range n {
			edges = append(edges, Edge{From: Hub{}, Event: eventOf(i), To: Spoke{}})
		}
		g, err := NewGraph(Hub{}, edges...)
		if err != nil {
			b.Fatal(err)
		}
		return g
	}
	run := func(b *testing.B, g *Graph, event reflect.Type) {
		for b.Loop() {
			if _, found := g.FindNext(g.InitialNode, event); !found {
				b.Fatal("transition not found")
			}
		}
	}

	for _, n := range []int{1, 16} {
		b.Run(fmt.Sprintf("own/events=%d", n), func(b *testing.B) {
			run(b, build(b, n), eventOf(n-1))
		})
	}
	b.Run("wildcard/events=16", func(b *testing.B) {
		run(b, build(b, 16), eventOf(16))
	})
}
