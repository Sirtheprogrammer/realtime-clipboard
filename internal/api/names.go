package api

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strings"
)

// Room codes avoid vowels and look-alike characters so they survive being read
// aloud or typed from a phone screen.
const codeAlphabet = "bcdfghjkmnpqrstvwxyz23456789"

var roomCodePattern = regexp.MustCompile(`^[a-z0-9-]{4,64}$`)

func NewRoomCode() string {
	return randomString(codeAlphabet, 4) + "-" + randomString(codeAlphabet, 4)
}

// NormalizeRoomCode lowercases and validates a user-supplied code.
func NormalizeRoomCode(raw string) (string, bool) {
	code := strings.ToLower(strings.TrimSpace(raw))
	code = strings.Trim(code, "-")
	if !roomCodePattern.MatchString(code) {
		return "", false
	}
	return code, true
}

var (
	adjectives = []string{"amber", "brisk", "calm", "clever", "cosmic", "dusty",
		"eager", "fuzzy", "gentle", "hidden", "jolly", "lucky", "mellow",
		"nimble", "quiet", "rapid", "silver", "sunny", "tidy", "wandering"}
	creatures = []string{"otter", "falcon", "cactus", "lantern", "comet",
		"badger", "maple", "pebble", "quokka", "raven", "sparrow", "tulip",
		"walrus", "yak", "zebra", "beacon", "ferret", "heron", "koala", "lynx"}
)

// NewDeviceName labels a connection in the dashboard so you can tell which
// machine sent a paste without asking anyone to sign in.
func NewDeviceName() string {
	return pick(adjectives) + " " + pick(creatures)
}

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

func randomString(alphabet string, n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			v = big.NewInt(0)
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out)
}
