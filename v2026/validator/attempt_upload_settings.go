// Tunables of the validator's attempt object uploads to an operator's api: how
// long a request may sit still, how slow a whole upload may be, how often a
// cut-short upload goes again, how many objects a seal uploads at once, the
// pooled HTTP/1.1 connections, and whether objects travel compressed. They
// answer the 2026-11-16 path loss into the api's site, when every upload had a
// fixed 30 seconds and a 4.19 MB records object at about 100 KB/s needed 42.
package validator

import (
	"time"
)

// One upload owner's copy; tests shorten any of them.
type AttemptUploadSettings struct {
	// The longest a request body may sit still while it is sent, and the base
	// of every request's cap. Thirty seconds was the fixed budget of a whole
	// upload until 2026-11-16; it is now spent only without progress.
	StallTimeout time.Duration
	// How often an upload's progress is looked at.
	WatchInterval time.Duration
	// The slowest whole upload accepted, in bytes a second: a request's cap is
	// StallTimeout plus its size at this rate, 286 s for 4 MiB at 16 KiB/s. A
	// reserved upload also stops at its signed consent.
	FloorBytesPerSecond int
	// The bound of the stored-object check before a repeat.
	ProbeTimeout time.Duration
	// Requests per object for the answers of a path that lost the upload: its
	// own deadline, a transport failure, 408, 425, 429 or 5xx. A reserved
	// refusal keeps its own bound. One sends each object once.
	Attempts int
	// The first wait's ceiling. Each later ceiling doubles up to BackoffCap,
	// and a wait is drawn from the upper half of its ceiling so that
	// validators that lost the same path do not return at once. A Retry-After
	// the answer carried is honored up to BackoffCap.
	BackoffBase time.Duration
	BackoffCap  time.Duration
	// Data objects a seal uploads while it builds its next chunks. The api
	// admits one fresh reserved upload per owner at a time
	// (reserved_attempt_upload.admission.fresh_active_per_owner), so one keeps
	// within it while the seal's record walk hides the upload; raise it only
	// with that limit. Zero uploads each chunk before building the next.
	InFlight int
	// Idle pooled upload connections kept per origin.
	IdleConnsPerHost int
	// An idle upload connection closes before the load balancer's one-minute
	// keepalive would close it under a new request.
	IdleConnTimeout time.Duration
	// Bytes the transport writes at once, more than its 4 KiB default.
	WriteBufferBytes int
	// Sends an object as zstd to an origin that advertised zstd request
	// bodies, when that is smaller. NDJSON records shrink several times over,
	// and a slow path's time is its bytes.
	Compress bool
}

// Production's values, a new copy on every call.
func DefaultAttemptUploadSettings() AttemptUploadSettings {
	return AttemptUploadSettings{
		StallTimeout:        30 * time.Second,
		WatchInterval:       time.Second,
		FloorBytesPerSecond: 16 * 1024,
		ProbeTimeout:        30 * time.Second,
		Attempts:            6,
		BackoffBase:         2 * time.Second,
		BackoffCap:          time.Minute,
		InFlight:            1,
		IdleConnsPerHost:    8,
		IdleConnTimeout:     45 * time.Second,
		WriteBufferBytes:    64 * 1024,
		Compress:            true,
	}
}

// The whole request's cap for a body of size bytes.
func (self AttemptUploadSettings) requestCap(size int) time.Duration {
	return self.StallTimeout + time.Duration(size)*time.Second/time.Duration(max(self.FloorBytesPerSecond, 1))
}
