package ipratelimit

import (
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestIPv4ToUint32(t *testing.T) {
	cases := []struct {
		ip       net.IP
		expected uint64
	}{
		{net.IPv4(0, 0, 0, 0), 0},
		{net.IPv4(1, 2, 3, 4), 0x01020304},
	}
	for _, c := range cases {
		v := ipv4ToUint64(c.ip.To4())
		if v != c.expected {
			t.Errorf("IPv4ToUint32(%v) -> %v, expected %v",
				c.ip, v, c.expected)
		}
	}
}

func TestIPv6ExtractMasks(t *testing.T) {
	cases := []struct {
		ip                  string
		eip48, eip56, eip64 uint64
	}{
		{
			"0::1", 0, 0, 0,
		},
		{
			"1111:2222:3333:4444:5555:6666:7777:8888",
			0x1111222233330000,
			0x1111222233334400,
			0x1111222233334444,
		},
	}

	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		ip48, ip56, ip64 := ipv6ExtractMasks(ip)
		if ip48 != c.eip48 || ip56 != c.eip56 || ip64 != c.eip64 {
			t.Errorf("IP %q (%v)", c.ip, ip)
			t.Errorf("   expected (%.16x, %.16x, %.16x)",
				c.eip48, c.eip56, c.eip64)
			t.Errorf("        got (%.16x, %.16x, %.16x)",
				ip48, ip56, ip64)
		}
	}
}

func as(yes, no int) []bool {
	r := []bool{}
	for i := 0; i < yes; i++ {
		r = append(r, true)
	}
	for i := 0; i < no; i++ {
		r = append(r, false)
	}
	return r
}

func TestBasic(t *testing.T) {
	cases := []struct {
		reqs    uint64
		period  time.Duration
		pkts    uint64
		allowed []bool
	}{
		{0, time.Second, 3, as(0, 3)},
		{1, time.Second, 1, as(1, 0)},
		{1, time.Second, 2, as(1, 1)},
		{2, time.Second, 2, as(2, 0)},
		{2, time.Second, 3, as(2, 1)},
		{10, time.Second, 20, as(10, 10)},
	}
	for _, c := range cases {
		l := NewLimiter(c.reqs, c.period, 256)
		ip := net.IPv4(1, 2, 3, 4)
		as := []bool{}
		for i := uint64(0); i < c.pkts; i++ {
			as = append(as, l.Allow(ip))
		}
		if diff := cmp.Diff(c.allowed, as); diff != "" {
			t.Errorf(
				"[rate=%d/%v, pkts=%d, allowed=%v]"+
					" mismatch (-want +got):\n%s",
				c.reqs, c.period, c.pkts, c.allowed, diff)
		}
	}
}

func TestBasicIPv6(t *testing.T) {
	operations := []struct {
		ip      string
		allowed bool
	}{
		{"1111:2222:3333:4444::a", true},
		{"1111:2222:3333:4444::b", false},
		{"1111:2222:3333:5555::c", false},
		{"1111:2222:3333:5500::d", false},
		{"1111:2222:3333::e", false},
	}

	l := NewLimiter(1, time.Second, 256)
	for i, op := range operations {
		ip := net.ParseIP(op.ip)
		allowed := l.Allow(ip)
		if allowed != op.allowed {
			t.Errorf("operation %d: Allow(%v) -> %v, expected %v",
				i, ip, allowed, op.allowed)
		}
	}
}

func TestIPv6Subnetting(t *testing.T) {
	// These two are equal in the first 64 bits, and differ at the end.
	// So they should be counted as the same at all levels.
	ip64a := net.ParseIP("1111:1111:1111:1111:aaaa::a")
	ip64b := net.ParseIP("1111:1111:1111:1111:bbbb::b")

	// These two are equal in the first 56 bits, and differ at the end.
	// So they should be counted as the same for /48 and /56, but not /64.
	ip56a := net.ParseIP("2222:2222:2222:22aa::a")
	ip56b := net.ParseIP("2222:2222:2222:22bb::b")

	// These two are equal in the first 48 bits, and differ at the end.
	// So they should be counted as the same for /48, but not /56 or /64.
	ip48a := net.ParseIP("3333:3333:3333:aaaa::a")
	ip48b := net.ParseIP("3333:3333:3333:bbbb::b")

	operations := []struct {
		ip            net.IP
		a48, a56, a64 bool
	}{
		{ip64a, true, true, true},
		{ip64b, false, false, false},

		{ip56a, true, true, true},
		{ip56b, false, false, true},

		{ip48a, true, true, true},
		{ip48b, false, true, true},
	}

	l := NewLimiter(1, time.Second, 256)
	for i, op := range operations {
		a48, a56, a64 := l.allowV6(op.ip)
		diff := cmp.Diff(
			[]bool{op.a48, op.a56, op.a64},
			[]bool{a48, a56, a64},
		)
		if diff != "" {
			t.Errorf("operation %d: Allow(%v) mismatch (-want +got):\n%s",
				i, op.ip, diff)
		}
	}
}

