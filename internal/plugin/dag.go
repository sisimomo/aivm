package plugin

import (
	"fmt"
	"sort"
)

func topologicalSort(graph map[string][]string) ([]string, error) {
	inDegree := make(map[string]int, len(graph))
	adjacency := make(map[string][]string)

	for node := range graph {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
	}
	for node, deps := range graph {
		for _, dep := range deps {
			adjacency[dep] = append(adjacency[dep], node)
			inDegree[node]++
		}
	}

	queue := make([]string, 0)
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}
	sort.Strings(queue)

	sorted := make([]string, 0, len(graph))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)
		dependents := adjacency[node]
		sort.Strings(dependents)
		for _, next := range dependents {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) != len(graph) {
		return nil, fmt.Errorf("cycle detected in plugin dependency graph")
	}
	return sorted, nil
}
