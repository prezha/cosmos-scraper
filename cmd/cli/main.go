/*
Copyright © 2022 prezha

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var version = "v0.3.0-beta"

func main() {
	stdLogger.Printf("cosmos-scraper %s started", version)

	ctx, cancel := context.WithCancel(context.Background())

	// gracefully stop if <Ctrl>-<C> or SIGTERM signal received
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		stopping := false // indicator if stop was (already) requested
		for sig := range c {
			// quit immediately if already requested (more than once)
			if stopping {
				stdLogger.Println("ok, cosmos-scraper stopped forcibly (there might be some inconsistencies requiring manual recovery).")
				os.Exit(0)
			}
			stdLogger.Printf("received %v signal, trying to stop 'gracefully' (signal again to quit immediately)...", sig)
			stopping = true
			cancel()
		}
	}()

	dbc, bxs, txs := initDB(ctx, dbHost, dbPort, dbUser, dbPass, napTime)
	defer func() {
		recover() // silence any panics
		if err := dbc.Disconnect(ctx); err != nil {
			stdLogger.Fatalf("failed disconnecting from database: %v", err)
		}
	}()

	bcc, tail, head := initBC(ctx, bcNode, bcPort)

	stdLogger.Printf("spawning workers...")
	reqChan := make(chan request, maxReqWorkers)
	perChan := make(chan persist, maxPerWorkers)
	var wgr, wgp sync.WaitGroup
	for i := 0; i < maxReqWorkers; i++ {
		wgr.Add(1)
		go func() {
			defer wgr.Done()
			reqWorker(ctx, bcc, bxs, txs, reqChan, perChan, napTime)
		}()
	}
	for i := 0; i < maxPerWorkers; i++ {
		wgp.Add(1)
		go func() {
			defer wgp.Done()
			perWorker(ctx, perChan)
		}()
	}

	stdLogger.Printf("starting scraping from block %d to %d", tail, head)
	// catch up and keep up with current blockchain height
	var err error
	for ctx.Err() == nil {
		stdLogger.Printf("queuing new blocks [%d..%d]", tail, head)
		// fill-in buffered reqChan channel in bulks of maxReqWorkers new requests
		for ctx.Err() == nil && tail <= head {
			reqChan <- request{height: tail}
			tail++ // next unprocessed block
		}
		// wait for new blocks
		for ctx.Err() == nil && tail > head {
			stdLogger.Printf("no new blocks after %d - napping for %s", head, napTime)
			select {
			case <-ctx.Done():
				continue // will break from this and also outer loop because of ctx.Err()
			case <-time.After(napTime):
				stdLogger.Println("awakening...")
				if head, err = bcHeight(ctx, bcc, napTime); err != nil {
					stdLogger.Panicf("error getting current blockchain height: %v", err)
				}
			}
		}
	}

	// gracefully exit
	stdLogger.Println("stopping requesters...")
	close(reqChan)
	wgr.Wait()
	stdLogger.Println("requesters stopped")

	stdLogger.Println("stopping persisters...")
	close(perChan)
	wgp.Wait()
	stdLogger.Println("persisters stopped")

	stdLogger.Println("cosmos-scraper stopped 'gracefully'.")
}
