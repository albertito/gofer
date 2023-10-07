// Package ipratelimit implements a per-IP rate limiter.
//
// It implements a Limiter, which is configured with a maximum number of
// requests per given time period to allow per IP address.
//
// The Limiter has a fixed maximum size, to limit memory usage. If the maximum
// is reached, older entries are evicted.
//
// It is safe for concurrent use.
//
// The main use-case is to help prevent abuse, not to perform accurate request
// accounting/throttling. The implementation choices reflect this.
//
// For IPv4 addresses, we use the full address as the limiting key.
//
// For IPv6 addresses, since end users are usually assigned a range of
// /64, /56 or /48, we use the following heuristic: There are 3 rate limiters,
// one for each of the common subnet masks (/48, /56, /64). They operate in
// parallel, and any can deny access.
// By default, the rate for /64 is the one given, the rate for /56 is 4x, and
// the rate for /48 is 8x; these rates can be individually configured if
// needed.
//
// Note that rate-limiting 0.0.0.0 is not supported. It will be automatically
// treated as 0.0.0.1. The same applies to IPv6.
package ipratelimit // blitiri.com.ar/go/gofer/ipratelimit

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"
)

// For IPv4, we use the IP addresses just as they are, nothing fancy.
//
// For IPv6, the main challenge is that the key space is too large, and that
// users get assigned vast ranges (/48, /56, or /64). If we pick too narrow,
// we allow DoS bypass. If it's too wide, we would over-block.
// We could do fancy heuristics for coalescing entries, but it gets
// computationally expensive.
// So we use use an intermediate solution: we keep 3 rate limiters, one for
// each of the common subnet masks. They have different limits to decrease the
// chances of over-blocking. This is probably okay for coarse abuse
// prevention, but not good for precise rate limiting.

// Useful articles and references on IP/HTTP rate limiting and IPv6
// assignment, for convenience:
//   - https://adam-p.ca/blog/2022/02/ipv6-rate-limiting/
//   - https://caddyserver.com/docs/json/apps/http/servers/routes/handle/rate_limit/
//   - https://datatracker.ietf.org/doc/html/draft-ietf-httpapi-ratelimit-headers
//   - https://dev.to/satrobit/rate-limiting-in-ipv6-era-using-probabilistic-data-structures-15on
//   - https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After
//   - https://konghq.com/blog/engineering/how-to-design-a-scalable-rate-limiting-algorithm
//   - https://serverfault.com/questions/863501/blocking-or-rate-limiting-ipv6-what-size-prefix
//   - https://www.nginx.com/blog/rate-limiting-nginx/
//   - https://www.rfc-editor.org/rfc/rfc9110#field.retry-after
//   - https://www.ripe.net/publications/docs/ripe-690

// Possible future changes:
//   - AllowWithInfo returning (bool, uint64, time.Duration) to indicate how
//     many more requests are left within this time window, and when the next
//     window starts.
//   - AllowOrSleep that sleeps until a request is allowed.
//   - AllowN that allows N requests at once (can be used for weighting
//     some requests differently).

// Convert an IPv4 to our uint64 representation.
// ip4 MUST be an ipv4 address. It is not checked for performance reasons
// (this improves ipv4 performance by 1.5% to 3.5%).
func ipv4ToUint64(ip4 net.IP) uint64 {
	return uint64(binary.BigEndian.Uint32(ip4))
}

// Convert an IPv6 to a set of uint64 representations, one for /48, /56, and
// /64, as described above.
func ipv6ExtractMasks(ip net.IP) (ip48, ip56, ip64 uint64) {
	ip64 = binary.BigEndian.Uint64(ip[0:8])
	ip56 = ip64 & 0xffff_ffff_ffff_ff00
	ip48 = ip64 & 0xffff_ffff_ffff_0000
	return
}

