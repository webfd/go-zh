// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Central free lists.
//
// See malloc.h for an overview.
//
// The MCentral doesn't actually contain the list of free objects; the MSpan does.
// Each MCentral is two lists of MSpans: those with free objects (c->nonempty)
// and those that are completely allocated (c->empty).
//
// TODO(rsc): tcmalloc uses a "transfer cache" to split the list
// into sections of class_to_transfercount[sizeclass] objects
// so that it is faster to move those lists between MCaches and MCentrals.

#include "runtime.h"
#include "arch_GOARCH.h"
#include "malloc.h"

static bool MCentral_Grow(MCentral *c);
static void MCentral_Free(MCentral *c, MLink *v);
static void MCentral_ReturnToHeap(MCentral *c, MSpan *s);

// Initialize a single central free list.
void
runtime·MCentral_Init(MCentral *c, int32 sizeclass)
{
	c->sizeclass = sizeclass;
	runtime·MSpanList_Init(&c->nonempty);
	runtime·MSpanList_Init(&c->empty);
}

// Allocate a span to use in an MCache.
MSpan*
runtime·MCentral_CacheSpan(MCentral *c)
{
	MSpan *s;
	int32 cap, n;
	uint32 sg;

	runtime·lock(c);
	sg = runtime·mheap.sweepgen;
retry:
	for(s = c->nonempty.next; s != &c->nonempty; s = s->next) {
		if(s->sweepgen == sg-2 && runtime·cas(&s->sweepgen, sg-2, sg-1)) {
			runtime·unlock(c);
			runtime·MSpan_Sweep(s);
			runtime·lock(c);
			// the span could have been moved to heap, retry
			goto retry;
		}
		if(s->sweepgen == sg-1) {
			// the span is being swept by background sweeper, skip
			continue;
		}
		// we have a nonempty span that does not require sweeping, allocate from it
		goto havespan;
	}

	for(s = c->empty.next; s != &c->empty; s = s->next) {
		if(s->sweepgen == sg-2 && runtime·cas(&s->sweepgen, sg-2, sg-1)) {
			// we have an empty span that requires sweeping,
			// sweep it and see if we can free some space in it
			runtime·MSpanList_Remove(s);
			// swept spans are at the end of the list
			runtime·MSpanList_InsertBack(&c->empty, s);
			runtime·unlock(c);
			runtime·MSpan_Sweep(s);
			runtime·lock(c);
			// the span could be moved to nonempty or heap, retry
			goto retry;
		}
		if(s->sweepgen == sg-1) {
			// the span is being swept by background sweeper, skip
			continue;
		}
		// already swept empty span,
		// all subsequent ones must also be either swept or in process of sweeping
		break;
	}

	// Replenish central list if empty.
	if(!MCentral_Grow(c)) {
		runtime·unlock(c);
		return nil;
	}
	goto retry;

havespan:
	cap = (s->npages << PageShift) / s->elemsize;
	n = cap - s->ref;
	if(n == 0)
		runtime·throw("empty span");
	if(s->freelist == nil)
		runtime·throw("freelist empty");
	c->nfree -= n;
	runtime·MSpanList_Remove(s);
	runtime·MSpanList_InsertBack(&c->empty, s);
	s->incache = true;
	runtime·unlock(c);
	return s;
}

// Return span from an MCache.
void
runtime·MCentral_UncacheSpan(MCentral *c, MSpan *s)
{
	MLink *v;
	int32 cap, n;

	runtime·lock(c);

	s->incache = false;

	// Move any explicitly freed items from the freebuf to the freelist.
	while((v = s->freebuf) != nil) {
		s->freebuf = v->next;
		runtime·markfreed(v);
		v->next = s->freelist;
		s->freelist = v;
		s->ref--;
	}

	if(s->ref == 0) {
		// Free back to heap.  Unlikely, but possible.
		MCentral_ReturnToHeap(c, s); // unlocks c
		return;
	}
	
	cap = (s->npages << PageShift) / s->elemsize;
	n = cap - s->ref;
	if(n > 0) {
		c->nfree += n;
		runtime·MSpanList_Remove(s);
		runtime·MSpanList_Insert(&c->nonempty, s);
	}
	runtime·unlock(c);
}

// Free the list of objects back into the central free list c.
// Called from runtime·free.
void
runtime·MCentral_FreeList(MCentral *c, MLink *start)
{
	MLink *next;

	runtime·lock(c);
	for(; start != nil; start = next) {
		next = start->next;
		MCentral_Free(c, start);
	}
	runtime·unlock(c);
}

