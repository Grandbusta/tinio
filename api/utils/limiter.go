package utils

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter allows so many requests per key (an email, an address), then one
// every so often. It's kept in this process: with more than one API process,
// each counts its own.
type Limiter struct {
	mu    sync.Mutex
	every rate.Limit
	burst int
	by    map[string]*seen
	swept time.Time
}

type seen struct {
	l    *rate.Limiter
	last time.Time
}

func NewLimiter(every time.Duration, burst int) *Limiter {
	return &Limiter{every: rate.Every(every), burst: burst, by: map[string]*seen{}}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.swept) > 10*time.Minute { // forget keys quiet for an hour
		for k, s := range l.by {
			if now.Sub(s.last) > time.Hour {
				delete(l.by, k)
			}
		}
		l.swept = now
	}
	s, ok := l.by[key]
	if !ok {
		s = &seen{l: rate.NewLimiter(l.every, l.burst)}
		l.by[key] = s
	}
	s.last = now
	return s.l.AllowN(now, 1)
}
