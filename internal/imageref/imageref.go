// Package imageref compares container image references against the patterns
// Back-Orbit uses to recognise software.
//
// Both the protection template catalog and the project analyzer answer the
// same question — "is this image that piece of software?" — and both got it
// wrong in the same way when they answered it with a substring search. An
// image reference carries a registry host, a namespace and a tag around the
// name, so a substring can hit any of them: "mongo" claims mongo-express,
// "postgres" claims postgres-exporter, and a registry host containing a
// product name claims everything published under it.
//
// The rule here is to strip the reference down to its repository path and
// then compare whole path segments, anchored at the end. That keeps
// "postgres" matching "ghcr.io/immich-app/postgres" while refusing
// "mongo-express" — a neighbouring tool is a different name, not a longer one.
package imageref

import "strings"

// Matches reports whether a repository path is the software a pattern names.
//
// The path must be one produced by RepositoryPath; the pattern is a
// repository path too, possibly a partial one ("postgres" matches any
// namespace's postgres, "valkey/valkey" only that one). Matching is on whole
// segments anchored at the end, so a pattern never matches a longer name that
// merely starts or ends with it.
func Matches(path, pattern string) bool {
	pattern = strings.Trim(strings.ToLower(strings.TrimSpace(pattern)), "/")
	if pattern == "" || path == "" {
		return false
	}
	if path == pattern {
		return true
	}
	return strings.HasSuffix(path, "/"+pattern)
}

// MatchesAny reports whether a repository path matches any of the patterns.
func MatchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if Matches(path, pattern) {
			return true
		}
	}
	return false
}

// RepositoryPath turns "ghcr.io/immich-app/immich-server:v2.7.5" into
// "immich-app/immich-server", and "docker.io/library/postgres" into
// "library/postgres": no registry host, no tag, no digest, lower case.
func RepositoryPath(image string) string {
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

// RepositoryPaths reduces a list of image references, dropping empty ones.
func RepositoryPaths(images []string) []string {
	out := make([]string, 0, len(images))
	for _, image := range images {
		if path := RepositoryPath(image); path != "" {
			out = append(out, path)
		}
	}
	return out
}

// LastSegment returns the part of a pattern after the final slash, which is
// where a tag or digest would show up if one were mistakenly written into it.
func LastSegment(pattern string) string {
	if slash := strings.LastIndex(pattern, "/"); slash >= 0 {
		return pattern[slash+1:]
	}
	return pattern
}
