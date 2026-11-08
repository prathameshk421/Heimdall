package codegraph

import (
	"cmp"
	"slices"
	"sort"
)

const (
	maxDefiners   = 8
	minNameChars  = 3
	longNameChars = 8
	damping       = 0.85
	rounds        = 30
	topFiles      = 20
)

// buildGraph links each file to the files that define the names it uses. A name defined in many files links nothing.
func (i *Index) buildGraph() {
	i.fwd = map[string]map[string]float64{}
	i.rev = map[string]map[string]float64{}
	users := map[string]map[string]bool{} // name -> files that use it
	for name, refs := range i.refs {
		if len(name) < minNameChars {
			continue
		}
		set := map[string]bool{}
		for _, r := range refs {
			set[r.File] = true
		}
		users[name] = set
	}
	for name, set := range users {
		definers := distinctFiles(i.defs[name])
		if len(definers) == 0 || len(definers) > maxDefiners {
			continue
		}
		weight := 1 / float64(len(definers))
		if len(name) >= longNameChars {
			weight *= 2
		}
		for user := range set {
			for _, def := range definers {
				if def != user && i.canSee(user, def) {
					addEdge(i.fwd, user, def, weight)
					addEdge(i.rev, def, user, weight)
				}
			}
		}
	}
}

func distinctFiles(defs []Def) []string {
	var out []string
	for _, d := range defs {
		if !slices.Contains(out, d.File) {
			out = append(out, d.File)
		}
	}
	return out
}

func addEdge(g map[string]map[string]float64, from, to string, w float64) {
	if g[from] == nil {
		g[from] = map[string]float64{}
	}
	g[from][to] += w
}

// rank runs personalized PageRank that restarts on the changed files. It walks reversed edges to find the files
// that depend on the change, or forward edges to find the files the change uses.
func (i *Index) rank(changed []string, reversed bool) []RankedFile {
	adj := i.fwd
	if reversed {
		adj = i.rev
	}
	restart := map[string]float64{}
	for _, f := range changed {
		if _, ok := i.files[f]; ok {
			restart[f] = 1
		}
	}
	if len(restart) == 0 {
		return nil
	}
	for f := range restart {
		restart[f] = 1 / float64(len(restart))
	}
	score := maps(restart)
	for range rounds {
		next := map[string]float64{}
		for f, p := range restart {
			next[f] = (1 - damping) * p
		}
		for from, s := range score {
			spread(next, adj[from], s*damping, restart)
		}
		score = next
	}
	return topScores(score, restart)
}

// spread sends mass along the edges of one node. A node without edges sends it back to the restart set.
func spread(next map[string]float64, edges map[string]float64, mass float64, restart map[string]float64) {
	total := 0.0
	for _, w := range edges {
		total += w
	}
	if total == 0 {
		for f, p := range restart {
			next[f] += mass * p
		}
		return
	}
	for to, w := range edges {
		next[to] += mass * w / total
	}
}

func maps(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func topScores(score, exclude map[string]float64) []RankedFile {
	var out []RankedFile
	for f, s := range score {
		if _, skip := exclude[f]; !skip && s > 0 {
			out = append(out, RankedFile{File: f, Score: s})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if c := cmp.Compare(out[b].Score, out[a].Score); c != 0 {
			return c < 0
		}
		return out[a].File < out[b].File
	})
	if len(out) > topFiles {
		out = out[:topFiles]
	}
	return out
}
