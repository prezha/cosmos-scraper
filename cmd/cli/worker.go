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
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

type request struct {
	height int
}

type persist struct {
	height   int
	datatype string
	raw      []byte
	col      *mongo.Collection
}

// reqWorker gets block from reqChan (based on specific height) and send it to perChan channel along with any transactions found in that block
func reqWorker(ctx context.Context, bcc *bcClient, bxs, txs *mongo.Collection, reqChan <-chan request, perChan chan<- persist, napTime time.Duration) {
	for r := range reqChan {
		b, err := blockAt(ctx, bcc, fmt.Sprint(r.height), napTime)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				continue // drain channel to shutdown, then exit
			}
			// skip blocks (and transactions) unavailable due to bc hardforks
			// example response: '400 Bad Request: { "code": 3, "message": "height 1 is not available, lowest height is 1995900: invalid request", "details": [ ]}'
			// note: api/response might change in the future
			if strings.Contains(err.Error(), fmt.Sprintf("height %d is not available", r.height)) {
				bxsLogger.Printf("%d unavailable (skipping): %v", r.height, err)
				txsLogger.Printf("%d unavailable (skipping): %v", r.height, err)
				continue
			}
			stdLogger.Panicf("error getting block at height %d (unretryable): %v", r.height, err)
		}
		perChan <- persist{
			height:   r.height,
			datatype: "block",
			raw:      b,
			col:      bxs,
		}

		// get only non-empty transactions
		t, err := transactionsAt(ctx, bcc, fmt.Sprint(r.height), napTime)
		if err != nil {
			stdLogger.Panicf("error getting transactions at height %d (unretryable): %v", r.height, err)
		}
		if t == nil {
			txsLogger.Printf("%d empty (skipping)", r.height)
			continue
		}
		perChan <- persist{
			height:   r.height,
			datatype: "transactions",
			raw:      t,
			col:      txs,
		}
	}
}

// perWorker saves blocks and transactions from perChan channel
func perWorker(ctx context.Context, perChan <-chan persist) {
	for p := range perChan {
		id, err := store(ctx, p.raw, p.col)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				continue // drain channel to shutdown, then exit
			}
			stdLogger.Panicf("error storing %s at height %d: %v", p.datatype, p.height, err)
		}
		if p.datatype == "block" {
			bxsLogger.Printf("%d -> %v", p.height, id)
		} else if p.datatype == "transactions" {
			txsLogger.Printf("%d -> %v", p.height, id)
		} else {
			stdLogger.Panicf("error determining datatype in %v", p)
		}
	}
}
