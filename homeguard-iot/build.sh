#!/bin/bash

CGO_ENABLED=0 go build -o ./out/server ./cmd/server
CGO_ENABLED=0 go build -o ./out/simulator ./cmd/simulator

