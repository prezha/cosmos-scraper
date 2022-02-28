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
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rogpeppe/go-internal/lockedfile"
)

// logSetup initialises loggers for processes blocks and transactions and all other (standard) records using UTC timestamps
// it's also used to prevent multiple concurrently running app instances, corrupting the data (duplicate+ records)
// logs written to std logger would also be echoed to stdout
func logSetup(file string) error {
	// f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f, err := lockedfile.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // write-locked
	if err != nil {
		return err
	}
	//log.SetOutput(f)

	bxsLogger = log.New(f, "bxs: ", log.LstdFlags|log.LUTC)                            // logger for processed blocks only!       -> log file
	txsLogger = log.New(f, "txs: ", log.LstdFlags|log.LUTC)                            // logger for processed transactions only! -> log file
	stdLogger = log.New(io.MultiWriter(f, os.Stdout), "std: ", log.LstdFlags|log.LUTC) // logger for everything else              -> log file & stdout

	return nil
}

// logHeight checks log consistency from checkpoint and returns last processed block
// log entries (and blockchain blocks) below checkpoint will be ignored (ie, checkpoint is a minimal logHeight value to return)
func logHeight(file string, checkpoint int) (int, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return -1, fmt.Errorf("error reading log file %s: %v", err, file)
	}

	var b, t sort.IntSlice
	lines := bytes.Split(content, []byte{'\n'})
	for n, line := range lines {
		l := strings.Split(string(line), " ")
		switch l[0] {
		case "bxs:":
			if i, err := strconv.Atoi(l[3]); err != nil {
				return -1, fmt.Errorf("error parsing log at line %d for block height: %v", n+1, err)
			} else {
				b = append(b, i)
			}
		case "txs:":
			if i, err := strconv.Atoi(l[3]); err != nil {
				return -1, fmt.Errorf("error parsing log at line %d for block height: %v", n+1, err)
			} else {
				t = append(t, i)
			}
		case "std:":
			// check for invalid blocks that are skipped
			if len(l) > 4 && l[4] == "invalid" {
				if i, err := strconv.Atoi(l[3]); err != nil {
					return -1, fmt.Errorf("error parsing log at line %d for block height: %v", n+1, err)
				} else {
					b = append(b, i)
					t = append(t, i)
				}
			}
		}
	}

	lastBxs := 0
	if len(b) > 0 {
		b.Sort()
		lastBxs = b[len(b)-1]
		// skip check if checkpoint value is: negative, lastBxs or greater
		if 0 <= checkpoint && checkpoint < lastBxs {
			c := -1 // checkpoint index
			for i, v := range b {
				// skip any blocks before checkpoint
				if v < checkpoint {
					continue
				}
				// note index of checkpoint value
				if v == checkpoint {
					c = i
					continue
				}
				// check if blocks are in-order after checkpoint or dump reversed values to file to aid manual recovery
				if v != checkpoint+(i-c) {
					sort.Sort(sort.Reverse(b))
					dumpIntSliceToFile(b, logFile+".bxs-dump")
					return -1, fmt.Errorf("error detected while parsing log against checkpoint %d: blocks out-of-order (got: %d, want: %d); manual recovery needed (check '%s.bxs-dump' file)", checkpoint, v, checkpoint+(i-c), logFile)
				}
			}
		}
	}

	lastTxs := 0
	if len(t) > 0 {
		t.Sort()
		lastTxs = t[len(t)-1]
		// skip check if checkpoint value is: negative, lastTxs or greater
		if 0 <= checkpoint && checkpoint < lastTxs {
			c := -1 // checkpoint index
			for i, v := range t {
				// skip any blocks before checkpoint
				if v < checkpoint {
					continue
				}
				// note index of checkpoint value
				if v == checkpoint {
					c = i
					continue
				}
				// check if transactions are in-order after checkpoint or dump reversed values to file to aid manual recovery
				if v != checkpoint+(i-c) {
					sort.Sort(sort.Reverse(t))
					dumpIntSliceToFile(t, logFile+".txs-dump")
					return -1, fmt.Errorf("error detected while parsing log against checkpoint %d: transactions out-of-order (got: %d, want: %d); manual recovery needed (check '%s.txs-dump' file)", checkpoint, v, checkpoint+(i-c), logFile)
				}
			}
		}
	}

	// if lastBxs != lastTxs but checkpoint >= max(lastBxs, lastTxs), then return -1 and no error, so checkpoint will be used
	if (lastBxs > lastTxs && checkpoint >= lastBxs) ||
		(lastTxs > lastBxs && checkpoint >= lastTxs) {
		return -1, nil
	}

	if lastBxs != lastTxs {
		sort.Sort(sort.Reverse(b))
		dumpIntSliceToFile(b, logFile+".bxs-dump")
		sort.Sort(sort.Reverse(t))
		dumpIntSliceToFile(t, logFile+".txs-dump")
		return -1, fmt.Errorf("error: crash detected while parsing log: last processed block height %d != %d last processed transaction height; manual recovery needed (check '%s.?xs-dump' files)", lastBxs, lastTxs, logFile)
	}

	return lastBxs, nil
}

// dumpIntSliceToFile stores int slice to file having single value per line
func dumpIntSliceToFile(slice []int, file string) error {
	if len(slice) == 0 {
		return nil
	}

	f, err := os.OpenFile(file, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	for _, v := range slice {
		if _, err := f.WriteString(fmt.Sprintf("%d\n", v)); err != nil {
			return err
		}
	}

	return nil
}
