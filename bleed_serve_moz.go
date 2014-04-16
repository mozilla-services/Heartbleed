// From heartbleed.fillip.io
// Adding DynamoDB caching.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	mzutil "github.com/mozilla-services/Heartbleed/mzutil"

	flags "github.com/jessevdk/go-flags"
	bleed "github.com/mozilla-services/Heartbleed/bleed"
	"github.com/smugmug/godynamo/conf"
	"github.com/smugmug/godynamo/conf_file"
	keepalive "github.com/smugmug/godynamo/keepalive"
)

var PAYLOAD = []byte("heartbleed.mozilla.com")

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, REDIRHOST, http.StatusFound)
}

func testHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK")
}

type result struct {
	Code  int    `json:"code"`
	Data  string `json:"data"`
	Error string `json:"error"`
	Host  string `json:"host"`
}

func handleRequest(tgt *bleed.Target, w http.ResponseWriter, r *http.Request, skip bool) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Caching
	var fullCheck = true
	var rc int
	var err error
	var errS string
	data := ""
	if cReply, ok := cacheCheck(tgt.HostIp); ok {
		log.Printf("creply %+v %s\n", cReply, ok)
		if cReply.LastUpdate < time.Now().UTC().Truncate(EXPRY).Unix() {
			log.Printf("Refetching " + tgt.HostIp)
		} else {
			rc = int(cReply.Status)
			fullCheck = false
		}
	}

	if fullCheck {
		log.Printf("Checking " + tgt.HostIp)
		data, err = bleed.Heartbleed(tgt, PAYLOAD, skip)

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
		err = cacheSet(tgt.HostIp, rc)

		switch rc {
		case 0:
			log.Printf("%v (%v) - VULNERABLE [skip: %v]", tgt.HostIp, tgt.Service, skip)
		case 1:
			data = ""
			log.Printf("%v (%v) - SAFE", tgt.HostIp, tgt.Service)
		case 2:
			data = ""
			if err != nil {
				errS = err.Error()
				if errS == "Please try again" {
					log.Printf("%v (%v) - MISMATCH", tgt.HostIp, tgt.Service)
				} else {
					log.Printf("%v (%v) - ERROR [%v]", tgt.HostIp, tgt.Service, errS)
				}
			}
		}
	}

	// clear the data, because we don't want to expose that.
	data = ""

	res := result{rc, data, errS, tgt.HostIp}
	j, err := json.Marshal(res)
	if err != nil {
		log.Println("ERROR", err)
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

func main() {

	var err error

	// Get the configurations
	flags.Parse(&opts)
	if opts.ConfigFile == "" {
		opts.ConfigFile = "config.ini"
	}
	config, err := mzutil.ReadMzConfig(opts.ConfigFile)
	if err != nil {
		log.Fatal("Could not read config file " +
			opts.ConfigFile + " " +
			err.Error())
	}
	config.SetDefault("VERSION", "0.5")
	REDIRHOST = config.Get("redir.host", "localhost")
	PORT_SRV = config.Get("listen.port", ":8082")
	os.Setenv("GODYNAMO_CONF_FILE",
		config.Get("godynamo.conf.file", "./conf/aws-config.json"))

	// should take a conf arg
	conf_file.Read()
	EXPRY, err = time.ParseDuration(config.Get("expry", "10m"))

	if conf.Vals.Initialized == false {
		panic("Uninitialized conf.Vals global")
	}

	if conf.Vals.Network.DynamoDB.KeepAlive {
		log.Printf("Launching background DynamoDB keepalive")
		go keepalive.KeepAlive([]string{conf.Vals.Network.DynamoDB.URL})
	}
	// if we were using IAM, put that code here.

	http.HandleFunc("/", defaultHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/bleed/", bleedHandler)
	http.HandleFunc("/bleed/query", bleedQueryHandler)
	log.Printf("Starting server on %s\n", PORT_SRV)
	err = http.ListenAndServe(PORT_SRV, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
