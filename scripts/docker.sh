#!/bin/sh
set -e

image="alexheld/site"

docker build -t $image .
exec docker run --rm -itp 5030:5000 $image
