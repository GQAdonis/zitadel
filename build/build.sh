#!/bin/bash

docker build -t tribehealth/zitadel:v0.0.1 -f Dockerfile --push --platform=linux/amd64  .
