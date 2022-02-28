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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type bcClient struct {
	url        url.URL
	httpClient *http.Client
}

// newBCClient returns bcClient referencing host and port
func newBCClient(host, port string) *bcClient {
	var c bcClient
	c.url = url.URL{Host: fmt.Sprintf("%s:%s", host, port), Scheme: "http"}
	c.httpClient = &http.Client{}
	return &c
}

// request makes http request with specified path and optional query
func (c *bcClient) request(path string, query string) ([]byte, error) {
	// avoid race condition with concurrent overwrites: work with copy of bcClient's url object for each request!
	ref := c.url
	ref.Path = path
	ref.RawQuery = query
	url := ref.ResolveReference(&ref).String()

	req, err := http.NewRequest("GET", url, nil) // will slow down exit while waiting for timeouts, but using http.NewRequestWithContext would more likely create inconsistencies when interrupted with context.Canceled
	if err != nil {
		return nil, fmt.Errorf("error creating request %s: %v", url, err)
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error making request %s: %s: %s", url, resp.Status, strings.ReplaceAll(strings.ReplaceAll(string(body), "\n", ""), "  ", " "))
	}

	return io.ReadAll(resp.Body)
}

// initBC returns client and unprocessed blocks range from log and blockchain
func initBC(ctx context.Context, bcNode, bcPort string) (bcc *bcClient, gapTail, gapHead int) {
	stdLogger.Printf("connecting to bc node at %s:%s...", bcNode, bcPort)

	bcc = newBCClient(bcNode, bcPort)

	h, err := bcHeight(ctx, bcc, napTime) // last unprocessed block
	if err != nil {
		stdLogger.Panicf("error getting current blockchain height: %v", err)
	}
	stdLogger.Printf("current blockchain height is: %d", h)
	gapHead = h

	l, err := logHeight(logFile, logCheckpoint) // last processed block
	if err != nil {
		stdLogger.Panicf("error determining last processed block from log: %v", err)
	}
	stdLogger.Printf("current log height is: %d; log checkpoint is: %d", l, logCheckpoint)

	if l < logCheckpoint {
		if l > 0 || logCheckpoint > 0 { // only warn if not first start or if log checkpoint > 0
			stdLogger.Println("warn: log checkpoint is greater than current log height: will use checkpoint value as starting height")
		}
		l = logCheckpoint
	}
	if l > h {
		stdLogger.Panicln("current log height is greater than current blockchain height: cannot continue - check parameters and try again")
	}
	gapTail = l + 1 // first unprocessed block

	return bcc, gapTail, gapHead
}

// bcHeight returns latest block height or error
func bcHeight(ctx context.Context, bcc *bcClient, napTime time.Duration) (int, error) {
	blk, err := blockAt(ctx, bcc, "latest", napTime)
	if err != nil {
		return -1, err
	}
	var b struct {
		Block struct {
			Header struct {
				Height string `json:"height"`
			} `json:"header"`
		} `json:"block"`
	}
	if err := json.Unmarshal(blk, &b); err != nil {
		return -1, fmt.Errorf("error unmarshalling latest block - got response:\n%s: %v", string(blk), err)
	}

	if b.Block.Header.Height == "" {
		return -1, fmt.Errorf("error getting latest block height - got response:\n%s", string(blk))
	}

	h, err := strconv.Atoi(b.Block.Header.Height)
	if err != nil {
		return -1, fmt.Errorf("error decoding latest block height - got response:\n%s: %v", string(blk), err)
	}

	return h, nil
}

// blockAt returns block at height
// special height value of "latest" references latest block
// it will retry indefinitely on api response error, pausing for napTime between retries, unless ctx cancelled
func blockAt(ctx context.Context, bcc *bcClient, height string, napTime time.Duration) ([]byte, error) {
	for {
		// ref: https://v1.cosmos.network/rpc
		res, err := bcc.request("/cosmos/base/tendermint/v1beta1/blocks/"+height, "")
		if err != nil {
			// return unretryable error
			if strings.Contains(err.Error(), "400 Bad Request") {
				return nil, err
			}
			stdLogger.Printf("error getting block at height %s (will retry in %s): %v", height, napTime, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(napTime):
				continue
			}
		}
		return res, nil
	}
}

// transactionsAt returns transactions at height or error
// it will retry indefinitely on api response error, pausing for napTime between retries, unless ctx cancelled or due to unmarshalling errors
func transactionsAt(ctx context.Context, bcc *bcClient, height string, napTime time.Duration) ([]byte, error) {
	for {
		// ref: https://v1.cosmos.network/rpc
		res, err := bcc.request("/cosmos/tx/v1beta1/txs", "events=tx.height="+height)
		if err != nil {
			stdLogger.Printf("error getting transactions at height %s (will retry in %s): %v", height, napTime, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(napTime):
				continue
			}
		}

		var t struct {
			Pagination struct {
				Total   string      `json:"total"`
				NextKey interface{} `json:"next_key"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(res, &t); err != nil {
			return nil, err
		}
		if t.Pagination.Total == "0" {
			return nil, nil
		}
		// TODO: handle paginated response

		return res, nil
	}
}
