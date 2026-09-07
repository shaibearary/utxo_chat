package network

import (
	"sync"
	"time"

	"github.com/shaibearary/utxo_chat/message"
)

// String names a message source for the operator console and logs.
func (s MessageSource) String() string {
	switch s {
	case MessageSourcePeer:
		return "peer"
	case MessageSourceLocal:
		return "local"
	case MessageSourceWallet:
		return "wallet"
	}
	return "unknown"
}

// Origin records how this node came to hold a message: which peer delivered it
// and every peer that has announced it. Without this a relayed message is
// indistinguishable from one submitted here, which makes it impossible to watch
// a message propagate across a multi-node setup.
//
// It is deliberately in-memory. This is one node's view of the gossip rather
// than a property of the message, and nothing in it is signed: persisting it
// would file unverifiable hearsay from peers alongside the message store. A
// restarted node reports messages it reloads from disk as having no known
// origin, which is the honest answer — it did not see them arrive.
type Origin struct {
	// Source is how the message entered this node.
	Source MessageSource

	// Peer is the address the message was first received from. Empty when the
	// message was submitted locally.
	Peer string

	// Announcers holds every peer that has sent an INV for this outpoint,
	// including those whose announcement lost the race to deliver it. A peer
	// appears at most once however many times it announces.
	Announcers []string

	// FirstSeen is when this node first heard of the outpoint, by either route.
	FirstSeen time.Time

	// Delivered separates a record created by an announcement alone from one
	// where the body actually arrived here. It has to be reported, not just
	// used internally: the zero value of Source is MessageSourcePeer, so an
	// outpoint this node has only ever heard advertised would otherwise be
	// rendered as "relayed by an unknown peer".
	Delivered bool
}

// originTracker holds the Origin records. Announcements arrive on peer read
// loops and deliveries on separate goroutines, so every access takes the lock.
type originTracker struct {
	mu      sync.RWMutex
	records map[message.Outpoint]*Origin
}

func newOriginTracker() *originTracker {
	return &originTracker{records: make(map[message.Outpoint]*Origin)}
}

// get returns the record for an outpoint, creating it on first sight. The
// caller must hold the write lock.
func (t *originTracker) get(outpoint message.Outpoint) *Origin {
	rec, ok := t.records[outpoint]
	if !ok {
		rec = &Origin{FirstSeen: time.Now()}
		t.records[outpoint] = rec
	}
	return rec
}

// recordAnnounce notes that a peer advertised an outpoint. It is called for
// every INV item, including outpoints this node already stores, because the
// point is to show who is advertising rather than who is telling us something
// new.
func (t *originTracker) recordAnnounce(outpoint message.Outpoint, peer string) {
	if peer == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	rec := t.get(outpoint)
	for _, existing := range rec.Announcers {
		if existing == peer {
			return
		}
	}
	rec.Announcers = append(rec.Announcers, peer)
}

// recordDelivery notes that the message body arrived and was accepted. The
// first delivery wins: a message that arrives again from a second peer was
// still learned from the first, and overwriting would erase that.
func (t *originTracker) recordDelivery(outpoint message.Outpoint, source MessageSource, peer string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec := t.get(outpoint)
	if rec.Delivered {
		return
	}
	rec.Delivered = true
	rec.Source = source
	rec.Peer = peer
}

// lookup returns a copy of the record for an outpoint. The copy matters: the
// caller renders it without the lock, and Announcers would otherwise be a slice
// a peer goroutine can append to concurrently.
func (t *originTracker) lookup(outpoint message.Outpoint) (Origin, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rec, ok := t.records[outpoint]
	if !ok {
		return Origin{}, false
	}

	out := *rec
	out.Announcers = append([]string(nil), rec.Announcers...)
	return out, true
}

// forget drops the record for an outpoint whose message is no longer stored,
// so the tracker does not outgrow the message store it describes.
func (t *originTracker) forget(outpoint message.Outpoint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.records, outpoint)
}