type entry struct {
	// Timestamp of the last request allowed.
	// We could use time.Time, but this uses less memory (8 bytes vs 24), and
	// improves performance by ~7-9% on all the "DifferentIPv*" benchmarks,
	// with no negative impact on the rest.
	lastAllowed miniTime

	// Requests left (since lastAllowed).
	requestsLeft uint64

	// Prev and next keys in the LRU list.
	// We use this as a doubly-linked list, to implement an LRU cache and keep
	// the size of the entries map bounded.
	// This could be implemented separately, but keeping it in line helps with
	// performance and memory usage.
	lruPrev, lruNext uint64
}

func (e *entry) reset() {
	e.lastAllowed = 0
	e.requestsLeft = 0
	e.lruPrev = 0
	e.lruNext = 0
}

type limiter struct {
	// Allow this many requests per period.
	Requests uint64

	// Allow requests at most once per this duration.
	Period time.Duration

	// Maximum number of entries to keep track of. This is important to keep
	// the memory usage bounded.
	Size int

	// Pool of free entries, to reduce/avoid allocations.
	// This results in an 15-35% reduction in operation latency on this
	// package's benchmarks.
	entryPool sync.Pool

	// Protects the mutable fields below.
	//
	// This is a single lock for the whole limiter, and the data layout and
	// implementation take significant advantage of this (e.g. the embedded
	// LRU list on each entry).
	//
	// This results in very fast operations when contention is low to
	// moderate, which is what we're optimizing for.
	//
	// Even on parallel benchmarks that have a fair amount of contention, this
	// does alright compared to other libraries that optimize for that.
	// However, it does not scale as well to very high contention scenarios
	// (e.g. parallel benchmarks on >32 cores).
	//
	// To improve high contention performance, we could do fine grained
	// locking in a variety of ways (including sharding the limiters at a high
	// level), although that often causes large performance regressions or
	// when contention is low to moderate, which is our main use case.
	mu sync.Mutex

	// Map of key (IP in uint64 form) to limiter entry.
	m map[uint64]*entry

	// LRU doubly-linked list first and last entry.
	// 0 means "not present", which happens when the list is empty. This works
	// only because we never expect to have 0 (address 0.0.0.0 / 0::0) as a
	// valid key.
	lruFirst, lruLast uint64
}

func newlimiter(req uint64, period time.Duration, size int) *limiter {
	l := &limiter{
		Requests: req,
		Period:   period,
		Size:     size,

		m: make(map[uint64]*entry, size),
	}

	l.entryPool.New = func() any { return &entry{} }
	return l
}

// lruBump moves the key to the top of the LRU list.
func (l *limiter) lruBump(key uint64, e *entry) {
	if l.lruFirst == key {
		return
	}

	// Update the last pointer (if this key is the last one).
	if l.lruLast == key {
		l.lruLast = e.lruPrev
	}

	// Take the key out of the list chain.
	if e.lruPrev != 0 {
		l.m[e.lruPrev].lruNext = e.lruNext
	}
	if e.lruNext != 0 {
		l.m[e.lruNext].lruPrev = e.lruPrev
	}

	// Update the current first element.
	if l.lruFirst != 0 {
		l.m[l.lruFirst].lruPrev = key
	}

	// Adjust the key's entry pointers to be at the beginning.
	e.lruNext = l.lruFirst
	e.lruPrev = 0

	// Set this key as the new first element.
	l.lruFirst = key
}

// lruPrepend adds an element to the top of the list. If the list is full, the
// last element is removed and its entry is returned.
func (l *limiter) lruPrepend(key uint64, e *entry) {
	if l.lruFirst == 0 {
		l.lruFirst = key
		l.lruLast = key
		return
	}

	// Add the new element to the beginning of the list.
	e.lruNext = l.lruFirst
	l.m[l.lruFirst].lruPrev = key
	l.lruFirst = key

	// If we're over capacity, remove the last element.
	if len(l.m) > l.Size {
		lastK := l.lruLast
		lastE := l.m[l.lruLast]
		l.lruLast = lastE.lruPrev
		l.m[l.lruLast].lruNext = 0
		delete(l.m, lastK)
		l.entryPool.Put(lastE)
	}
}