func TestSize(t *testing.T) {
	sizes := []int{
		1, 2, 3, 5, 8, 10, 100, 256, 10000,
	}
	for _, size := range sizes {
		l := newlimiter(1, 0, size)

		// Note we avoid i=0 because we never expect the zero IP to be
		// allowed.
		i := 1

		// First, run up to size to fill in the map.
		for ; i < size+1; i++ {
			ip := net.IPv4(byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
			if !l.allow(ipv4ToUint64(ip.To4())) {
				t.Errorf("size %d, IP %v, i %d: not allowed", size, ip, i)
			}

			if len(l.m) != i {
				t.Errorf("size %d, IP %v, i %d: len %d != i %d",
					size, ip, i, len(l.m), i)
			}
		}

		// Now do another size iterations, checking that the size of the maps
		// stays constant.
		for ; i < (size+1)*2; i++ {
			ip := net.IPv4(byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
			if !l.allow(ipv4ToUint64(ip.To4())) {
				t.Errorf("size %d, IP %v, i %d: not allowed", size, ip, i)
			}

			if len(l.m) != size {
				t.Errorf("size %d, IP %v, i %d: len %d != size %d",
					size, ip, i, len(l.m), size)
			}
		}
	}
}

func TestLRU(t *testing.T) {
	ip1 := net.IPv4(1, 1, 1, 1)
	ip2 := net.IPv4(2, 2, 2, 2)
	ip3 := net.IPv4(3, 3, 3, 3)
	ip4 := net.IPv4(4, 4, 4, 4)

	// We're going to do a sequence of allow() calls, and check that the LRU
	// list is as we expect after each one.
	operations := []struct {
		ip  net.IP
		lru []net.IP
	}{
		{ip1, []net.IP{ip1}},

		// Bump ip1 (it is a special case when there's only one element).
		{ip1, []net.IP{ip1}},

		// Add ip2 and ip3, all straightforward.
		{ip2, []net.IP{ip2, ip1}},
		{ip3, []net.IP{ip3, ip2, ip1}},

		// Add ip4, evict ip1 which is the oldest.
		{ip4, []net.IP{ip4, ip3, ip2}},

		// Add ip1, evict ip2 which is the oldest.
		{ip1, []net.IP{ip1, ip4, ip3}},

		// Bump ip3 (last one), twice in a row.
		{ip3, []net.IP{ip3, ip1, ip4}},
		{ip3, []net.IP{ip3, ip1, ip4}},

		// Bump ip1 (middle one).
		{ip1, []net.IP{ip1, ip3, ip4}},
	}

	l := newlimiter(1, 0, 3)
	for i, op := range operations {
		l.allow(ipv4ToUint64(op.ip.To4()))
		lru := getLRU(l)

		if diff := cmp.Diff(op.lru, lru); diff != "" {
			t.Errorf("operation %d: allow(%v)", i, op.ip)
			t.Errorf("    expected LRU %v, got %v", op.lru, lru)
			t.Errorf("    diff (-want +got):\n%s", diff)
		}
	}
}

func getLRU(l *limiter) []net.IP {
	r := []net.IP{}
	k := l.lruFirst
	for k != 0 {
		ip := net.IPv4(byte(k>>24), byte(k>>16), byte(k>>8), byte(k))
		r = append(r, ip)
		k = l.m[k].lruNext
	}
	return r
}

func TestZeroKey(t *testing.T) {
	l := newlimiter(1, 0, 3)
	if !l.allow(0) {
		t.Errorf("allow(0) = false, want true")
	}
	lru := getLRU(l)
	expected := []net.IP{net.IPv4(0, 0, 0, 1)}
	if diff := cmp.Diff(expected, lru); diff != "" {
		t.Errorf("allow(0):")
		t.Errorf("    expected LRU %v, got %v", expected, lru)
		t.Errorf("    diff (-want +got):\n%s", diff)
	}
}

var (
	nowSec  = int64(0)
	nowNsec = int64(0)
)

func fakeTimeNow() time.Time {
	return time.Unix(nowSec, nowNsec)
}

func TestTime(t *testing.T) {
	// Override the time function so we can control the time.
	timeNow = fakeTimeNow
	defer func() { timeNow = time.Now }()

	l := newlimiter(2, time.Second, 3)
	check := func(want bool) {
		t.Helper()
		if got := l.allow(22); got != want {
			t.Errorf("@%s: allow(22) = %v, want %v", timeNow(), got, want)
		}
	}

	nowSec = 500
	nowNsec = 1000
	check(true) // Request 1.
	nowNsec = 1001
	check(true) // Request 2, last one allowed.
	nowNsec = 1002
	check(false) // Request 3, limit exhausted.

	nowSec, nowNsec = 501, 999
	check(false) // Not yet 1s.

	nowNsec = 1000
	check(true) // Exactly 1s since last allowed.
	nowNsec = 1001
	check(true) // Request 2, last one allowed.
	nowNsec = 1003
	check(false) // Request 3, limit exhausted.
}

func TestSetIPv6Rates(t *testing.T) {
	check := func(l *limiter, req uint64, period time.Duration) {
		t.Helper()
		if l.Requests != req || l.Period != period {
			t.Errorf("Requests / Period = %d / %v ; expect %d / %v",
				l.Requests, l.Period, req, period)
		}
	}
	l := NewLimiter(1, time.Second, 3)
	check(l.ipv4, 1, time.Second)
	check(l.ip64, 1, time.Second)
	check(l.ip56, 1, time.Second/4)
	check(l.ip48, 1, time.Second/8)

	l.SetIPv6s64Rate(64, time.Second/64)
	check(l.ipv4, 1, time.Second)
	check(l.ip64, 64, time.Second/64)
	check(l.ip56, 1, time.Second/4)
	check(l.ip48, 1, time.Second/8)

	l.SetIPv6s56Rate(56, time.Second/56)
	check(l.ipv4, 1, time.Second)
	check(l.ip64, 64, time.Second/64)
	check(l.ip56, 56, time.Second/56)
	check(l.ip48, 1, time.Second/8)

	l.SetIPv6s48Rate(48, time.Second/48)
	check(l.ipv4, 1, time.Second)
	check(l.ip64, 64, time.Second/64)
	check(l.ip56, 56, time.Second/56)
	check(l.ip48, 48, time.Second/48)
}

func TestDebugString(t *testing.T) {
	l := NewLimiter(1, time.Second, 3)
	l.Allow(net.IPv4(1, 1, 1, 1))
	l.Allow(net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888"))
	t.Logf(l.DebugString())
}

func TestDebugHTML(t *testing.T) {
	l := NewLimiter(1, time.Second, 3)
	l.Allow(net.IPv4(1, 1, 1, 1))
	l.Allow(net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888"))
	t.Logf(l.DebugHTML())
}

func BenchmarkDifferentIPv4_256(b *testing.B) {
	l := NewLimiter(1, time.Second, 256)
	for i := 0; i < b.N; i++ {
		l.Allow(net.IPv4(byte(i>>24), byte(i>>16), byte(i>>8), byte(i)))
	}
}

func BenchmarkDifferentIPv4_10000(b *testing.B) {
	l := NewLimiter(1, time.Second, 10000)
	for i := 0; i < b.N; i++ {
		l.Allow(net.IPv4(byte(i>>24), byte(i>>16), byte(i>>8), byte(i)))
	}
}

func BenchmarkSameIPv4_Strict(b *testing.B) {
	l := NewLimiter(1, time.Second, 256)
	ip := net.IPv4(1, 2, 3, 4)
	for i := 0; i < b.N; i++ {
		l.Allow(ip)
	}
}

func BenchmarkSameIPv4_Bursty(b *testing.B) {
	l := NewLimiter(100, time.Second, 256)
	ip := net.IPv4(1, 2, 3, 4)
	for i := 0; i < b.N; i++ {
		l.Allow(ip)
	}
}

func BenchmarkSameIPv4_Strict_Parallel(b *testing.B) {
	l := NewLimiter(1, time.Second, 256)
	ip := net.IPv4(1, 2, 3, 4)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow(ip)
		}
	})
}

func BenchmarkDifferentIPv6_256(b *testing.B) {
	l := NewLimiter(1, time.Second, 256)
	ip := net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888")
	for i := 0; i < b.N; i++ {
		// Change the IP at different levels, so they don't all fall under the
		// same /64, /56, /48.
		ip[7] = byte(i)
		ip[6] = byte(i >> 8)
		ip[5] = byte(i)
		ip[4] = byte(i >> 8)
		l.Allow(ip)
	}
}

func BenchmarkDifferentIPv6_10000(b *testing.B) {
	l := NewLimiter(1, time.Second, 10000)
	ip := net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888")
	for i := 0; i < b.N; i++ {
		ip[7] = byte(i)
		ip[6] = byte(i >> 8)
		ip[5] = byte(i)
		ip[4] = byte(i >> 8)
		l.Allow(ip)
	}
}

func BenchmarkSameIPv6_Strict(b *testing.B) {
	l := NewLimiter(1, time.Second, 256)
	ip := net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888")
	for i := 0; i < b.N; i++ {
		l.Allow(ip)
	}
}

func BenchmarkSameIPv6_Bursty(b *testing.B) {
	l := NewLimiter(100, time.Second, 256)
	ip := net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888")
	for i := 0; i < b.N; i++ {
		l.Allow(ip)
	}
}

func BenchmarkSameIPv6_Strict_Parallel(b *testing.B) {
	l := NewLimiter(100, time.Second, 256)
	ip := net.ParseIP("1111:2222:3333:4444:5555:6666:7777:8888")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow(ip)
		}
	})
}
