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
	TemplateID string   `json:"templateId"`
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Category   string   `json:"category"`
	Score      int      `json:"score"`
	Matched    []string `json:"matched"`
	Missing    []string `json:"missing"`
	Plan       Plan     `json:"plan"`
}

// Match returns eligible matches ordered by descending score. A template is
// eligible only when every required service role and technology is evidenced;
// weak optional hints can improve ranking but can never manufacture a match.
func (c Catalog) Match(evidence Evidence) []Result {
	images := normalize(evidence.Images)
	technologies := normalize(evidence.Technologies)
	results := make([]Result, 0)
	for _, template := range c.Templates {
		result, eligible := matchTemplate(template, images, technologies)
		if eligible {
			results = append(results, result)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].TemplateID < results[j].TemplateID
		}
		return results[i].Score > results[j].Score
	})
	return results
}

func matchTemplate(template Template, images, technologies []string) (Result, bool) {
	result := Result{TemplateID: template.Metadata.ID, Name: template.Metadata.Name, Version: template.Metadata.Version, Category: template.Metadata.Category, Matched: []string{}, Missing: []string{}, Plan: template.Plan}
	required, optional, earned := 0, 0, 0
	usedImages := make(map[int]bool)
	for _, group := range template.Match.RequiredImageGroups {
		required++
		if index, ok := distinctPatternMatch(images, group, usedImages); ok {
			usedImages[index] = true
			earned++
			result.Matched = append(result.Matched, "required image role: "+strings.Join(group, " or "))
		} else {
			result.Missing = append(result.Missing, "required image role: "+strings.Join(group, " or "))
		}
	}
	for _, technology := range template.Match.RequiredTechnologies {
		required++
		if contains(technologies, technology) {
			earned++
			result.Matched = append(result.Matched, "required technology: "+technology)
		} else {
			result.Missing = append(result.Missing, "required technology: "+technology)
		}
	}
	if earned != required {
		return Result{}, false
	}
	for _, pattern := range template.Match.OptionalImages {
		optional++
		if patternMatch(images, []string{pattern}) {
			earned++
			result.Matched = append(result.Matched, "optional image: "+pattern)
		} else {
			result.Missing = append(result.Missing, "optional image: "+pattern)
		}
	}
	for _, technology := range template.Match.OptionalTechnologies {
		optional++
		if contains(technologies, technology) {
			earned++
			result.Matched = append(result.Matched, "optional technology: "+technology)
		} else {
			result.Missing = append(result.Missing, "optional technology: "+technology)
		}
	}
	denominator := required + optional
	result.Score = 100
	if denominator > 0 {
		result.Score = earned * 100 / denominator
	}
	return result, true
}

func distinctPatternMatch(values, patterns []string, used map[int]bool) (int, bool) {
	for index, value := range values {
		if used[index] {
			continue
		}
		for _, pattern := range patterns {
			if strings.Contains(value, strings.ToLower(pattern)) {
				return index, true
			}
		}
	}
	return 0, false
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

func patternMatch(values, patterns []string) bool {
	for _, value := range values {
		for _, pattern := range patterns {
			if strings.Contains(value, strings.ToLower(pattern)) {
				return true
			}
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	wanted = strings.ToLower(wanted)
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
