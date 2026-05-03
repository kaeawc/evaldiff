// Package index builds and caches the two artifacts evaldiff is built around:
// the behavior index (normalized prompts, tool schemas, agent edges, model
// IDs, sampling params) and the eval-coverage index (which behaviors each
// eval exercises). Both are content-addressable and cached per commit.
package index
