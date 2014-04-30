package main

/* Metric modifications

   Metric:
   .check              total sites checked
   .cache.hit          cache hit/miss
   .cache.miss
   .cache.vulerable    cached sate
   .cache.safe
   .cache.error
   .site.vulnerable    active check state
   .site.safe
   .site.error
*/

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"

	"github.com/docopt/docopt-go"

	bleed "github.com/mozilla-services/Heartbleed/bleed"
	cache "github.com/mozilla-services/Heartbleed/server/cache"
	mz_metrics "github.com/mozilla-services/Heartbleed/server/metrics"
)

var PAYLOAD = []byte("filippo.io/Heartbleed")

var withCache bool

var metrics *mz_metrics.Metrics

type result struct {
	Code  int    `json:"code"`
	Data  string `json:"data"`
	Error string `json:"error"`
	Host  string `json:"host"`
}

func handleRequest(tgt *bleed.Target, w http.ResponseWriter, r *http.Request, skip bool) {
	if tgt.HostIp == "" {
		// tens of empty requests per minute, mah...
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")

	var rc int
	var errS string
	var data string

	var rc_state = []string{"vulnerable", "safe", "error"}

	cacheKey := tgt.Service + "://" + tgt.HostIp
	if skip {
		cacheKey += "/skip"
	}

	if metrics != nil {
		metrics.Increment("check")
	}

	var cacheOk bool
	if withCache {
		cReply, ok := cache.Check(cacheKey)
		if ok {
			rc = int(cReply.Status)
			errS = cReply.Error
			data = cReply.Data
			cacheOk = true
			if metrics != nil {
				metrics.Increment("cache.hit")
				metrics.Increment("cache." + rc_state[rc])
			}
		}
	}

	if !withCache || !cacheOk {
		out, err := bleed.Heartbleed(tgt, PAYLOAD, skip)

		if err == bleed.Safe || err == bleed.Closed {
			rc = 1
		} else if err != nil {
			rc = 2
		} else {
			rc = 0
			// _, err := bleed.Heartbleed(tgt, PAYLOAD)
			// if err == nil {
			// 	// Two VULN in a row
			// 	rc = 0
			// } else {
			// 	// One VULN and one not
			// 	_, err := bleed.Heartbleed(tgt, PAYLOAD)
			// 	if err == nil {
			// 		// 2 VULN on 3 tries
			// 		rc = 0
			// 	} else {
			// 		// 1 VULN on 3 tries
			// 		if err == bleed.Safe {
			// 			rc = 1
			// 		} else {
			// 			rc = 2
			// 		}
			// 	}
			// }
		}

		switch rc {
		case 0:
			data = out
			log.Printf("%v (%v) - VULNERABLE [skip: %v]", tgt.HostIp, tgt.Service, skip)
		case 1:
			log.Printf("%v (%v) - SAFE", tgt.HostIp, tgt.Service)
		case 2:
			errS = err.Error()
			if errS == "Please try again" {
				log.Printf("%v (%v) - MISMATCH", tgt.HostIp, tgt.Service)
			} else {
				log.Printf("%v (%v) - ERROR [%v]", tgt.HostIp, tgt.Service, errS)
			}
		}
		if metrics != nil {
			metrics.Increment("site." + rc_state[rc])
		}
	}

	if withCache && !cacheOk {
		if metrics != nil {
			metrics.Increment("cache.miss")
		}
		cache.Set(cacheKey, rc, data, errS)
	}

	res := result{rc, data, errS, tgt.HostIp}
	j, err := json.Marshal(res)
	if err != nil {
		log.Println("[json] ERROR:", err)
	} else {
		w.Write(j)
	}
}

func bleedHandler(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Path[len("/bleed/"):]

	tgt := bleed.Target{
		HostIp:  string(host),
		Service: "https",
	}
	handleRequest(&tgt, w, r, true)
}

func bleedQueryHandler(w http.ResponseWriter, r *http.Request) {
	q, ok := r.URL.Query()["u"]
	if !ok || len(q) != 1 {
		return
	}

	skip, ok := r.URL.Query()["skip"]
	s := false
	if ok && len(skip) == 1 {
		s = true
	}

	tgt := bleed.Target{
		HostIp:  string(q[0]),
		Service: "https",
	}

	u, err := url.Parse(tgt.HostIp)
	if err == nil && u.Host != "" {
		tgt.HostIp = u.Host
		if u.Scheme != "" {
			tgt.Service = u.Scheme
		}
	}

	handleRequest(&tgt, w, r, s)
}

var Usage = `Heartbleed test server.

Usage:
  HBserver --redir-host=<host> [--listen=<addr:port> --expiry=<duration> --metric.server=<addr:port> --metric.name=<statsd_name> --cache.name=<name>]
  HBserver -h | --help
  HBserver --version

Options:
  --redir-host HOST   Redirect requests to "/" to this host.
  --listen ADDR:PORT  Listen and serve requests to this address:port [default: :8082].
  --expiry DURATION   ENABLE CACHING. Expire records after this period.
                      Uses Go's parse syntax
                      e.g. 10m = 10 minutes, 600s = 600 seconds, 1d = 1 day, etc.
  -h --help           Show this screen.
  --metric.server ADDR:PORT    Address of the statsd server. If no value
                      present, no statsd reporting is performed
  --metric.name NAME  Statsd app prefix name to use [default: heartbleed]
  --cache.name NAME   Name of the AWS DDB cache to use
  --version           Show version.`

func main() {
	var err error
	arguments, err := docopt.Parse(Usage, nil, true, "HBserver 0.3", false)
	if err != nil {
		log.Printf("ERROR: %s", err.Error())
		return
	}

	var statsd_server, statsd_prefix string

	if arguments["--expiry"] != nil {
		withCache = true
	}

	if _, ok := arguments["--cache.name"]; !ok {
		arguments["--cache.name"] = ""
	}

	if withCache {
		cache.Init(arguments["--expiry"].(string), arguments["--cache.name"].(string))
	}

	if _, ok := arguments["--listen"]; !ok {
		arguments["--listen"] = ":8082"
	}

	// Metric handling block
	if arguments["--metric.server"] != nil {
		if prefix, ok := arguments["metric.name"]; !ok {
			statsd_prefix = "heartbleed"
		} else {
			statsd_prefix = prefix.(string)
		}

		metrics = mz_metrics.New(statsd_prefix, statsd_server)
		http.HandleFunc("/metrics", func(w http.ResponseWriter,
			r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			snapshot := metrics.Snapshot()
			if snapshot == nil {
				w.Write([]byte("{}"))
				return
			}
			reply, err := json.Marshal(snapshot)
			if err != nil {
				log.Printf("handler ERROR: Could not generate report: %s",
					err.Error())
				jer := make(map[string]bool)
				jer["error"] = true
				reply, err = json.Marshal(jer)
			}
			w.Write(reply)
			return
		})
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, arguments["--redir-host"].(string), http.StatusFound)
	})

	// Required for some ELBs
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/bleed/", bleedHandler)
	http.HandleFunc("/bleed/query", bleedQueryHandler)

	log.Printf("Starting server on %s\n", arguments["--listen"].(string))
	err = http.ListenAndServe(arguments["--listen"].(string), nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
