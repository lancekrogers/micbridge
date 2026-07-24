// Package audio is the platform capture boundary (miniaudio/malgo and later
// measured native fallbacks). Callbacks must never block, allocate, log, or
// fan out to API subscribers.
package audio
