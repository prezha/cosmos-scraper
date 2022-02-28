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
	"log"
	"time"

	"github.com/spf13/viper"
)

// note: follows sensible default values (matching .env-example) that might be overridden during init()
var (
	logFile = "cosmos-scraper.log" // global log file for processed blocks & blocks' transactions

	// log checkpoint - last block number to consider as being consistent
	// also to avoid errors after bc hardforks, eg '400 Bad Request: { "code": 3, "message": "height 1 is not available, lowest height is 1995900: invalid request", "details": [ ]}'
	logCheckpoint = 0

	bxsLogger *log.Logger // global logger for processed blocks
	txsLogger *log.Logger // global logger for processed blocks' transactions
	stdLogger *log.Logger // global logger for everything else

	// using Cosmos REST APIs via Light Client Daemon
	// ref: https://docs.cosmos.network/master/core/grpc_rest.html and https://v1.cosmos.network/rpc/
	bcNode = "localhost"
	bcPort = "1317"

	dbHost = "localhost"
	dbPort = "27017"
	dbName = "cosmos-scraper"
	dbUser = "root"
	dbPass = "P1OLbzBD53YhFetc"

	maxReqWorkers = 100 // max number of workers in requests pool
	maxPerWorkers = 100 // max number of workers in persists pool

	napTime = 1 * time.Minute // sleep time between action retries
)

// init initialises vars from .env file or EXPORTed environment variables (latter, if set, take precedence) and initialises loggers
func init() {
	viper.SetConfigFile(".env")
	viper.ReadInConfig()
	viper.AutomaticEnv()

	if v := viper.GetString("cs_log_file"); v != "" {
		logFile = v
	}
	if v := viper.GetInt("cs_log_checkpoint"); v >= 0 {
		logCheckpoint = v
	}

	if v := viper.GetString("cs_bc_node"); v != "" {
		bcNode = v
	}
	if v := viper.GetString("cs_bc_port"); v != "" {
		bcPort = v
	}

	if v := viper.GetString("cs_db_host"); v != "" {
		dbHost = v
	}
	if v := viper.GetString("cs_db_port"); v != "" {
		dbPort = v
	}
	if v := viper.GetString("cs_db_name"); v != "" {
		dbName = v
	}
	if v := viper.GetString("cs_db_user"); v != "" {
		dbUser = v
	}
	if v := viper.GetString("cs_db_pass"); v != "" {
		dbPass = v
	}

	if v := viper.GetInt("cs_max_req_workers"); v != 0 {
		maxReqWorkers = v
	}
	if v := viper.GetInt("cs_max_per_workers"); v != 0 {
		maxPerWorkers = v
	}

	if v := viper.GetDuration("cs_naptime"); v != 0 {
		napTime = v
	}

	// init log
	if err := logSetup(logFile); err != nil {
		log.Fatalf("failed to set up logging: %v", err)
	}
}
