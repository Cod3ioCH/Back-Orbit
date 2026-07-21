package protectionblueprints

import (
	"sort"
	"strings"
)

// Evidence is the secret-free project evidence accepted by the matcher.
type Evidence struct {
	Images       []string
	Technologies []string
}

// Result explains why a template matched and what remains uncertain.
type Result struct {
	TemplateID string `json:"templateId"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Category   string `json:"category"`
	// Score is the share of the topology this template describes that was
	// actually found. Required evidence is a precondition, so an eligible
	// match always carries all of it; the score therefore says how many of
	// the template's optional components are present too. 100 means nothing
	// the template describes is missing.
	Score   int      `json:"score"`
	Matched []string `json:"matched"`
	Missing []string `json:"missing"`
	Plan    Plan     `json:"plan"`
}

// Match returns eligible matches ordered by descending score. A template is
// eligible only when every required service role and technology is evidenced;
// weak optional hints can improve ranking but can never manufacture a match.
func (c Catalog) Match(evidence Evidence) []Result {
	images := repositoryPaths(evidence.Images)
	technologies := normalize(evidence.Technologies)
	results := make([]Result, 0)
	for _, template := range c.Templates {
		result, eligible := matchTemplate(template, images, technologies)
		if eligible {
			results = append(results, result)
		}
	}
	// The most specific match first, then the most complete one.
	//
	// Score alone is the wrong order: a one-service template that happens to
	// fit some infrastructure container scores 100 just as easily as the
	// template describing the actual application, and the UI shows the first
	// result as "the" blueprint. How many components a template explains is
	// what makes it the better description of this project — the same
	// principle the detectors use when an image match outranks a shared data
	// directory.
	sort.Slice(results, func(i, j int) bool {
		if len(results[i].Matched) != len(results[j].Matched) {
			return len(results[i].Matched) > len(results[j].Matched)
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].TemplateID < results[j].TemplateID
	})
	return results
}

func matchTemplate(template Template, images, technologies []string) (Result, bool) {
	result := Result{
		TemplateID: template.Metadata.ID, Name: template.Metadata.Name,
		Version: template.Metadata.Version, Category: template.Metadata.Category,
		Matched: []string{}, Missing: []string{}, Plan: template.Plan,
	}

	// Every required role must be filled by a *different* image: a project
	// with one PostgreSQL container does not satisfy a template that expects
	// two. Solved as an assignment rather than first-come-first-served,
	// because a greedy pass gives up whenever an early role takes the only
	// image a later role could have used — and the order images arrive in is
	// not something the matcher gets to choose.
	assigned, complete := assign(images, template.Match.RequiredImageGroups)
	for index, group := range template.Match.RequiredImageGroups {
		label := "required image role: " + strings.Join(group, " or ")
		if assigned[index] >= 0 {
			result.Matched = append(result.Matched, label)
		} else {
			result.Missing = append(result.Missing, label)
		}
	}
	if !complete {
		return Result{}, false
	}

	for _, technology := range template.Match.RequiredTechnologies {
		if !contains(technologies, technology) {
			return Result{}, false
		}
		result.Matched = append(result.Matched, "required technology: "+technology)
	}

	// Required evidence is a precondition, so it is not what the score
	// measures — every eligible match has all of it. What varies is how much
	// of the rest of the template's topology is present.
	optional, found := 0, 0
	for _, group := range template.Match.OptionalImageGroups {
		optional++
		label := "optional image: " + strings.Join(group, " or ")
		if anyImageMatches(images, group) {
			found++
			result.Matched = append(result.Matched, label)
		} else {
			result.Missing = append(result.Missing, label)
		}
	}
	for _, technology := range template.Match.OptionalTechnologies {
		optional++
		label := "optional technology: " + technology
		if contains(technologies, technology) {
			found++
			result.Matched = append(result.Matched, label)
		} else {
			result.Missing = append(result.Missing, label)
		}
	}

	required := len(template.Match.RequiredImageGroups) + len(template.Match.RequiredTechnologies)
	result.Score = 100
	if total := required + optional; total > 0 {
		result.Score = (required + found) * 100 / total
	}
	return result, true
}

// assign fills each required role with a distinct image, and reports whether
// every role could be filled. Standard augmenting-path bipartite matching: it
// finds an assignment whenever one exists, which a greedy pass does not.
//
// Returns, per role, the index of the image assigned to it, or -1.
func assign(images []string, groups [][]string) ([]int, bool) {
	roleOf := make([]int, len(images)) // which role currently holds each image
	for i := range roleOf {
		roleOf[i] = -1
	}
	assigned := make([]int, len(groups))
	for i := range assigned {
		assigned[i] = -1
	}

	var augment func(role int, visited []bool) bool
	augment = func(role int, visited []bool) bool {
		for index, image := range images {
			if visited[index] || !imageMatches(image, groups[role]) {
				continue
			}
			visited[index] = true
			// Free, or its current holder can be moved somewhere else.
			if roleOf[index] == -1 || augment(roleOf[index], visited) {
				roleOf[index] = role
				assigned[role] = index
				return true
			}
		}
		return false
	}

	complete := true
	for role := range groups {
		if !augment(role, make([]bool, len(images))) {
			complete = false
		}
	}
	return assigned, complete
}

// imageMatches reports whether a repository path satisfies any pattern in a
// group.
func imageMatches(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(path, pattern) {
			return true
		}
	}
	return false
}

func anyImageMatches(paths []string, patterns []string) bool {
	for _, path := range paths {
		if imageMatches(path, patterns) {
			return true
		}
	}
	return false
}

// matchesPattern compares a pattern against a repository path on whole path
// segments, anchored at the end.
//
// Substring matching would be wrong in both directions: "mongo" would claim
// the "mongo-express" container, leaving the admin UI role with nothing and
// the template unmatched, and a registry host that happens to contain a
// product name would match anything published under it. Anchoring at the end
// keeps "postgres" matching "ghcr.io/immich-app/postgres" while refusing
// "mongo-express".
func matchesPattern(path, pattern string) bool {
	pattern = strings.Trim(strings.ToLower(strings.TrimSpace(pattern)), "/")
	if pattern == "" || path == "" {
		return false
	}
	if path == pattern {
		return true
	}
	return strings.HasSuffix(path, "/"+pattern)
}

// repositoryPaths reduces image references to their repository path: no
// registry host, no tag, no digest, lower case.
func repositoryPaths(images []string) []string {
	out := make([]string, 0, len(images))
	for _, image := range images {
		if path := repositoryPath(image); path != "" {
			out = append(out, path)
		}
	}
	return out
}

// repositoryPath turns "ghcr.io/immich-app/immich-server:v2.7.5" into
// "immich-app/immich-server", and "docker.io/library/postgres" into
// "library/postgres".
func repositoryPath(image string) string {
	image = strings.ToLower(strings.TrimSpace(image))
	if image == "" {
		return ""
	}
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	// A colon after the last slash is a tag; before it, a registry port.
	if slash := strings.LastIndex(image, "/"); slash >= 0 {
		if colon := strings.Index(image[slash:], ":"); colon >= 0 {
			image = image[:slash+colon]
		}
	} else if colon := strings.Index(image, ":"); colon >= 0 {
		image = image[:colon]
	}

	// Docker's own rule for telling a registry host from a namespace: the
	// first component has a dot or a port, or is localhost.
	if slash := strings.Index(image, "/"); slash > 0 {
		head := image[:slash]
		if strings.ContainsAny(head, ".:") || head == "localhost" {
			image = image[slash+1:]
		}
	}
	return strings.Trim(image, "/")
}

func normalize(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func contains(values []string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
