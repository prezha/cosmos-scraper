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
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// initDB connects to mongo database returning client and respective collections for blocks and transactions
func initDB(ctx context.Context, dbHost, dbPort, dbUser, dbPass string, napTime time.Duration) (dbc *mongo.Client, bxs, txs *mongo.Collection) {
	stdLogger.Printf("connecting to database at %s:%s as %s...", dbHost, dbPort, dbUser)

	dbc, err := dbClient(ctx, dbHost, dbPort, dbUser, dbPass, napTime)
	if err != nil {
		stdLogger.Fatalf("failed connecting to database: %v", err)
	}

	bxs = dbc.Database(dbName).Collection("blocks")
	txs = dbc.Database(dbName).Collection("transactions")

	return dbc, bxs, txs
}

// dbClient returns mongo database client after successfully connecting to it
// it will retry indefinitely on connection error, pausing for napTime between retries, unless ctx cancelled
func dbClient(ctx context.Context, dbHost, dbPort, dbUser, dbPass string, napTime time.Duration) (mc *mongo.Client, err error) {
	uri := fmt.Sprintf("mongodb://%s:%s@%s:%s", dbUser, dbPass, dbHost, dbPort)
	for {
		if mc, err = mongo.Connect(ctx, options.Client().ApplyURI(uri)); err != nil {
			stdLogger.Printf("error connecting to database (will retry in %s): %v", napTime, err)
		} else if err = mc.Ping(ctx, readpref.Primary()); err != nil {
			stdLogger.Printf("error pinging database (will retry in %s): %v", napTime, err)
		} else {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(napTime):
			continue
		}
	}
	return mc, nil
}

// store stores raw bytes as a single generalised mongo db doc returning InsertedID or any error occurred
// it will retry indefinitely on database insert error, pausing for napTime between retries, unless ctx cancelled or due to unmarshalling errors
func store(ctx context.Context, raw []byte, db *mongo.Collection) (interface{}, error) {
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("error unmarshalling %v: %v", raw, err)
	}

	var res *mongo.InsertOneResult
	var err error
	for {
		if res, err = db.InsertOne(context.Background(), doc); err == nil {
			break
		}
		stdLogger.Printf("error inserting into database (will retry in %s): %v", napTime, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(napTime):
			continue
		}

	}
	return res.InsertedID, nil
}
