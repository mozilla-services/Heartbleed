/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package metrics

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
)

type Metrics struct {
	sync.Mutex
	dict   map[string]int64   // counters
	timer  map[string]float64 // timers
	prefix string             // prefix for
	statsd *statsd.Client
	born   time.Time
}

/* Run time metrics with statsd reporting
 */
func New(prefix, server string) (self *Metrics) {

	var statsdc *statsd.Client
	if server != "" {
		name := strings.ToLower(prefix)
		client, err := statsd.New(server, name)
		if err != nil {
			log.Printf("Metrics ERROR: Could not init statsd connection: %s",
				err.Error())
		} else {
			statsdc = client
		}
	}

	self = &Metrics{
		dict:   make(map[string]int64),
		timer:  make(map[string]float64),
		prefix: prefix,
		statsd: statsdc,
		born:   time.Now(),
	}
	return self
}

/* Change the prefix used for statsd reporting
 */
func (self *Metrics) Prefix(newPrefix string) {
	self.prefix = strings.TrimRight(newPrefix, ".")
	if self.statsd != nil {
		self.statsd.SetPrefix(newPrefix)
	}
}

/* Return a snapshot of the metric data
 */
func (self *Metrics) Snapshot() map[string]interface{} {
	defer self.Unlock()
	self.Lock()
	var pfx string
	if len(self.prefix) > 0 {
		pfx = self.prefix + "."
	}
	oldMetrics := make(map[string]interface{})
	// copy the old metrics
	for k, v := range self.dict {
		oldMetrics[pfx+"counter."+k] = v
	}
	for k, v := range self.timer {
		oldMetrics[pfx+"avg."+k] = v
	}
	oldMetrics[pfx+"server.age"] = time.Now().Unix() - self.born.Unix()
	return oldMetrics
}

/* Change the value of a counter
 */
func (self *Metrics) IncrementBy(metric string, count int) {
	defer self.Unlock()
	self.Lock()
	m, ok := self.dict[metric]
	if !ok {
		self.dict[metric] = int64(0)
		m = self.dict[metric]
	}
	atomic.AddInt64(&m, int64(count))
	self.dict[metric] = m
	log.Printf("metrics INFO: counter.%s = %d", metric, m)
	if self.statsd != nil {
		if count >= 0 {
			self.statsd.Inc(metric, int64(count), 1.0)
		} else {
			self.statsd.Dec(metric, int64(count), 1.0)
		}
	}
}

/* Convenience function to increment a counter
 */
func (self *Metrics) Increment(metric string) {
	self.IncrementBy(metric, 1)
}

/* Convenience function to decrement a counter
 */
func (self *Metrics) Decrement(metric string) {
	self.IncrementBy(metric, -1)
}

/* Record a time, keeping a running average of times for the dump
 */
func (self *Metrics) Timer(metric string, value int64) {
	defer self.Unlock()
	self.Lock()
	if m, ok := self.timer[metric]; !ok {
		self.timer[metric] = float64(value)
	} else {
		// calculate running average
		fv := float64(value)
		dm := (fv - m) / 2
		switch {
		case fv < m:
			self.timer[metric] = m - dm
		case fv > m:
			self.timer[metric] = m + dm
		}
	}

	log.Printf("metrics INFO: timer.%s = %d", metric, value)
	if self.statsd != nil {
		self.statsd.Timing(metric, value, 1.0)
	}
}
