#!/usr/bin/env bash
protoc -I ./ ./proto/bftraft.proto --go_out=plugins=grpc:./