// Helper: free one object back into the central free list.
// Caller must hold lock on c on entry.  Holds lock on exit.
static void
MCentral_Free(MCentral *c, MLink *v)
{
	MSpan *s;

	// Find span for v.
	s = runtime·MHeap_Lookup(&runtime·mheap, v);
	if(s == nil || s->ref == 0)
		runtime·throw("invalid free");
	if(s->sweepgen != runtime·mheap.sweepgen)
		runtime·throw("free into unswept span");
	
	// If the span is currently being used unsynchronized by an MCache,
	// we can't modify the freelist.  Add to the freebuf instead.  The
	// items will get moved to the freelist when the span is returned
	// by the MCache.
	if(s->incache) {
		v->next = s->freebuf;
		s->freebuf = v;
		return;
	}

	// Move span to nonempty if necessary.
	if(s->freelist == nil) {
		runtime·MSpanList_Remove(s);
		runtime·MSpanList_Insert(&c->nonempty, s);
	}

	// Add the object to span's free list.
	runtime·markfreed(v);
	v->next = s->freelist;
	s->freelist = v;
	s->ref--;
	c->nfree++;

	// If s is completely freed, return it to the heap.
	if(s->ref == 0) {
		MCentral_ReturnToHeap(c, s); // unlocks c
		runtime·lock(c);
	}
}

// Free n objects from a span s back into the central free list c.
// Called during sweep.
// Returns true if the span was returned to heap.  Sets sweepgen to
// the latest generation.
bool
runtime·MCentral_FreeSpan(MCentral *c, MSpan *s, int32 n, MLink *start, MLink *end)
{
	if(s->incache)
		runtime·throw("freespan into cached span");
	runtime·lock(c);

	// Move to nonempty if necessary.
	if(s->freelist == nil) {
		runtime·MSpanList_Remove(s);
		runtime·MSpanList_Insert(&c->nonempty, s);
	}

	// Add the objects back to s's free list.
	end->next = s->freelist;
	s->freelist = start;
	s->ref -= n;
	c->nfree += n;
	
	// delay updating sweepgen until here.  This is the signal that
	// the span may be used in an MCache, so it must come after the
	// linked list operations above (actually, just after the
	// lock of c above.)
	runtime·atomicstore(&s->sweepgen, runtime·mheap.sweepgen);

	if(s->ref != 0) {
		runtime·unlock(c);
		return false;
	}

	// s is completely freed, return it to the heap.
	MCentral_ReturnToHeap(c, s); // unlocks c
	return true;
}

void
runtime·MGetSizeClassInfo(int32 sizeclass, uintptr *sizep, int32 *npagesp, int32 *nobj)
{
	int32 size;
	int32 npages;

	npages = runtime·class_to_allocnpages[sizeclass];
	size = runtime·class_to_size[sizeclass];
	*npagesp = npages;
	*sizep = size;
	*nobj = (npages << PageShift) / size;
}

// Fetch a new span from the heap and
// carve into objects for the free list.
static bool
MCentral_Grow(MCentral *c)
{
	int32 i, n, npages;
	uintptr size;
	MLink **tailp, *v;
	byte *p;
	MSpan *s;

	runtime·unlock(c);
	runtime·MGetSizeClassInfo(c->sizeclass, &size, &npages, &n);
	s = runtime·MHeap_Alloc(&runtime·mheap, npages, c->sizeclass, 0, 1);
	if(s == nil) {
		// TODO(rsc): Log out of memory
		runtime·lock(c);
		return false;
	}

	// Carve span into sequence of blocks.
	tailp = &s->freelist;
	p = (byte*)(s->start << PageShift);
	s->limit = p + size*n;
	for(i=0; i<n; i++) {
		v = (MLink*)p;
		*tailp = v;
		tailp = &v->next;
		p += size;
	}
	*tailp = nil;
	runtime·markspan((byte*)(s->start<<PageShift), size, n, size*n < (s->npages<<PageShift));

	runtime·lock(c);
	c->nfree += n;
	runtime·MSpanList_Insert(&c->nonempty, s);
	return true;
}

// Return s to the heap.  s must be unused (s->ref == 0).  Unlocks c.
static void
MCentral_ReturnToHeap(MCentral *c, MSpan *s)
{
	int32 size;

	size = runtime·class_to_size[c->sizeclass];
	runtime·MSpanList_Remove(s);
	s->needzero = 1;
	s->freelist = nil;
	if(s->ref != 0)
		runtime·throw("ref wrong");
	c->nfree -= (s->npages << PageShift) / size;
	runtime·unlock(c);
	runtime·unmarkspan((byte*)(s->start<<PageShift), s->npages<<PageShift);
	runtime·MHeap_Free(&runtime·mheap, s, 0);
}