// For testing.
var timeNow = time.Now

func (l *limiter) allow(key uint64) bool {
	now := timeNow()

	if key == 0 {
		// We use 0 as the "null" key, because 0.0.0.0 or ::0 IP addresses are
		// not expected to be rate-limit targets. For IPv4 this is usually an
		// incorrect test and it is harmless. For IPv6 this happens
		// mainly on localhost requests, because ::1 will get masked to ::0
		// after masking. This is a bit unfortunate, but it's not a big deal.
		// We force the key to be 1 in those cases.
		key = 1
	}

	if l.Requests == 0 {
		// Always limiting, no need to compute anything.
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.m[key]
	if !ok {
		// It's a new entry.
		e = l.entryPool.Get().(*entry)
		e.reset()

		l.m[key] = e
		l.lruPrepend(key, e)
	} else {
		// Pre-existing entry, just update the LRU.
		l.lruBump(key, e)
	}

	// Decide if we should allow the request.
	if sinceMiniTime(now, e.lastAllowed) >= l.Period {
		e.lastAllowed = makeMiniTime(now)
		e.requestsLeft = l.Requests - 1
		return true
	} else if e.requestsLeft > 0 {
		e.requestsLeft--
		return true
	}

	return false
}

// Limiter is a rate limiter that keeps track of requests per IP address.
type Limiter struct {
	// Individual limiters per IP type.
	ipv4, ip48, ip56, ip64 *limiter
}

// NewLimiter creates a new Limiter.  Per IP address, up to `requests` per
// `period` will be allowed.  Once they're exhausted, requests will be denied
// until `period` has passed from the first approved request. Size is the
// maximum number of IP addresses to keep track of; when that size is reached,
// older entries are removed.  See the package documentation for more details
// on how IPv6 addresses are handled.
func NewLimiter(requests uint64, period time.Duration, size int) *Limiter {
	return &Limiter{
		ipv4: newlimiter(requests, period, size),
		ip64: newlimiter(requests, period, size),
		ip56: newlimiter(requests, period/4, size),
		ip48: newlimiter(requests, period/8, size),
	}
}

// SetIPv6s64Rate sets the rate limit for IPv6 addresses with /64 prefixes.
// It can only be changed before any requests are made.
func (l *Limiter) SetIPv6s64Rate(req uint64, per time.Duration) {
	l.ip64.Requests = req
	l.ip64.Period = per
}

// SetIPv6s56Rate sets the rate limit for IPv6 addresses with /56 prefixes.
// It can only be changed before any requests are made.
func (l *Limiter) SetIPv6s56Rate(req uint64, per time.Duration) {
	l.ip56.Requests = req
	l.ip56.Period = per
}

// SetIPv6s48Rate sets the rate limit for IPv6 addresses with /48 prefixes.
// It can only be changed before any requests are made.
func (l *Limiter) SetIPv6s48Rate(req uint64, per time.Duration) {
	l.ip48.Requests = req
	l.ip48.Period = per
}

// Allow checks if the given IP address is allowed to make a request.
func (l *Limiter) Allow(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// Convert the IPv4 address to a 64-bit integer, and use that as key.
		return l.ipv4.allow(ipv4ToUint64(ip4))
	}

	// Check if the three masks for ipv6. All must be allowed for the request
	// to be allowed.
	a48, a56, a64 := l.allowV6(ip)
	return a48 && a56 && a64
}

func (l *Limiter) allowV6(ip net.IP) (a48, a56, a64 bool) {
	ip48, ip56, ip64 := ipv6ExtractMasks(ip)
	return l.ip48.allow(ip48), l.ip56.allow(ip56), l.ip64.allow(ip64)
}

