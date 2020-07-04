#!/bin/sh

set -e

#image="xena/site"
image="alexheld/site"

docker build -t $image .
exec docker run --rm -itp 5030:5000 $image
