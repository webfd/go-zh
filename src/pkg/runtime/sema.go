// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Semaphore implementation exposed to Go.
// Intended use is provide a sleep and wakeup
// primitive that can be used in the contended case
// of other synchronization primitives.
// Thus it targets the same goal as Linux's futex,
// but it has much simpler semantics.
//
// That is, don't think of these as semaphores.
// Think of them as a way to implement sleep and wakeup
// such that every sleep is paired with a single wakeup,
// even if, due to races, the wakeup happens before the sleep.
//
// See Mullender and Cox, ``Semaphores in Plan 9,''
// http://swtch.com/semaphore.pdf

package runtime

import "unsafe"

// Asynchronous semaphore for sync.Mutex.

type semaRoot struct {
	lock
	head  *sudog
	tail  *sudog
	nwait uint32 // Number of waiters. Read w/o the lock.
}

// Prime to not correlate with any user patterns.
const semTabSize = 251

var semtable [semTabSize]struct {
	root semaRoot
	pad  [cacheLineSize - unsafe.Sizeof(semaRoot{})]byte
}

// Called from sync/net packages.
func asyncsemacquire(addr *uint32) {
	semacquire(addr, true)
}

func asyncsemrelease(addr *uint32) {
	semrelease(addr)
}

// Called from runtime.
func semacquire(addr *uint32, profile bool) {
	// Easy case.
	if cansemacquire(addr) {
		return
	}

	// Harder case:
	//	increment waiter count
	//	try cansemacquire one more time, return if succeeded
	//	enqueue itself as a waiter
	//	sleep
	//	(waiter descriptor is dequeued by signaler)
	s := acquireSudog()
	root := semroot(addr)
	t0 := int64(0)
	s.releasetime = 0
	if profile && blockprofilerate > 0 {
		t0 = gocputicks()
		s.releasetime = -1
	}
	for {
		golock(&root.lock)
		// Add ourselves to nwait to disable "easy case" in semrelease.
		goxadd(&root.nwait, 1)
		// Check cansemacquire to avoid missed wakeup.
		if cansemacquire(addr) {
			goxadd(&root.nwait, ^uint32(0))
			gounlock(&root.lock)
			break
		}
		// Any semrelease after the cansemacquire knows we're waiting
		// (we set nwait above), so go to sleep.
		root.queue(addr, s)
		goparkunlock(&root.lock, "semacquire")
		if cansemacquire(addr) {
			break
		}
	}
	if s.releasetime > 0 {
		goblockevent(int64(s.releasetime)-t0, 4)
	}
	releaseSudog(s)
}

func semrelease(addr *uint32) {
	root := semroot(addr)
	goxadd(addr, 1)

	// Easy case: no waiters?
	// This check must happen after the xadd, to avoid a missed wakeup
	// (see loop in semacquire).
	if goatomicload(&root.nwait) == 0 {
		return
	}

	// Harder case: search for a waiter and wake it.
	golock(&root.lock)
	if goatomicload(&root.nwait) == 0 {
		// The count is already consumed by another goroutine,
		// so no need to wake up another goroutine.
		gounlock(&root.lock)
		return
	}
	s := root.head
	for ; s != nil; s = s.next {
		if s.elem == unsafe.Pointer(addr) {
			goxadd(&root.nwait, ^uint32(0))
			root.dequeue(s)
			break
		}
	}
	gounlock(&root.lock)
	if s != nil {
		if s.releasetime != 0 {
			// TODO: Remove use of unsafe here.
			releasetimep := (*int64)(unsafe.Pointer(&s.releasetime))
			*releasetimep = gocputicks()
		}
		goready(s.g)
	}
}

func semroot(addr *uint32) *semaRoot {
	return &semtable[(uintptr(unsafe.Pointer(addr))>>3)%semTabSize].root
}

func cansemacquire(addr *uint32) bool {
	for {
		v := goatomicload(addr)
		if v == 0 {
			return false
		}
		if gocas(addr, v, v-1) {
			return true
		}
	}
}

func (root *semaRoot) queue(addr *uint32, s *sudog) {
	s.g = getg()
	s.elem = unsafe.Pointer(addr)
	s.next = nil
	s.prev = root.tail
	if root.tail != nil {
		root.tail.next = s
	} else {
		root.head = s
	}
	root.tail = s
}

func (root *semaRoot) dequeue(s *sudog) {
	if s.next != nil {
		s.next.prev = s.prev
	} else {
		root.tail = s.prev
	}
	if s.prev != nil {
		s.prev.next = s.next
	} else {
		root.head = s.next
	}
	s.next = nil
	s.prev = nil
}

// Synchronous semaphore for sync.Cond.
type syncSema struct {
	lock lock
	head *sudog
	tail *sudog
}

// Syncsemacquire waits for a pairing syncsemrelease on the same semaphore s.
func syncsemacquire(s *syncSema) {
	golock(&s.lock)
	if s.head != nil && s.head.nrelease > 0 {
		// Have pending release, consume it.
		var wake *sudog
		s.head.nrelease--
		if s.head.nrelease == 0 {
			wake = s.head
			s.head = wake.next
			if s.head == nil {
				s.tail = nil
			}
		}
		gounlock(&s.lock)
		if wake != nil {
			goready(wake.g)
		}
	} else {
		// Enqueue itself.
		w := acquireSudog()
		w.g = getg()
		w.nrelease = -1
		w.next = nil
		w.releasetime = 0
		t0 := int64(0)
		if blockprofilerate > 0 {
			t0 = gocputicks()
			w.releasetime = -1
		}
		if s.tail == nil {
			s.head = w
		} else {
			s.tail.next = w
		}
		s.tail = w
		goparkunlock(&s.lock, "semacquire")
		if t0 != 0 {
			goblockevent(int64(w.releasetime)-t0, 3)
		}
		releaseSudog(w)
	}
}

// Syncsemrelease waits for n pairing syncsemacquire on the same semaphore s.
func syncsemrelease(s *syncSema, n uint32) {
	golock(&s.lock)
	for n > 0 && s.head != nil && s.head.nrelease < 0 {
		// Have pending acquire, satisfy it.
		wake := s.head
		s.head = wake.next
		if s.head == nil {
			s.tail = nil
		}
		if wake.releasetime != 0 {
			// TODO: Remove use of unsafe here.
			releasetimep := (*int64)(unsafe.Pointer(&wake.releasetime))
			*releasetimep = gocputicks()
		}
		goready(wake.g)
		n--
	}
	if n > 0 {
		// enqueue itself
		w := acquireSudog()
		w.g = getg()
		w.nrelease = int32(n)
		w.next = nil
		w.releasetime = 0
		if s.tail == nil {
			s.head = w
		} else {
			s.tail.next = w
		}
		s.tail = w
		goparkunlock(&s.lock, "semarelease")
	} else {
		gounlock(&s.lock)
	}
}

func syncsemcheck(sz uintptr) {
	if sz != unsafe.Sizeof(syncSema{}) {
		print("runtime: bad syncSema size - sync=", sz, " runtime=", unsafe.Sizeof(syncSema{}), "\n")
		gothrow("bad syncSema size")
	}
}
