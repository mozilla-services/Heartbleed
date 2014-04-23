package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	ep "github.com/smugmug/godynamo/endpoint"
	get "github.com/smugmug/godynamo/endpoints/get_item"
	put "github.com/smugmug/godynamo/endpoints/put_item"
)

var REDIRHOST = "http://localhost"
var PORT_SRV = ":8082"
var CACHE_TAB = "mozHeartbleed"
var EXPRY time.Duration
var VERSION = "0.1"

const (
	VUNERABLE = iota
	SAFE
	ERROR
)

/* Command line args for the app.
 */
var opts struct {
	ConfigFile string `short:"c" long:"config" optional:"true" description:"General Config file"`
	Profile    string `long:"profile" optional:"true"`
	MemProfile string `long:"memprofile" optional:"true"`
	LogLevel   int    `short:"l" long:"loglevel" optional:"true"`
}

type cacheReply struct {
	Host       string
	LastUpdate int64
	Status     int64
}

type jrep map[string]interface{}

func cacheCheck(host string) (reply cacheReply, ok bool) {
	var getr get.Request
	var gr get.Response

	ok = false
	getr.TableName = CACHE_TAB
	getr.Key = make(ep.Item)
	getr.Key["hostname"] = ep.AttributeValue{S: host}
	body, code, err := getr.EndpointReq()
	if err != nil || code != http.StatusOK {
		if err != nil {
			log.Printf("!!!! Error: %s\n", err.Error())
		}
		ok = false
		return
	}
	// get the time from the body,
	log.Printf("####CACHE_GET: %s %d\n", string(body), len(body))
	if len(body) < 3 {
		ok = false
		return reply, ok
	}

	if err = json.Unmarshal([]byte(body), &gr); err == nil {
		reply.LastUpdate, err = strconv.ParseInt(gr.Item["Mtime"].N, 10, 64)
		if err != nil {
			log.Printf("Bad Record %s, %s", host, err.Error())
			ok = false
			return
		}
		reply.Status, err = strconv.ParseInt(gr.Item["Status"].N, 10, 64)
		if err != nil {
			log.Printf("Bad Record %s, %s", host, err.Error())
			ok = false
			return
		}
		reply.Host = gr.Item["hostname"].S
		//log.Printf("gr %+v", reply)
		ok = true
	}
	// if the time has not expired, then things are good.
	// else retry.
	return
}

func cacheSet(host string, state int) (err error) {
	var putr put.Request
	//var status string

	putr.TableName = CACHE_TAB
	putr.Item = make(ep.Item)
	putr.Item["hostname"] = ep.AttributeValue{S: host}
	putr.Item["Mtime"] = ep.AttributeValue{N: strconv.FormatInt(time.Now().UTC().Unix(), 10)}
	putr.Item["Status"] = ep.AttributeValue{N: strconv.FormatInt(int64(state), 10)}
	body, code, err := putr.EndpointReq()
	if err != nil || code != http.StatusOK {
		fmt.Printf("put failed %d, %v, %s", code, err, body)
	}
	log.Printf("####CACHE_SET: %s\n", string(body))
	return
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}