// DebugString returns a string with debugging information about the limiter.
// This is useful for debugging, but not for production use. It is not
// guaranteed to be stable.
func (l *Limiter) DebugString() string {
	s := "## IPv4\n\n"
	s += l.ipv4.debugString(kToIPv4)
	s += "\n\n"
	s += "## IPv6\n\n"
	s += "### /48\n\n"
	s += l.ip48.debugString(kToIPv6)
	s += "\n\n"
	s += "### /56\n\n"
	s += l.ip56.debugString(kToIPv6)
	s += "\n\n"
	s += "### /64\n\n"
	s += l.ip64.debugString(kToIPv6)
	s += "\n"
	return s
}

func (l *limiter) debugString(kToIP func(uint64) net.IP) string {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := ""
	s += fmt.Sprintf("Allow: %d / %v\n", l.Requests, l.Period)
	s += fmt.Sprintf("Size: %d / %d\n", len(l.m), l.Size)
	s += "\n"
	k := l.lruFirst
	for k != 0 {
		e := l.m[k]
		ip := kToIP(k)
		last := sinceMiniTime(time.Now(), e.lastAllowed).Round(
			time.Millisecond)
		s += fmt.Sprintf("%-22s %3d requests left, last allowed %10s ago\n",
			ip, e.requestsLeft, last)
		k = e.lruNext
	}
	return s
}

// DebugHTML returns a string with debugging information about the limiter, in
// HTML format (just content starting with `<h2>`, no meta-tags). This is
// useful for debugging, but not for production use. It is not guaranteed to
// be stable.
func (l *Limiter) DebugHTML() string {
	s := "<h2>IPv4</h2>"
	s += l.ipv4.debugHTML(kToIPv4)
	s += "<h2>IPv6</h2>"
	s += "<h3>/48</h3>"
	s += l.ip48.debugHTML(kToIPv6)
	s += "<h3>/56</h3>"
	s += l.ip56.debugHTML(kToIPv6)
	s += "<h3>/64</h3>"
	s += l.ip64.debugHTML(kToIPv6)
	return s
}

func (l *limiter) debugHTML(kToIP func(uint64) net.IP) string {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := fmt.Sprintf("Allow: %d / %v<br>\n", l.Requests, l.Period)
	s += fmt.Sprintf("Size: %d / %d<br>\n", len(l.m), l.Size)
	s += "<p>\n"
	if l.lruFirst == 0 {
		s += "(empty)<br>"
		return s
	}

	s += "<table>\n"
	s += "<tr><th>IP</th><th>Requests left</th><th>Last allowed</th></tr>\n"
	k := l.lruFirst
	for k != 0 {
		e := l.m[k]
		ip := kToIP(k)
		last := sinceMiniTime(time.Now(), e.lastAllowed).Round(
			time.Millisecond)
		s += fmt.Sprintf(`<tr><td class="ip">%v</td>`, ip)
		s += fmt.Sprintf(`<td class="requests">%d</td>`, e.requestsLeft)
		s += fmt.Sprintf(`<td class="last">%s</td></tr>`, last)
		s += "\n"
		k = e.lruNext
	}
	s += "</table>\n"
	return s
}

func kToIPv4(k uint64) net.IP {
	return net.IPv4(byte(k>>24), byte(k>>16), byte(k>>8), byte(k))
}

func kToIPv6(k uint64) net.IP {
	buf := make([]byte, 16)
	b := big.NewInt(0).SetUint64(k)
	b = b.Lsh(b, 64)
	return net.IP(b.FillBytes(buf[:]))
}

// miniTime is a small representation of time, as the number of nanoseconds
// elapsed since January 1, 1970 UTC.
// This is used to reduce memory footprint and improve performance.
type miniTime int64

func makeMiniTime(t time.Time) miniTime {
	return miniTime(t.UnixNano())
}

func sinceMiniTime(now time.Time, old miniTime) time.Duration {
	// time.Duration is an int64 nanosecond count, so we can just subtract.
	return time.Duration(now.UnixNano() - int64(old))
}
