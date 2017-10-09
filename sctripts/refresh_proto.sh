#!/usr/bin/env bash
protoc -I ./ ./proto/server/server.proto --go_out=plugins=grpc:./
protoc -I ./ ./proto/client/client.proto --go_out=plugins=grpc:./