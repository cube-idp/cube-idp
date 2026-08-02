package config

import "math/rand/v2"

// Wordlists for docker-style scaffold names. Every adjective-noun
// combination must satisfy the api name regex (max 31 chars) — asserted
// exhaustively in tests, so a wordlist edit can never produce an invalid
// cube identity.
var (
	nameAdjectives = []string{
		"agile", "amber", "bold", "brave", "breezy", "bright", "calm",
		"clever", "cosmic", "crisp", "eager", "gentle", "happy", "jolly",
		"keen", "lively", "mellow", "nimble", "quirky", "sunny", "swift",
		"witty",
	}
	nameNouns = []string{
		"badger", "beacon", "comet", "falcon", "fjord", "gecko", "glacier",
		"harbor", "heron", "koala", "lemur", "marmot", "meadow", "nebula",
		"otter", "panda", "pebble", "quartz", "raven", "sparrow", "summit",
		"tundra", "walrus", "wombat",
	}
)

// GenerateName returns a random docker-style cube name
// (<adjective>-<noun>) for scaffolded config documents. The result
// always satisfies the api name regex (asserted exhaustively in tests),
// so callers never need to re-validate it.
func GenerateName() string {
	adj := nameAdjectives[rand.IntN(len(nameAdjectives))]
	noun := nameNouns[rand.IntN(len(nameNouns))]
	return adj + "-" + noun
}
