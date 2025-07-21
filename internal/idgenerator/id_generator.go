package idgenerator

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jxskiss/base62"
)

const (
	instanceIDBits  uint64 = 10
	sequenceBits    uint64 = 12
	maxInstanceID   int64  = -1 ^ (-1 << instanceIDBits)
	maxSequence     int64  = -1 ^ (-1 << sequenceBits)
	timestampShift         = instanceIDBits + sequenceBits
	instanceIDShift        = sequenceBits
)

// customEpoch is the custom epoch start time (2024-01-01 00:00:00 UTC) in milliseconds.
var customEpoch int64 = 1704067200000

// IDGenerator is a distributed unique ID generator inspired by Twitter's Snowflake.
type IDGenerator struct {
	mu            sync.Mutex
	lastTimestamp int64
	instanceID    int64
	sequence      int64
}

// NewIDGenerator creates a new IDGenerator.
// The instanceID must be unique for each running instance of the service.
func NewIDGenerator(instanceID int64) (*IDGenerator, error) {
	if instanceID < 0 || instanceID > maxInstanceID {
		return nil, errors.New("instance ID out of range")
	}
	return &IDGenerator{instanceID: instanceID}, nil
}

// Generate creates and returns a new unique ID.
// The returned ID is a Base62 encoded string, which is URL-safe and compact.
func (g *IDGenerator) Generate() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	currentTimestamp := time.Now().UnixMilli() - customEpoch

	if currentTimestamp < g.lastTimestamp {
		// Clock moved backwards. This is a serious problem.
		// In a real-world high-concurrency system, this might require coordination.
		// For now, we return an error to prevent generating non-monotonic IDs.
		return "", errors.New("clock moved backwards, refusing to generate ID")
	}

	if currentTimestamp == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// Sequence overflow, spin-wait for the next millisecond.
			for currentTimestamp <= g.lastTimestamp {
				currentTimestamp = time.Now().UnixMilli() - customEpoch
			}
		}
	} else {
		// New millisecond, reset sequence.
		g.sequence = 0
	}

	g.lastTimestamp = currentTimestamp

	id := (currentTimestamp << timestampShift) |
		(g.instanceID << instanceIDShift) |
		g.sequence

	// Encode the 64-bit integer to a Base62 string.
	// We convert the int64 to a byte slice for the encoder.
	// Note: A more direct int64-to-base62 would be more efficient, but this is clear and works.
	// A simple string conversion is sufficient here.
	idStr := strconv.FormatInt(id, 10)
	encodedID := base62.EncodeToString([]byte(idStr))

	return encodedID, nil
}
